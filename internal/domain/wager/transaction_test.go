package wager_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/domaintest"
	"github.com/davibanfi/betledger/internal/domain/wager"
)

func TestParseExternalTransactionKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		value      string
		wantResult wager.Kind
		wantErr    error
	}{
		{name: "should accept when the kind is BET", value: "BET", wantResult: wager.KindBet},
		{name: "should accept when the kind is WIN", value: "WIN", wantResult: wager.KindWin},
		{name: "should accept when the kind is LOSS", value: "LOSS", wantResult: wager.KindLoss},
		{name: "should accept when the kind is REFUND", value: "REFUND", wantResult: wager.KindRefund},
		{name: "should accept when the kind is ROLLBACK", value: "ROLLBACK", wantResult: wager.KindRollback},
		{name: "should return TRANSACTION_KIND_NOT_ALLOWED when the kind is OPENING", value: "OPENING", wantErr: domain.FailureCodeKindNotAllowed},
		{name: "should return INVALID_INPUT when the kind is unknown", value: "UNKNOWN", wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the kind is empty", value: "", wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the kind is lowercase", value: "bet", wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := wager.ParseExternalKind(test.value)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestNewExternalTransaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    wager.Kind
		amount  string
		mutate  func(*wager.NewExternalParams)
		wantErr error
	}{
		{name: "should accept when BET has a positive amount", kind: wager.KindBet, amount: "25.00"},
		{name: "should accept when WIN has a positive amount", kind: wager.KindWin, amount: "25.00"},
		{name: "should accept when LOSS has a zero amount", kind: wager.KindLoss, amount: "0.00"},
		{name: "should accept when REFUND has a positive amount", kind: wager.KindRefund, amount: "25.00"},
		{name: "should accept when ROLLBACK has a positive amount", kind: wager.KindRollback, amount: "25.00"},

		{name: "should return INVALID_AMOUNT when BET has a zero amount", kind: wager.KindBet, amount: "0.00", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when WIN has a zero amount", kind: wager.KindWin, amount: "0.00", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when LOSS carries an amount", kind: wager.KindLoss, amount: "25.00", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when REFUND has a zero amount", kind: wager.KindRefund, amount: "0.00", wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_AMOUNT when ROLLBACK has a zero amount", kind: wager.KindRollback, amount: "0.00", wantErr: domain.FailureCodeInvalidAmount},

		{name: "should return TRANSACTION_KIND_NOT_ALLOWED when the kind is OPENING", kind: wager.KindOpening, amount: "25.00", wantErr: domain.FailureCodeKindNotAllowed},
		{name: "should return INVALID_INPUT when the kind is unknown", kind: "TRANSFER", amount: "25.00", wantErr: domain.FailureCodeInvalidInput},

		{
			name: "should return INVALID_INPUT when REFUND has no reference", kind: wager.KindRefund, amount: "25.00",
			mutate:  func(p *wager.NewExternalParams) { p.ReferenceExternalTransactionID = "" },
			wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when ROLLBACK has no reference", kind: wager.KindRollback, amount: "25.00",
			mutate:  func(p *wager.NewExternalParams) { p.ReferenceExternalTransactionID = "" },
			wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when BET carries a reference", kind: wager.KindBet, amount: "25.00",
			mutate:  func(p *wager.NewExternalParams) { p.ReferenceExternalTransactionID = "transaction-122" },
			wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when LOSS carries a reference", kind: wager.KindLoss, amount: "0.00",
			mutate:  func(p *wager.NewExternalParams) { p.ReferenceExternalTransactionID = "transaction-122" },
			wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should accept when WIN carries an optional reference", kind: wager.KindWin, amount: "25.00",
			mutate: func(p *wager.NewExternalParams) { p.ReferenceExternalTransactionID = "transaction-122" },
		},

		{
			name: "should return INVALID_INPUT when providerId is missing", kind: wager.KindBet, amount: "25.00",
			mutate: func(p *wager.NewExternalParams) { p.ProviderID = "" }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when externalTransactionId is missing", kind: wager.KindBet, amount: "25.00",
			mutate: func(p *wager.NewExternalParams) { p.ExternalTransactionID = "" }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when idempotencyKey is missing", kind: wager.KindBet, amount: "25.00",
			mutate: func(p *wager.NewExternalParams) { p.IdempotencyKey = "" }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when payloadHash is missing", kind: wager.KindBet, amount: "25.00",
			mutate: func(p *wager.NewExternalParams) { p.PayloadHash = "" }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when roundId is missing", kind: wager.KindBet, amount: "25.00",
			mutate: func(p *wager.NewExternalParams) { p.RoundID = "" }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when gameId is missing", kind: wager.KindBet, amount: "25.00",
			mutate: func(p *wager.NewExternalParams) { p.GameID = "" }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when walletId is nil", kind: wager.KindBet, amount: "25.00",
			mutate: func(p *wager.NewExternalParams) { p.WalletID = domain.NilID }, wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when playerId is nil", kind: wager.KindBet, amount: "25.00",
			mutate: func(p *wager.NewExternalParams) { p.PlayerID = domain.NilID }, wantErr: domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			params := domaintest.ValidExternalParams(t, test.kind, test.amount)
			if test.mutate != nil {
				test.mutate(&params)
			}

			_, err := wager.NewExternal(params)

			assert.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestTransactionStateMachine(t *testing.T) {
	t.Parallel()

	transaction := domaintest.MustExternalTransaction(t, wager.KindBet, "25.00")
	assert.Equal(t, wager.StatePending, transaction.State())

	require.NoError(t, transaction.MarkProcessed(domaintest.MustParseMoney(t, "75.00", "BRL")))

	balance, ok := transaction.ResultBalance()
	assert.True(t, ok)
	assert.Equal(t, domaintest.MustParseMoney(t, "75.00", "BRL"), balance)

	assert.ErrorIs(t, transaction.MarkRejected(domain.FailureCodeInsufficientFunds), domain.FailureCodeInvalidStateTransition)
	assert.ErrorIs(t, transaction.MarkProcessed(domaintest.MustParseMoney(t, "10.00", "BRL")), domain.FailureCodeInvalidStateTransition)
}

func TestTransactionPendingReferenceFlow(t *testing.T) {
	t.Parallel()

	transaction := domaintest.MustExternalTransaction(t, wager.KindRefund, "25.00")

	require.NoError(t, transaction.MarkPendingReference())
	assert.Equal(t, wager.StatePendingReference, transaction.State())

	referenceID := domain.NewID()
	require.NoError(t, transaction.ResolveReference(referenceID))

	resolved, ok := transaction.ReferenceTransactionID()
	assert.True(t, ok)
	assert.Equal(t, referenceID, resolved)

	assert.NoError(t, transaction.MarkProcessed(domaintest.MustParseMoney(t, "125.00", "BRL")))
}

func TestTransactionRejectionRequiresFailureCode(t *testing.T) {
	t.Parallel()

	transaction := domaintest.MustExternalTransaction(t, wager.KindBet, "25.00")

	assert.ErrorIs(t, transaction.MarkRejected(""), domain.FailureCodeInvalidInput)

	require.NoError(t, transaction.MarkRejected(domain.FailureCodeInsufficientFunds))
	assert.Equal(t, domain.FailureCodeInsufficientFunds, transaction.FailureCode())
	assert.True(t, transaction.State().IsTerminal())
}

func TestNewOpeningTransaction(t *testing.T) {
	t.Parallel()

	initial := domaintest.MustParseMoney(t, "1000.00", "BRL")

	transaction, err := wager.NewOpening(domain.NewID(), domain.NewID(), domain.NewID(), initial)

	require.NoError(t, err)
	assert.Equal(t, wager.StateProcessed, transaction.State())
	assert.False(t, transaction.Kind().IsExternal())
	assert.Empty(t, transaction.ProviderID())
	assert.Empty(t, transaction.ExternalTransactionID())
	assert.Empty(t, transaction.IdempotencyKey())
	assert.Empty(t, transaction.PayloadHash())
	assert.Empty(t, transaction.RoundID())
	assert.Empty(t, transaction.GameID())

	zero := domaintest.MustParseMoney(t, "0.00", "BRL")
	_, err = wager.NewOpening(domain.NewID(), domain.NewID(), domain.NewID(), zero)
	assert.ErrorIs(t, err, domain.FailureCodeInvalidAmount)
}

func TestTransactionStateCanTransitionTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		from       wager.State
		to         wager.State
		wantResult bool
	}{
		{name: "should return true when transitioning from pending to pending reference", from: wager.StatePending, to: wager.StatePendingReference, wantResult: true},
		{name: "should return true when transitioning from pending to processed", from: wager.StatePending, to: wager.StateProcessed, wantResult: true},
		{name: "should return true when transitioning from pending to rejected", from: wager.StatePending, to: wager.StateRejected, wantResult: true},
		{name: "should return true when transitioning from pending to failed", from: wager.StatePending, to: wager.StateFailed, wantResult: true},
		{name: "should return true when transitioning from pending reference to processed", from: wager.StatePendingReference, to: wager.StateProcessed, wantResult: true},
		{name: "should return true when transitioning from pending reference to rejected", from: wager.StatePendingReference, to: wager.StateRejected, wantResult: true},

		{name: "should return false when transitioning from pending reference to itself", from: wager.StatePendingReference, to: wager.StatePendingReference, wantResult: false},
		{name: "should return false when transitioning from pending reference back to pending", from: wager.StatePendingReference, to: wager.StatePending, wantResult: false},
		{name: "should return false when transitioning from processed to rejected", from: wager.StateProcessed, to: wager.StateRejected, wantResult: false},
		{name: "should return false when transitioning from processed to processed", from: wager.StateProcessed, to: wager.StateProcessed, wantResult: false},
		{name: "should return false when transitioning from rejected to processed", from: wager.StateRejected, to: wager.StateProcessed, wantResult: false},
		{name: "should return false when transitioning from failed to processed", from: wager.StateFailed, to: wager.StateProcessed, wantResult: false},
		{name: "should return false when the target state is unknown", from: wager.StatePending, to: "ARCHIVED", wantResult: false},
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
		state      wager.State
		wantResult bool
	}{
		{name: "should return false when the state is pending", state: wager.StatePending, wantResult: false},
		{name: "should return false when the state is pending reference", state: wager.StatePendingReference, wantResult: false},
		{name: "should return true when the state is processed", state: wager.StateProcessed, wantResult: true},
		{name: "should return true when the state is rejected", state: wager.StateRejected, wantResult: true},
		{name: "should return true when the state is failed", state: wager.StateFailed, wantResult: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.wantResult, test.state.IsTerminal())
		})
	}
}
