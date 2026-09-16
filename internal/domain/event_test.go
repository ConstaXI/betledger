package domain_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
)

func TestNewWagerTransactionProcessedEvent(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		transaction   func(t *testing.T) *domain.WagerTransaction
		correlationID string
		occurredAt    time.Time
		wantErr       error
	}{
		{
			name:          "should accept when the transaction is processed",
			transaction:   processedTransaction,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
		},
		{
			name:          "should return INVALID_STATE_TRANSITION when the transaction is still pending",
			transaction:   func(t *testing.T) *domain.WagerTransaction { return mustTransaction(t, domain.KindBet, "25.00") },
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantErr:       domain.FailureCodeInvalidStateTransition,
		},
		{
			name:          "should return INVALID_INPUT when correlationId is missing",
			transaction:   processedTransaction,
			correlationID: "",
			occurredAt:    occurredAt,
			wantErr:       domain.FailureCodeInvalidInput,
		},
		{
			name:          "should return INVALID_INPUT when occurredAt is missing",
			transaction:   processedTransaction,
			correlationID: "correlation-1",
			occurredAt:    time.Time{},
			wantErr:       domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.NewWagerTransactionProcessedEvent(test.transaction(t), test.correlationID, test.occurredAt)

			assert.ErrorIs(t, err, test.wantErr)
			if test.wantErr == nil {
				assert.Equal(t, domain.EventTypeWagerTransactionProcessed, got.EventType)
				assert.Equal(t, 1, got.Version)
			}
		})
	}
}

func TestNewWagerTransactionRejectedEvent(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	rejected := mustTransaction(t, domain.KindBet, "25.00")
	require.NoError(t, rejected.MarkRejected(domain.FailureCodeInsufficientFunds))

	got, err := domain.NewWagerTransactionRejectedEvent(rejected, "correlation-1", occurredAt)

	require.NoError(t, err)
	assert.Equal(t, domain.EventTypeWagerTransactionRejected, got.EventType)
	assert.Equal(t, rejected.WalletID(), got.AggregateID)

	data, ok := got.Data.(domain.WagerTransactionRejectedData)
	require.True(t, ok)
	assert.Equal(t, domain.FailureCodeInsufficientFunds, data.FailureCode)

	pending := mustTransaction(t, domain.KindBet, "25.00")
	_, err = domain.NewWagerTransactionRejectedEvent(pending, "correlation-1", occurredAt)
	assert.ErrorIs(t, err, domain.FailureCodeInvalidStateTransition)
}

func TestNewWagerTransactionPendingReferenceEvent(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	transaction := mustTransaction(t, domain.KindRefund, "25.00")
	require.NoError(t, transaction.MarkPendingReference())

	got, err := domain.NewWagerTransactionPendingReferenceEvent(transaction, "correlation-1", occurredAt)

	require.NoError(t, err)
	assert.Equal(t, domain.EventTypeWagerTransactionPendingReference, got.EventType)

	data, ok := got.Data.(domain.WagerTransactionPendingReferenceData)
	require.True(t, ok)
	assert.Equal(t, "transaction-122", data.ReferenceExternalTransactionID)
}

func TestNewWalletBalanceChangedEvent(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	wallet := mustWallet(t, mustMoney(t, "100.00", "BRL"))
	entry, err := wallet.Debit(mustMoney(t, "25.00", "BRL"), domain.NewID())
	require.NoError(t, err)

	got, err := domain.NewWalletBalanceChangedEvent(wallet, entry, "correlation-1", occurredAt)

	require.NoError(t, err)
	assert.Equal(t, domain.EventTypeWalletBalanceChanged, got.EventType)
	assert.Equal(t, wallet.ID(), got.AggregateID)

	data, ok := got.Data.(domain.WalletBalanceChangedData)
	require.True(t, ok)
	assert.Equal(t, domain.DirectionDebit, data.Direction)
	assert.Equal(t, mustMoney(t, "25.00", "BRL"), data.Money)
	assert.Equal(t, mustMoney(t, "100.00", "BRL"), data.BalanceBefore)
	assert.Equal(t, mustMoney(t, "75.00", "BRL"), data.BalanceAfter)
	assert.Equal(t, int64(2), data.WalletVersion)
}

func TestWalletBalanceChangedEventRejectsForeignEntry(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	wallet := mustWallet(t, mustMoney(t, "100.00", "BRL"))
	other := mustWallet(t, mustMoney(t, "100.00", "BRL"))
	entry, err := other.Debit(mustMoney(t, "25.00", "BRL"), domain.NewID())
	require.NoError(t, err)

	_, err = domain.NewWalletBalanceChangedEvent(wallet, entry, "correlation-1", occurredAt)

	assert.ErrorIs(t, err, domain.FailureCodeInvalidInput)
}

func TestEventMarshalJSON(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	wallet := mustWallet(t, mustMoney(t, "100.00", "BRL"))
	entry, err := wallet.Debit(mustMoney(t, "25.00", "BRL"), domain.NewID())
	require.NoError(t, err)

	event, err := domain.NewWalletBalanceChangedEvent(wallet, entry, "correlation-1", occurredAt)
	require.NoError(t, err)

	encoded, err := json.Marshal(event)
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

func TestEventOccurredAtIsNormalizedToUTC(t *testing.T) {
	t.Parallel()

	saoPaulo := time.FixedZone("BRT", -3*60*60)
	occurredAt := time.Date(2026, 9, 8, 9, 0, 0, 0, saoPaulo)

	transaction := processedTransaction(t)
	event, err := domain.NewWagerTransactionProcessedEvent(transaction, "correlation-1", occurredAt)
	require.NoError(t, err)

	encoded, err := json.Marshal(event)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	assert.Equal(t, "2026-09-08T12:00:00Z", decoded["occurredAt"])
	assert.Equal(t, time.UTC, event.OccurredAt.Location())
}

func TestEventWithCausationDoesNotMutate(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	event, err := domain.NewWagerTransactionProcessedEvent(processedTransaction(t), "correlation-1", occurredAt)
	require.NoError(t, err)

	caused := event.WithCausation("msg-123")

	assert.Equal(t, "msg-123", caused.CausationID)
	assert.Empty(t, event.CausationID, "the original event must stay an immutable snapshot")
	assert.Equal(t, event.EventID, caused.EventID)
}

func TestOpeningEventCarriesNoProviderMetadata(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	initial := mustMoney(t, "1000.00", "BRL")

	transaction, err := domain.NewOpeningTransaction(domain.NewID(), domain.NewID(), domain.NewID(), initial)
	require.NoError(t, err)

	event, err := domain.NewWagerTransactionProcessedEvent(transaction, "correlation-1", occurredAt)
	require.NoError(t, err)

	encoded, err := json.Marshal(event)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	data := decoded["data"].(map[string]any)

	assert.Equal(t, "OPENING", data["kind"])
	for _, field := range []string{"providerId", "externalTransactionId", "roundId", "gameId"} {
		assert.NotContains(t, data, field, "internal opening must omit external metadata")
	}
}

func processedTransaction(t *testing.T) *domain.WagerTransaction {
	t.Helper()

	transaction := mustTransaction(t, domain.KindBet, "25.00")
	require.NoError(t, transaction.MarkProcessed(mustMoney(t, "75.00", "BRL")))
	return transaction
}
