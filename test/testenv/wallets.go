//go:build integration

package testenv

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres"
	"github.com/davibanfi/betledger/internal/usecase"
)

// UseCases are the use cases wired to the PostgreSQL adapters, without Fx.
type UseCases struct {
	OpenWallet   *usecase.OpenWallet
	ProcessWager *usecase.ProcessWager
}

// NewUseCases wires the use cases to the database.
func (p *Postgres) NewUseCases() UseCases {
	return UseCases{
		OpenWallet: usecase.NewOpenWallet(
			postgres.NewTransactor(p.Pool),
			postgres.NewWalletRepository(),
			postgres.NewTransactionRepository(),
			postgres.NewLedgerRepository(),
			postgres.NewOutboxRepository(),
			time.Now,
		),
		ProcessWager: usecase.NewProcessWager(
			postgres.NewTransactor(p.Pool),
			postgres.NewWalletRepository(),
			postgres.NewTransactionRepository(),
			postgres.NewLedgerRepository(),
			postgres.NewOutboxRepository(),
			time.Now,
		),
	}
}

// MustOpenWallet opens a wallet for a new player through the use case.
func (u UseCases) MustOpenWallet(t *testing.T, balance money.Money) *wallet.Wallet {
	t.Helper()

	opened, err := u.OpenWallet.Execute(context.Background(), usecase.OpenWalletInput{
		PlayerID:       domain.NewID(),
		InitialBalance: balance,
		CorrelationID:  "req-seed",
	})
	require.NoError(t, err)
	return opened
}

// BetInput builds a BET on the wallet.
func BetInput(w *wallet.Wallet, externalID string, amountMinor int64) usecase.ProcessWagerInput {
	return WagerInput(w, wager.KindBet, externalID, amountMinor)
}

// WagerInput builds an operation of the kind on the wallet. The external
// identifier is scoped to the wallet, so tests sharing a database never collide.
func WagerInput(w *wallet.Wallet, kind wager.Kind, externalID string, amountMinor int64) usecase.ProcessWagerInput {
	scoped := w.ID().String() + ":" + externalID
	return usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: scoped,
		IdempotencyKey:        "provider-a:" + scoped,
		PlayerID:              w.PlayerID(),
		WalletID:              w.ID(),
		RoundID:               "round-1",
		GameID:                "fortune-chimp",
		Kind:                  kind,
		Money:                 money.MustNew(amountMinor, w.Currency()),
		CorrelationID:         "req-" + externalID,
	}
}

// ReferringInput builds an operation of the kind that refers to another one on
// the same wallet, both identifiers scoped like WagerInput scopes them.
func ReferringInput(
	w *wallet.Wallet,
	kind wager.Kind,
	externalID, referenceID string,
	amountMinor int64,
) usecase.ProcessWagerInput {
	input := WagerInput(w, kind, externalID, amountMinor)
	input.ReferenceExternalTransactionID = w.ID().String() + ":" + referenceID
	return input
}

// WalletState reads the stored balance, version and number of debits.
func (p *Postgres) WalletState(t *testing.T, walletID domain.ID) (balance, version int64, debits int) {
	t.Helper()

	require.NoError(t, p.Pool.QueryRow(context.Background(), `
		SELECT w.balance_minor, w.version,
			(SELECT count(*) FROM wallet_ledger_entries l WHERE l.wallet_id = w.id AND l.direction = 'DEBIT')
		FROM wallets w WHERE w.id = $1`, walletID).Scan(&balance, &version, &debits))
	return balance, version, debits
}

// AssertLedgerReconciles checks that the stored balance equals the credits minus
// the debits recorded in the ledger.
func (p *Postgres) AssertLedgerReconciles(t *testing.T, walletID domain.ID) {
	t.Helper()

	var stored, rebuilt int64
	require.NoError(t, p.Pool.QueryRow(context.Background(), `
		SELECT w.balance_minor,
			COALESCE(SUM(CASE l.direction WHEN 'CREDIT' THEN l.amount_minor ELSE -l.amount_minor END), 0)
		FROM wallets w
		LEFT JOIN wallet_ledger_entries l ON l.wallet_id = w.id
		WHERE w.id = $1
		GROUP BY w.balance_minor`, walletID).Scan(&stored, &rebuilt))
	assert.Equal(t, stored, rebuilt, "stored balance must equal credits minus debits in the ledger")
}

// EventCounts counts the outbox events of the wallet by type.
func (p *Postgres) EventCounts(t *testing.T, walletID domain.ID) map[string]int {
	t.Helper()

	rows, err := p.Pool.Query(context.Background(),
		"SELECT event_type, count(*) FROM outbox_events WHERE aggregate_id = $1 GROUP BY event_type", walletID)
	require.NoError(t, err)
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var eventType string
		var count int
		require.NoError(t, rows.Scan(&eventType, &count))
		counts[eventType] = count
	}
	require.NoError(t, rows.Err())
	return counts
}

// Connections counts the open connections tagged with the application name.
func (p *Postgres) Connections(t *testing.T, applicationName string) int {
	t.Helper()

	var count int
	require.NoError(t, p.Pool.QueryRow(context.Background(),
		"SELECT count(*) FROM pg_stat_activity WHERE application_name = $1", applicationName).Scan(&count))
	return count
}

// PlayerRecords is everything recorded for a player, in a deterministic order.
type PlayerRecords struct {
	Wallets      []WalletRecord
	Transactions []TransactionRecord
	Entries      []EntryRecord
	Events       []EventRecord
}

// WalletRecord is a stored wallet.
type WalletRecord struct {
	BalanceMinor int64
	Version      int64
}

// TransactionRecord is a stored wager transaction.
type TransactionRecord struct {
	Kind  string
	State string
}

// EntryRecord is a stored ledger entry.
type EntryRecord struct {
	Direction          string
	BalanceBeforeMinor int64
	BalanceAfterMinor  int64
}

// EventRecord is a stored outbox event.
type EventRecord struct {
	Type          string
	CorrelationID string
}

// PlayerRecords reads the wallets, transactions, ledger entries and outbox
// events of the player.
func (p *Postgres) PlayerRecords(t *testing.T, playerID domain.ID) PlayerRecords {
	t.Helper()

	ctx := context.Background()
	var records PlayerRecords

	rows, err := p.Pool.Query(ctx,
		"SELECT balance_minor, version FROM wallets WHERE player_id = $1 ORDER BY id", playerID)
	require.NoError(t, err)
	for rows.Next() {
		var record WalletRecord
		require.NoError(t, rows.Scan(&record.BalanceMinor, &record.Version))
		records.Wallets = append(records.Wallets, record)
	}
	require.NoError(t, rows.Err())

	rows, err = p.Pool.Query(ctx,
		"SELECT kind, state FROM wager_transactions WHERE player_id = $1 ORDER BY id", playerID)
	require.NoError(t, err)
	for rows.Next() {
		var record TransactionRecord
		require.NoError(t, rows.Scan(&record.Kind, &record.State))
		records.Transactions = append(records.Transactions, record)
	}
	require.NoError(t, rows.Err())

	rows, err = p.Pool.Query(ctx, `
		SELECT l.direction, l.balance_before_minor, l.balance_after_minor
		FROM wallet_ledger_entries l JOIN wallets w ON w.id = l.wallet_id
		WHERE w.player_id = $1 ORDER BY l.id`, playerID)
	require.NoError(t, err)
	for rows.Next() {
		var record EntryRecord
		require.NoError(t, rows.Scan(&record.Direction, &record.BalanceBeforeMinor, &record.BalanceAfterMinor))
		records.Entries = append(records.Entries, record)
	}
	require.NoError(t, rows.Err())

	rows, err = p.Pool.Query(ctx, `
		SELECT o.event_type, o.payload->>'correlationId'
		FROM outbox_events o JOIN wallets w ON w.id = o.aggregate_id
		WHERE w.player_id = $1 ORDER BY o.event_type`, playerID)
	require.NoError(t, err)
	for rows.Next() {
		var record EventRecord
		require.NoError(t, rows.Scan(&record.Type, &record.CorrelationID))
		records.Events = append(records.Events, record)
	}
	require.NoError(t, rows.Err())

	return records
}

// WalletExists reports whether the wallet was stored.
func (p *Postgres) WalletExists(t *testing.T, walletID domain.ID) bool {
	t.Helper()

	var exists bool
	require.NoError(t, p.Pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM wallets WHERE id = $1)", walletID).Scan(&exists))
	return exists
}
