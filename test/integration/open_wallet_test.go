//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/usecase"
)

func TestOpenWalletPersistsOpeningAtomically(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	w, err := useCases.OpenWallet.Execute(ctx, usecase.OpenWalletInput{
		PlayerID:       domain.NewID(),
		InitialBalance: money.MustNew(100000, money.MustCurrency("BRL")),
		CorrelationID:  "req-integration",
	})
	require.NoError(t, err)

	var balance, version int64
	require.NoError(t, database.Pool.QueryRow(ctx,
		"SELECT balance_minor, version FROM wallets WHERE id = $1", w.ID()).Scan(&balance, &version))
	assert.Equal(t, int64(100000), balance)
	assert.Equal(t, int64(1), version)

	var kind, state string
	require.NoError(t, database.Pool.QueryRow(ctx,
		"SELECT kind, state FROM wager_transactions WHERE wallet_id = $1", w.ID()).Scan(&kind, &state))
	assert.Equal(t, "OPENING", kind)
	assert.Equal(t, "PROCESSED", state)

	var direction string
	var before, after int64
	require.NoError(t, database.Pool.QueryRow(ctx,
		"SELECT direction, balance_before_minor, balance_after_minor FROM wallet_ledger_entries WHERE wallet_id = $1",
		w.ID()).Scan(&direction, &before, &after))
	assert.Equal(t, "CREDIT", direction)
	assert.Equal(t, int64(0), before)
	assert.Equal(t, int64(100000), after)

	rows, err := database.Pool.Query(ctx,
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

	_, err := useCases.OpenWallet.Execute(ctx, input)
	require.NoError(t, err)

	_, err = useCases.OpenWallet.Execute(ctx, input)
	assert.ErrorIs(t, err, domain.FailureCodeWalletAlreadyExists)

	var wallets, transactions, outbox int
	require.NoError(t, database.Pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM wallets WHERE player_id = $1),
			(SELECT count(*) FROM wager_transactions WHERE player_id = $1),
			(SELECT count(*) FROM outbox_events o JOIN wallets w ON w.id = o.aggregate_id WHERE w.player_id = $1)`,
		input.PlayerID).Scan(&wallets, &transactions, &outbox))
	assert.Equal(t, 1, wallets)
	assert.Equal(t, 1, transactions)
	assert.Equal(t, 2, outbox)
}
