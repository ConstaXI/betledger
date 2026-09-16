package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
)

func TestParseExternalTransactionKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		value      string
		wantResult domain.TransactionKind
		wantErr    error
	}{
		{name: "should accept when the kind is BET", value: "BET", wantResult: domain.KindBet},
		{name: "should accept when the kind is WIN", value: "WIN", wantResult: domain.KindWin},
		{name: "should accept when the kind is LOSS", value: "LOSS", wantResult: domain.KindLoss},
		{name: "should accept when the kind is REFUND", value: "REFUND", wantResult: domain.KindRefund},
		{name: "should accept when the kind is ROLLBACK", value: "ROLLBACK", wantResult: domain.KindRollback},
		{name: "should return TRANSACTION_KIND_NOT_ALLOWED when the kind is OPENING", value: "OPENING", wantErr: domain.FailureCodeKindNotAllowed},
		{name: "should return INVALID_INPUT when the kind is unknown", value: "UNKNOWN", wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the kind is empty", value: "", wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the kind is lowercase", value: "bet", wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.ParseExternalTransactionKind(test.value)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestNewExternalTransaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    domain.TransactionKind
		amount  string
		mutate  func(*domain.NewExternalTransactionParams)
		wantErr error
	}{
		{name: "should accept when BET has a positive amount", kind: domain.KindBet, amount: "25.00"},
		{name: "should accept when WIN has a positive amount", kind: domain.KindWin, amount: "25.00"},
		{name: "should accept when LOSS has a zero amount", kind: domain.KindLoss, amount: "0.00"},
		{name: "should accept when REFUND has a positive amount", kind: domain.KindRefund, amount: "25.00"},
		{name: "should accept when ROLLBACK has a positive amount", kind: domain.KindRollback, amount: "25.00"},

		{name: "should return INVALID_AMOUNT when BET has a zero amount", kind: domain.KindBet, amount: "0.00", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when WIN has a zero amount", kind: domain.KindWin, amount: "0.00", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when LOSS carries an amount", kind: domain.KindLoss, amount: "25.00", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when REFUND has a zero amount", kind: domain.KindRefund, amount: "0.00", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when ROLLBACK has a zero amount", kind: domain.KindRollback, amount: "0.00", wantErr: domain.FailureCodeInvalidAmount},

		{name: "should return TRANSACTION_KIND_NOT_ALLOWED when the kind is OPENING", kind: domain.KindOpening, amount: "25.00", wantErr: domain.FailureCodeKindNotAllowed},
		{name: "should return INVALID_INPUT when the kind is unknown", kind: "TRANSFER", amount: "25.00", wantErr: domain.FailureCodeInvalidInput},

		{
			name: "should return INVALID_INPUT when REFUND has no reference", kind: domain.KindRefund, amount: "25.00",
			mutate:  func(p *domain.NewExternalTransactionParams) { p.ReferenceExternalTransactionID = "" },
			wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when ROLLBACK has no reference", kind: domain.KindRollback, amount: "25.00",
			mutate:  func(p *domain.NewExternalTransactionParams) { p.ReferenceExternalTransactionID = "" },
			wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when BET carries a reference", kind: domain.KindBet, amount: "25.00",
			mutate:  func(p *domain.NewExternalTransactionParams) { p.ReferenceExternalTransactionID = "transaction-122" },
			wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when LOSS carries a reference", kind: domain.KindLoss, amount: "0.00",
			mutate:  func(p *domain.NewExternalTransactionParams) { p.ReferenceExternalTransactionID = "transaction-122" },
			wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should accept when WIN carries an optional reference", kind: domain.KindWin, amount: "25.00",
			mutate: func(p *domain.NewExternalTransactionParams) { p.ReferenceExternalTransactionID = "transaction-122" },
		},

		{
			name: "should return INVALID_INPUT when providerId is missing", kind: domain.KindBet, amount: "25.00",
			mutate: func(p *domain.NewExternalTransactionParams) { p.ProviderID = "" }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when externalTransactionId is missing", kind: domain.KindBet, amount: "25.00",
			mutate: func(p *domain.NewExternalTransactionParams) { p.ExternalTransactionID = "" }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when idempotencyKey is missing", kind: domain.KindBet, amount: "25.00",
			mutate: func(p *domain.NewExternalTransactionParams) { p.IdempotencyKey = "" }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when payloadHash is missing", kind: domain.KindBet, amount: "25.00",
			mutate: func(p *domain.NewExternalTransactionParams) { p.PayloadHash = "" }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when roundId is missing", kind: domain.KindBet, amount: "25.00",
			mutate: func(p *domain.NewExternalTransactionParams) { p.RoundID = "" }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when gameId is missing", kind: domain.KindBet, amount: "25.00",
			mutate: func(p *domain.NewExternalTransactionParams) { p.GameID = "" }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when walletId is nil", kind: domain.KindBet, amount: "25.00",
			mutate: func(p *domain.NewExternalTransactionParams) { p.WalletID = domain.NilID }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when playerId is nil", kind: domain.KindBet, amount: "25.00",
			mutate: func(p *domain.NewExternalTransactionParams) { p.PlayerID = domain.NilID }, wantErr: domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			params := validExternalParams(t, test.kind, test.amount)
			if test.mutate != nil {
				test.mutate(&params)
			}

			_, err := domain.NewExternalTransaction(params)

			assert.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestTransactionStateMachine(t *testing.T) {
	t.Parallel()

	transaction := mustTransaction(t, domain.KindBet, "25.00")
	assert.Equal(t, domain.StatePending, transaction.State())

	require.NoError(t, transaction.MarkProcessed(mustMoney(t, "75.00", "BRL")))

	balance, ok := transaction.ResultBalance()
	assert.True(t, ok)
	assert.Equal(t, mustMoney(t, "75.00", "BRL"), balance)

	assert.ErrorIs(t, transaction.MarkRejected(domain.FailureCodeInsufficientFunds), domain.FailureCodeInvalidStateTransition)
	assert.ErrorIs(t, transaction.MarkProcessed(mustMoney(t, "10.00", "BRL")), domain.FailureCodeInvalidStateTransition)
}

func TestTransactionPendingReferenceFlow(t *testing.T) {
	t.Parallel()

	transaction := mustTransaction(t, domain.KindRefund, "25.00")

	require.NoError(t, transaction.MarkPendingReference())
	assert.Equal(t, domain.StatePendingReference, transaction.State())

	referenceID := domain.NewID()
	require.NoError(t, transaction.ResolveReference(referenceID))

	resolved, ok := transaction.ReferenceTransactionID()
	assert.True(t, ok)
	assert.Equal(t, referenceID, resolved)

	assert.NoError(t, transaction.MarkProcessed(mustMoney(t, "125.00", "BRL")))
}

func TestTransactionRejectionRequiresFailureCode(t *testing.T) {
	t.Parallel()

	transaction := mustTransaction(t, domain.KindBet, "25.00")

	assert.ErrorIs(t, transaction.MarkRejected(""), domain.FailureCodeInvalidInput)

	require.NoError(t, transaction.MarkRejected(domain.FailureCodeInsufficientFunds))
	assert.Equal(t, domain.FailureCodeInsufficientFunds, transaction.FailureCode())
	assert.True(t, transaction.State().IsTerminal())
}

func TestNewOpeningTransaction(t *testing.T) {
	t.Parallel()

	initial := mustMoney(t, "1000.00", "BRL")

	transaction, err := domain.NewOpeningTransaction(domain.NewID(), domain.NewID(), domain.NewID(), initial)

	require.NoError(t, err)
	assert.Equal(t, domain.StateProcessed, transaction.State())
	assert.False(t, transaction.Kind().IsExternal())
	assert.Empty(t, transaction.ProviderID())
	assert.Empty(t, transaction.ExternalTransactionID())
	assert.Empty(t, transaction.IdempotencyKey())
	assert.Empty(t, transaction.PayloadHash())
	assert.Empty(t, transaction.RoundID())
	assert.Empty(t, transaction.GameID())

	zero := mustMoney(t, "0.00", "BRL")
	_, err = domain.NewOpeningTransaction(domain.NewID(), domain.NewID(), domain.NewID(), zero)
	assert.ErrorIs(t, err, domain.FailureCodeInvalidAmount)
}

func TestTransactionStateCanTransitionTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		from       domain.TransactionState
		to         domain.TransactionState
		wantResult bool
	}{
		{name: "should return true when transitioning from pending to pending reference", from: domain.StatePending, to: domain.StatePendingReference, wantResult: true},
		{name: "should return true when transitioning from pending to processed", from: domain.StatePending, to: domain.StateProcessed, wantResult: true},
		{name: "should return true when transitioning from pending to rejected", from: domain.StatePending, to: domain.StateRejected, wantResult: true},
		{name: "should return true when transitioning from pending to failed", from: domain.StatePending, to: domain.StateFailed, wantResult: true},
		{name: "should return true when transitioning from pending reference to processed", from: domain.StatePendingReference, to: domain.StateProcessed, wantResult: true},
		{name: "should return true when transitioning from pending reference to rejected", from: domain.StatePendingReference, to: domain.StateRejected, wantResult: true},

		{name: "should return false when transitioning from pending reference to itself", from: domain.StatePendingReference, to: domain.StatePendingReference, wantResult: false},
		{name: "should return false when transitioning from pending reference back to pending", from: domain.StatePendingReference, to: domain.StatePending, wantResult: false},
		{name: "should return false when transitioning from processed to rejected", from: domain.StateProcessed, to: domain.StateRejected, wantResult: false},
		{name: "should return false when transitioning from processed to processed", from: domain.StateProcessed, to: domain.StateProcessed, wantResult: false},
		{name: "should return false when transitioning from rejected to processed", from: domain.StateRejected, to: domain.StateProcessed, wantResult: false},
		{name: "should return false when transitioning from failed to processed", from: domain.StateFailed, to: domain.StateProcessed, wantResult: false},
		{name: "should return false when the target state is unknown", from: domain.StatePending, to: "ARCHIVED", wantResult: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.wantResult, test.from.CanTransitionTo(test.to))
		})
	}
}

func TestTransactionStateIsTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		state      domain.TransactionState
		wantResult bool
	}{
		{name: "should return false when the state is pending", state: domain.StatePending, wantResult: false},
		{name: "should return false when the state is pending reference", state: domain.StatePendingReference, wantResult: false},
		{name: "should return true when the state is processed", state: domain.StateProcessed, wantResult: true},
		{name: "should return true when the state is rejected", state: domain.StateRejected, wantResult: true},
		{name: "should return true when the state is failed", state: domain.StateFailed, wantResult: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.wantResult, test.state.IsTerminal())
		})
	}
}

func validExternalParams(t *testing.T, kind domain.TransactionKind, amount string) domain.NewExternalTransactionParams {
	t.Helper()

	params := domain.NewExternalTransactionParams{
		ID:                    domain.NewID(),
		Kind:                  kind,
		ProviderID:            "provider-a",
		ExternalTransactionID: "transaction-123",
		IdempotencyKey:        "provider-a:transaction-123",
		PayloadHash:           "hash",
		WalletID:              domain.NewID(),
		PlayerID:              domain.NewID(),
		RoundID:               "round-987",
		GameID:                "fortune-chimp",
		Money:                 mustMoney(t, amount, "BRL"),
	}
	if kind.RequiresReference() {
		params.ReferenceExternalTransactionID = "transaction-122"
	}
	return params
}

func mustTransaction(t *testing.T, kind domain.TransactionKind, amount string) *domain.WagerTransaction {
	t.Helper()

	transaction, err := domain.NewExternalTransaction(validExternalParams(t, kind, amount))
	require.NoError(t, err)
	return transaction
}
