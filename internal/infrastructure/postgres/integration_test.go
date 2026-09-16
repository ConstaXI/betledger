//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres"
	"github.com/davibanfi/betledger/internal/usecase"
)

var (
	databaseURL  string
	pool         *pgxpool.Pool
	openWallet   *usecase.OpenWallet
	processWager *usecase.ProcessWager
)

func TestMain(m *testing.M) {
	os.Exit(runIntegration(m))
}

func runIntegration(m *testing.M) int {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("betledger"),
		tcpostgres.WithUsername("betledger"),
		tcpostgres.WithPassword("betledger"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Printf("start postgres container: %v", err)
		return 1
	}
	defer func() { _ = testcontainers.TerminateContainer(container) }()

	databaseURL, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Printf("postgres connection string: %v", err)
		return 1
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		log.Printf("open database: %v", err)
		return 1
	}
	defer db.Close()

	provider, err := postgres.NewMigrationProvider(db)
	if err != nil {
		log.Printf("migration provider: %v", err)
		return 1
	}
	if _, err := provider.Up(ctx); err != nil {
		log.Printf("apply migrations: %v", err)
		return 1
	}

	pool, err = pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Printf("create pool: %v", err)
		return 1
	}
	defer pool.Close()

	openWallet = usecase.NewOpenWallet(
		postgres.NewTransactor(pool),
		postgres.NewWalletRepository(),
		postgres.NewTransactionRepository(),
		postgres.NewLedgerRepository(),
		postgres.NewOutboxRepository(),
		time.Now,
	)
	processWager = usecase.NewProcessWager(
		postgres.NewTransactor(pool),
		postgres.NewWalletRepository(),
		postgres.NewTransactionRepository(),
		postgres.NewLedgerRepository(),
		postgres.NewOutboxRepository(),
		time.Now,
	)

	return m.Run()
}

func TestMigrationsAreReversible(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, err := pool.Exec(ctx, "CREATE DATABASE migrations_roundtrip")
	require.NoError(t, err)

	db, err := sql.Open("pgx", strings.Replace(databaseURL, "/betledger?", "/migrations_roundtrip?", 1))
	require.NoError(t, err)
	defer db.Close()

	provider, err := postgres.NewMigrationProvider(db)
	require.NoError(t, err)

	applied, err := provider.Up(ctx)
	require.NoError(t, err)
	assert.Len(t, applied, len(provider.ListSources()))

	for range applied {
		_, err := provider.Down(ctx)
		require.NoError(t, err)
	}
	_, err = provider.Down(ctx)
	assert.ErrorIs(t, err, goose.ErrNoNextVersion)

	reapplied, err := provider.Up(ctx)
	require.NoError(t, err)
	assert.Len(t, reapplied, len(provider.ListSources()))
}

func TestOpenWalletPersistsOpeningAtomically(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	w, err := openWallet.Execute(ctx, usecase.OpenWalletInput{
		PlayerID:       domain.NewID(),
		InitialBalance: money.MustNew(100000, money.MustCurrency("BRL")),
		CorrelationID:  "req-integration",
	})
	require.NoError(t, err)

	var balance, version int64
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT balance_minor, version FROM wallets WHERE id = $1", w.ID()).Scan(&balance, &version))
	assert.Equal(t, int64(100000), balance)
	assert.Equal(t, int64(1), version)

	var kind, state string
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT kind, state FROM wager_transactions WHERE wallet_id = $1", w.ID()).Scan(&kind, &state))
	assert.Equal(t, "OPENING", kind)
	assert.Equal(t, "PROCESSED", state)

	var direction string
	var before, after int64
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT direction, balance_before_minor, balance_after_minor FROM wallet_ledger_entries WHERE wallet_id = $1",
		w.ID()).Scan(&direction, &before, &after))
	assert.Equal(t, "CREDIT", direction)
	assert.Equal(t, int64(0), before)
	assert.Equal(t, int64(100000), after)

	rows, err := pool.Query(ctx,
		"SELECT event_type, payload->>'correlationId' FROM outbox_events WHERE aggregate_id = $1 ORDER BY event_type",
		w.ID())
	require.NoError(t, err)
	var eventTypes, correlationIDs []string
	for rows.Next() {
		var eventType, correlationID string
		require.NoError(t, rows.Scan(&eventType, &correlationID))
		eventTypes = append(eventTypes, eventType)
		correlationIDs = append(correlationIDs, correlationID)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"WagerTransactionProcessed", "WalletBalanceChanged"}, eventTypes)
	assert.Equal(t, []string{"req-integration", "req-integration"}, correlationIDs)
}

func TestOpenWalletRejectsDuplicateWithoutSideEffects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	input := usecase.OpenWalletInput{
		PlayerID:       domain.NewID(),
		InitialBalance: money.MustNew(5000, money.MustCurrency("BRL")),
		CorrelationID:  "req-duplicate",
	}

	_, err := openWallet.Execute(ctx, input)
	require.NoError(t, err)

	_, err = openWallet.Execute(ctx, input)
	assert.ErrorIs(t, err, domain.FailureCodeWalletAlreadyExists)

	var wallets, transactions, outbox int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM wallets WHERE player_id = $1),
			(SELECT count(*) FROM wager_transactions WHERE player_id = $1),
			(SELECT count(*) FROM outbox_events o JOIN wallets w ON w.id = o.aggregate_id WHERE w.player_id = $1)`,
		input.PlayerID).Scan(&wallets, &transactions, &outbox))
	assert.Equal(t, 1, wallets)
	assert.Equal(t, 1, transactions)
	assert.Equal(t, 2, outbox)
}

func TestTransactorRollsBackWhenTheUnitOfWorkFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	w, err := wallet.Open(domain.NewID(), domain.NewID(), money.MustNew(0, money.MustCurrency("BRL")))
	require.NoError(t, err)
	errAbort := errors.New("abort")

	err = postgres.NewTransactor(pool).WithinTransaction(ctx, func(ctx context.Context) error {
		require.NoError(t, postgres.NewWalletRepository().Create(ctx, w))
		return errAbort
	})

	assert.ErrorIs(t, err, errAbort)
	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM wallets WHERE id = $1", w.ID()).Scan(&count))
	assert.Equal(t, 0, count)
}

func TestRepositoriesRejectWritesOutsideTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	w, err := wallet.Open(domain.NewID(), domain.NewID(), money.MustNew(0, money.MustCurrency("BRL")))
	require.NoError(t, err)

	err = postgres.NewWalletRepository().Create(ctx, w)

	assert.Error(t, err)
	var count int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM wallets WHERE id = $1", w.ID()).Scan(&count))
	assert.Equal(t, 0, count)
}

func TestSchemaEnforcesFinancialInvariants(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	w, err := openWallet.Execute(ctx, usecase.OpenWalletInput{
		PlayerID:       domain.NewID(),
		InitialBalance: money.MustNew(100000, money.MustCurrency("BRL")),
		CorrelationID:  "req-constraints",
	})
	require.NoError(t, err)

	var openingID, eventID string
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT id::text FROM wager_transactions WHERE wallet_id = $1", w.ID()).Scan(&openingID))
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT id::text FROM outbox_events WHERE aggregate_id = $1 LIMIT 1", w.ID()).Scan(&eventID))

	const (
		checkViolation    = "23514"
		uniqueViolation   = "23505"
		restrictViolation = "23001"
	)

	tests := []struct {
		name     string
		sql      string
		args     []any
		wantCode string
	}{
		{
			name:     "should return check_violation when a wallet balance becomes negative",
			sql:      "UPDATE wallets SET balance_minor = -1 WHERE id = $1",
			args:     []any{w.ID()},
			wantCode: checkViolation,
		},
		{
			name:     "should return restrict_violation when a ledger entry is updated",
			sql:      "UPDATE wallet_ledger_entries SET amount_minor = 1 WHERE wallet_id = $1",
			args:     []any{w.ID()},
			wantCode: restrictViolation,
		},
		{
			name:     "should return restrict_violation when a ledger entry is deleted",
			sql:      "DELETE FROM wallet_ledger_entries WHERE wallet_id = $1",
			args:     []any{w.ID()},
			wantCode: restrictViolation,
		},
		{
			name:     "should return restrict_violation when the ledger is truncated",
			sql:      "TRUNCATE wallet_ledger_entries",
			wantCode: restrictViolation,
		},
		{
			name: "should return check_violation when a ledger entry breaks the balance arithmetic",
			sql: `INSERT INTO wallet_ledger_entries
				(id, wallet_id, transaction_id, direction, currency, amount_minor, balance_before_minor, balance_after_minor)
				VALUES (gen_random_uuid(), $1, $2, 'CREDIT', 'BRL', 100, 0, 999)`,
			args:     []any{w.ID(), openingID},
			wantCode: checkViolation,
		},
		{
			name: "should return unique_violation when a wallet receives a second opening",
			sql: `INSERT INTO wager_transactions (id, kind, state, wallet_id, player_id, currency, amount_minor)
				VALUES (gen_random_uuid(), 'OPENING', 'PROCESSED', $1, $2, 'BRL', 100)`,
			args:     []any{w.ID(), w.PlayerID()},
			wantCode: uniqueViolation,
		},
		{
			name: "should return check_violation when an opening carries provider metadata",
			sql: `INSERT INTO wager_transactions (id, kind, state, wallet_id, player_id, currency, amount_minor, provider_id)
				VALUES (gen_random_uuid(), 'OPENING', 'PROCESSED', $1, $2, 'BRL', 100, 'provider-a')`,
			args:     []any{w.ID(), w.PlayerID()},
			wantCode: checkViolation,
		},
		{
			name:     "should return restrict_violation when an outbox payload is changed",
			sql:      `UPDATE outbox_events SET payload = '{}' WHERE id = $1`,
			args:     []any{eventID},
			wantCode: restrictViolation,
		},
		{
			name: "should accept when publication tracking columns change",
			sql:  "UPDATE outbox_events SET attempts = attempts + 1, published_at = now() WHERE id = $1",
			args: []any{eventID},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()

			_, err = tx.Exec(ctx, test.sql, test.args...)

			pgErr := &pgconn.PgError{}
			errors.As(err, &pgErr)
			assert.Equal(t, test.wantCode, pgErr.Code)
		})
	}
}
