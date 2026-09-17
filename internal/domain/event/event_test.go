package event_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/domaintest"
	"github.com/davibanfi/betledger/internal/domain/event"
	"github.com/davibanfi/betledger/internal/domain/wager"
)

func TestEventMarshalJSON(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	w := domaintest.MustOpenWallet(t, domaintest.MustParseMoney(t, "100.00", "BRL"))
	entry, err := w.Debit(domaintest.MustParseMoney(t, "25.00", "BRL"), domain.NewID())
	require.NoError(t, err)

	e, err := event.NewWalletBalanceChanged(w, entry, "correlation-1", occurredAt)
	require.NoError(t, err)

	encoded, err := json.Marshal(e)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	assert.Equal(t, "WalletBalanceChanged", decoded["eventType"])
	assert.Equal(t, "2026-09-08T12:00:00Z", decoded["occurredAt"])
	assert.Equal(t, "correlation-1", decoded["correlationId"])
	assert.Equal(t, float64(1), decoded["version"])
	assert.NotContains(t, decoded, "causationId", "causationId must be omitted when absent")

	data, ok := decoded["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"amount": "25.00", "currency": "BRL"}, data["money"])
	assert.Equal(t, map[string]any{"amount": "75.00", "currency": "BRL"}, data["balanceAfter"])
}

func TestEventWithCausationDoesNotMutate(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	processed := domaintest.MustExternalTransaction(t, wager.KindBet, "25.00")
	require.NoError(t, processed.MarkProcessed(domaintest.MustParseMoney(t, "75.00", "BRL")))
	e, err := event.NewWagerTransactionProcessed(processed, "correlation-1", occurredAt)
	require.NoError(t, err)

	caused := e.WithCausation("msg-123")

	assert.Equal(t, "msg-123", caused.CausationID)
	assert.Empty(t, e.CausationID, "the original event must stay an immutable snapshot")
	assert.Equal(t, e.ID, caused.ID)
}
