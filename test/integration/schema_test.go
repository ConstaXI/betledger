//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/usecase"
)

func TestSchemaEnforcesFinancialInvariants(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	w, err := useCases.OpenWallet.Execute(ctx, usecase.OpenWalletInput{
		PlayerID:       domain.NewID(),
		InitialBalance: money.MustNew(100000, money.MustCurrency("BRL")),
		CorrelationID:  "req-constraints",
	})
	require.NoError(t, err)

	var openingID, eventID string
	require.NoError(t, database.Pool.QueryRow(ctx,
		"SELECT id::text FROM wager_transactions WHERE wallet_id = $1", w.ID()).Scan(&openingID))
	require.NoError(t, database.Pool.QueryRow(ctx,
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
			tx, err := database.Pool.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()

			_, err = tx.Exec(ctx, test.sql, test.args...)

			pgErr := &pgconn.PgError{}
			errors.As(err, &pgErr)
			assert.Equal(t, test.wantCode, pgErr.Code)
		})
	}
}
