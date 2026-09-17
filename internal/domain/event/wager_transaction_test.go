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

func TestNewWagerTransactionProcessed(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	processedBet := domaintest.MustExternalTransaction(t, wager.KindBet, "25.00")
	require.NoError(t, processedBet.MarkProcessed(domaintest.MustParseMoney(t, "75.00", "BRL")))
	opening, err := wager.NewOpening(domain.NewID(), domain.NewID(), domain.NewID(),
		domaintest.MustParseMoney(t, "1000.00", "BRL"))
	require.NoError(t, err)
	pendingBet := domaintest.MustExternalTransaction(t, wager.KindBet, "25.00")
	rejectedBet := domaintest.MustExternalTransaction(t, wager.KindBet, "25.00")
	require.NoError(t, rejectedBet.MarkRejected(domain.FailureCodeInsufficientFunds))

	processedBetEvent := event.Event{
		Type:          event.TypeWagerTransactionProcessed,
		AggregateID:   processedBet.WalletID(),
		CorrelationID: "correlation-1",
		OccurredAt:    occurredAt,
		Version:       1,
		Data: event.WagerTransactionProcessedData{
			TransactionID:         processedBet.ID().String(),
			Kind:                  wager.KindBet,
			WalletID:              processedBet.WalletID().String(),
			PlayerID:              processedBet.PlayerID().String(),
			Money:                 domaintest.MustParseMoney(t, "25.00", "BRL"),
			Balance:               domaintest.MustParseMoney(t, "75.00", "BRL"),
			ProviderID:            "provider-a",
			ExternalTransactionID: "transaction-123",
			RoundID:               "round-987",
			GameID:                "fortune-chimp",
		},
	}

	tests := []struct {
		name          string
		transaction   *wager.Transaction
		correlationID string
		occurredAt    time.Time
		wantResult    event.Event
		wantErr       error
	}{
		{
			name:          "should accept when the transaction is processed",
			transaction:   processedBet,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantResult:    processedBetEvent,
		},
		{
			name:          "should accept when the opening carries no provider metadata",
			transaction:   opening,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantResult: event.Event{
				Type:          event.TypeWagerTransactionProcessed,
				AggregateID:   opening.WalletID(),
				CorrelationID: "correlation-1",
				OccurredAt:    occurredAt,
				Version:       1,
				Data: event.WagerTransactionProcessedData{
					TransactionID: opening.ID().String(),
					Kind:          wager.KindOpening,
					WalletID:      opening.WalletID().String(),
					PlayerID:      opening.PlayerID().String(),
					Money:         domaintest.MustParseMoney(t, "1000.00", "BRL"),
					Balance:       domaintest.MustParseMoney(t, "1000.00", "BRL"),
				},
			},
		},
		{
			name:          "should normalize when occurredAt is in another time zone",
			transaction:   processedBet,
			correlationID: "correlation-1",
			occurredAt:    time.Date(2026, 9, 8, 9, 0, 0, 0, time.FixedZone("BRT", -3*60*60)),
			wantResult:    processedBetEvent,
		},
		{
			name:          "should return INVALID_STATE_TRANSITION when the transaction is still pending",
			transaction:   pendingBet,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantErr:       domain.FailureCodeInvalidStateTransition,
		},
		{
			name:          "should return INVALID_STATE_TRANSITION when the transaction was rejected",
			transaction:   rejectedBet,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantErr:       domain.FailureCodeInvalidStateTransition,
		},
		{
			name:          "should return INVALID_INPUT when the transaction is missing",
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantErr:       domain.FailureCodeInvalidInput,
		},
		{
			name:        "should return INVALID_INPUT when correlationId is missing",
			transaction: processedBet,
			occurredAt:  occurredAt,
			wantErr:     domain.FailureCodeInvalidInput,
		},
		{
			name:          "should return INVALID_INPUT when occurredAt is missing",
			transaction:   processedBet,
			correlationID: "correlation-1",
			wantErr:       domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := event.NewWagerTransactionProcessed(test.transaction, test.correlationID, test.occurredAt)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantErr == nil, !domain.IsNilID(got.ID))
			got.ID = domain.NilID
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestNewWagerTransactionRejected(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	rejectedBet := domaintest.MustExternalTransaction(t, wager.KindBet, "25.00")
	require.NoError(t, rejectedBet.MarkRejected(domain.FailureCodeInsufficientFunds))
	pendingBet := domaintest.MustExternalTransaction(t, wager.KindBet, "25.00")
	processedBet := domaintest.MustExternalTransaction(t, wager.KindBet, "25.00")
	require.NoError(t, processedBet.MarkProcessed(domaintest.MustParseMoney(t, "75.00", "BRL")))

	tests := []struct {
		name          string
		transaction   *wager.Transaction
		correlationID string
		occurredAt    time.Time
		wantResult    event.Event
		wantErr       error
	}{
		{
			name:          "should accept when the transaction was rejected",
			transaction:   rejectedBet,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantResult: event.Event{
				Type:          event.TypeWagerTransactionRejected,
				AggregateID:   rejectedBet.WalletID(),
				CorrelationID: "correlation-1",
				OccurredAt:    occurredAt,
				Version:       1,
				Data: event.WagerTransactionRejectedData{
					TransactionID:         rejectedBet.ID().String(),
					Kind:                  wager.KindBet,
					WalletID:              rejectedBet.WalletID().String(),
					PlayerID:              rejectedBet.PlayerID().String(),
					Money:                 domaintest.MustParseMoney(t, "25.00", "BRL"),
					FailureCode:           domain.FailureCodeInsufficientFunds,
					ProviderID:            "provider-a",
					ExternalTransactionID: "transaction-123",
					RoundID:               "round-987",
					GameID:                "fortune-chimp",
				},
			},
		},
		{
			name:          "should return INVALID_STATE_TRANSITION when the transaction is still pending",
			transaction:   pendingBet,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantErr:       domain.FailureCodeInvalidStateTransition,
		},
		{
			name:          "should return INVALID_STATE_TRANSITION when the transaction was processed",
			transaction:   processedBet,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantErr:       domain.FailureCodeInvalidStateTransition,
		},
		{
			name:          "should return INVALID_INPUT when the transaction is missing",
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantErr:       domain.FailureCodeInvalidInput,
		},
		{
			name:        "should return INVALID_INPUT when correlationId is missing",
			transaction: rejectedBet,
			occurredAt:  occurredAt,
			wantErr:     domain.FailureCodeInvalidInput,
		},
		{
			name:          "should return INVALID_INPUT when occurredAt is missing",
			transaction:   rejectedBet,
			correlationID: "correlation-1",
			wantErr:       domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := event.NewWagerTransactionRejected(test.transaction, test.correlationID, test.occurredAt)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantErr == nil, !domain.IsNilID(got.ID))
			got.ID = domain.NilID
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestNewWagerTransactionPendingReference(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	waitingRefund := domaintest.MustExternalTransaction(t, wager.KindRefund, "25.00")
	require.NoError(t, waitingRefund.MarkPendingReference())
	pendingRefund := domaintest.MustExternalTransaction(t, wager.KindRefund, "25.00")

	tests := []struct {
		name          string
		transaction   *wager.Transaction
		correlationID string
		occurredAt    time.Time
		wantResult    event.Event
		wantErr       error
	}{
		{
			name:          "should accept when the transaction waits for its reference",
			transaction:   waitingRefund,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantResult: event.Event{
				Type:          event.TypeWagerTransactionPendingReference,
				AggregateID:   waitingRefund.WalletID(),
				CorrelationID: "correlation-1",
				OccurredAt:    occurredAt,
				Version:       1,
				Data: event.WagerTransactionPendingReferenceData{
					TransactionID:                  waitingRefund.ID().String(),
					Kind:                           wager.KindRefund,
					WalletID:                       waitingRefund.WalletID().String(),
					PlayerID:                       waitingRefund.PlayerID().String(),
					Money:                          domaintest.MustParseMoney(t, "25.00", "BRL"),
					ReferenceExternalTransactionID: "transaction-122",
					ProviderID:                     "provider-a",
					ExternalTransactionID:          "transaction-123",
					RoundID:                        "round-987",
					GameID:                         "fortune-chimp",
				},
			},
		},
		{
			name:          "should return INVALID_STATE_TRANSITION when the transaction is not waiting",
			transaction:   pendingRefund,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantErr:       domain.FailureCodeInvalidStateTransition,
		},
		{
			name:          "should return INVALID_INPUT when the transaction is missing",
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantErr:       domain.FailureCodeInvalidInput,
		},
		{
			name:        "should return INVALID_INPUT when correlationId is missing",
			transaction: waitingRefund,
			occurredAt:  occurredAt,
			wantErr:     domain.FailureCodeInvalidInput,
		},
		{
			name:          "should return INVALID_INPUT when occurredAt is missing",
			transaction:   waitingRefund,
			correlationID: "correlation-1",
			wantErr:       domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := event.NewWagerTransactionPendingReference(test.transaction, test.correlationID, test.occurredAt)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantErr == nil, !domain.IsNilID(got.ID))
			got.ID = domain.NilID
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestWagerTransactionDataMarshalJSON(t *testing.T) {
	t.Parallel()

	brl := domaintest.MustParseMoney(t, "25.00", "BRL")

	tests := []struct {
		name       string
		data       any
		wantResult string
	}{
		{
			name: "should format when a processed operation carries provider metadata",
			data: event.WagerTransactionProcessedData{
				TransactionID: "t-1", Kind: wager.KindBet, WalletID: "w-1", PlayerID: "p-1",
				Money: brl, Balance: domaintest.MustParseMoney(t, "75.00", "BRL"),
				ProviderID: "provider-a", ExternalTransactionID: "transaction-123", RoundID: "round-987", GameID: "fortune-chimp",
			},
			wantResult: `{"transactionId":"t-1","kind":"BET","walletId":"w-1","playerId":"p-1",
				"money":{"amount":"25.00","currency":"BRL"},"balance":{"amount":"75.00","currency":"BRL"},
				"providerId":"provider-a","externalTransactionId":"transaction-123","roundId":"round-987","gameId":"fortune-chimp"}`,
		},
		{
			name: "should format when the opening omits provider metadata",
			data: event.WagerTransactionProcessedData{
				TransactionID: "t-1", Kind: wager.KindOpening, WalletID: "w-1", PlayerID: "p-1",
				Money: brl, Balance: brl,
			},
			wantResult: `{"transactionId":"t-1","kind":"OPENING","walletId":"w-1","playerId":"p-1",
				"money":{"amount":"25.00","currency":"BRL"},"balance":{"amount":"25.00","currency":"BRL"}}`,
		},
		{
			name: "should format when a rejection carries its failure code",
			data: event.WagerTransactionRejectedData{
				TransactionID: "t-1", Kind: wager.KindBet, WalletID: "w-1", PlayerID: "p-1",
				Money: brl, FailureCode: domain.FailureCodeInsufficientFunds,
				ProviderID: "provider-a", ExternalTransactionID: "transaction-123", RoundID: "round-987", GameID: "fortune-chimp",
			},
			wantResult: `{"transactionId":"t-1","kind":"BET","walletId":"w-1","playerId":"p-1",
				"money":{"amount":"25.00","currency":"BRL"},"failureCode":"INSUFFICIENT_FUNDS",
				"providerId":"provider-a","externalTransactionId":"transaction-123","roundId":"round-987","gameId":"fortune-chimp"}`,
		},
		{
			name: "should format when a pending reference carries the awaited operation",
			data: event.WagerTransactionPendingReferenceData{
				TransactionID: "t-1", Kind: wager.KindRefund, WalletID: "w-1", PlayerID: "p-1",
				Money: brl, ReferenceExternalTransactionID: "transaction-122",
				ProviderID: "provider-a", ExternalTransactionID: "transaction-123", RoundID: "round-987", GameID: "fortune-chimp",
			},
			wantResult: `{"transactionId":"t-1","kind":"REFUND","walletId":"w-1","playerId":"p-1",
				"money":{"amount":"25.00","currency":"BRL"},"referenceExternalTransactionId":"transaction-122",
				"providerId":"provider-a","externalTransactionId":"transaction-123","roundId":"round-987","gameId":"fortune-chimp"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(test.data)

			assert.NoError(t, err)
			assert.JSONEq(t, test.wantResult, string(got))
		})
	}
}
