package wager_test

import (
	"errors"
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

func TestTransactionResolveReference(t *testing.T) {
	t.Parallel()

	base := domaintest.ValidExternalParams(t, wager.KindBet, "25.00")
	processed := func(reference *wager.Transaction) error {
		return reference.MarkProcessed(domaintest.MustParseMoney(t, "75.00", "BRL"))
	}
	rejected := func(reference *wager.Transaction) error {
		return reference.MarkRejected(domain.FailureCodeInsufficientFunds)
	}
	pending := func(*wager.Transaction) error { return nil }
	unchanged := func(*wager.NewExternalParams) {}

	tests := []struct {
		name            string
		kind            wager.Kind
		amount          string
		referenceKind   wager.Kind
		referenceAmount string
		mutateReference func(params *wager.NewExternalParams)
		conclude        func(reference *wager.Transaction) error
		wantErr         error
		wantRejected    bool
		wantResolved    bool
	}{
		{
			name:            "should accept when a win refers to a bet of the round",
			kind:            wager.KindWin,
			amount:          "40.00",
			referenceKind:   wager.KindBet,
			referenceAmount: "25.00",
			mutateReference: unchanged,
			conclude:        processed,
			wantResolved:    true,
		},
		{
			name:            "should accept when a refund returns the whole bet",
			kind:            wager.KindRefund,
			amount:          "25.00",
			referenceKind:   wager.KindBet,
			referenceAmount: "25.00",
			mutateReference: unchanged,
			conclude:        processed,
			wantResolved:    true,
		},
		{
			name:            "should accept when a rollback undoes a bet",
			kind:            wager.KindRollback,
			amount:          "25.00",
			referenceKind:   wager.KindBet,
			referenceAmount: "25.00",
			mutateReference: unchanged,
			conclude:        processed,
			wantResolved:    true,
		},
		{
			name:            "should accept when a rollback undoes a win",
			kind:            wager.KindRollback,
			amount:          "25.00",
			referenceKind:   wager.KindWin,
			referenceAmount: "25.00",
			mutateReference: unchanged,
			conclude:        processed,
			wantResolved:    true,
		},
		{
			name:            "should accept when a rollback undoes a refund",
			kind:            wager.KindRollback,
			amount:          "25.00",
			referenceKind:   wager.KindRefund,
			referenceAmount: "25.00",
			mutateReference: func(params *wager.NewExternalParams) { params.ReferenceExternalTransactionID = "transaction-121" },
			conclude:        processed,
			wantResolved:    true,
		},
		{
			name:            "should return REFERENCE_MISMATCH when a refund refers to a win",
			kind:            wager.KindRefund,
			amount:          "25.00",
			referenceKind:   wager.KindWin,
			referenceAmount: "25.00",
			mutateReference: unchanged,
			conclude:        processed,
			wantErr:         domain.FailureCodeReferenceMismatch,
			wantRejected:    true,
		},
		{
			name:            "should return REFERENCE_MISMATCH when a rollback refers to a loss",
			kind:            wager.KindRollback,
			amount:          "25.00",
			referenceKind:   wager.KindLoss,
			referenceAmount: "0.00",
			mutateReference: unchanged,
			conclude:        processed,
			wantErr:         domain.FailureCodeReferenceMismatch,
			wantRejected:    true,
		},
		{
			name:            "should return REFERENCE_NOT_PROCESSED when the reference was rejected",
			kind:            wager.KindRefund,
			amount:          "25.00",
			referenceKind:   wager.KindBet,
			referenceAmount: "25.00",
			mutateReference: unchanged,
			conclude:        rejected,
			wantErr:         domain.FailureCodeReferenceNotProcessed,
			wantRejected:    true,
		},
		{
			name:            "should return REFERENCE_NOT_PROCESSED when the reference is still pending",
			kind:            wager.KindRefund,
			amount:          "25.00",
			referenceKind:   wager.KindBet,
			referenceAmount: "25.00",
			mutateReference: unchanged,
			conclude:        pending,
			wantErr:         domain.FailureCodeReferenceNotProcessed,
			wantRejected:    true,
		},
		{
			name:            "should return REFERENCE_MISMATCH when the reference is from another round",
			kind:            wager.KindRefund,
			amount:          "25.00",
			referenceKind:   wager.KindBet,
			referenceAmount: "25.00",
			mutateReference: func(params *wager.NewExternalParams) { params.RoundID = "round-988" },
			conclude:        processed,
			wantErr:         domain.FailureCodeReferenceMismatch,
			wantRejected:    true,
		},
		{
			name:            "should return REFERENCE_MISMATCH when the reference is from another wallet",
			kind:            wager.KindRefund,
			amount:          "25.00",
			referenceKind:   wager.KindBet,
			referenceAmount: "25.00",
			mutateReference: func(params *wager.NewExternalParams) { params.WalletID = domain.NewID() },
			conclude:        processed,
			wantErr:         domain.FailureCodeReferenceMismatch,
			wantRejected:    true,
		},
		{
			name:            "should return REFERENCE_MISMATCH when the reference is from another player",
			kind:            wager.KindRefund,
			amount:          "25.00",
			referenceKind:   wager.KindBet,
			referenceAmount: "25.00",
			mutateReference: func(params *wager.NewExternalParams) { params.PlayerID = domain.NewID() },
			conclude:        processed,
			wantErr:         domain.FailureCodeReferenceMismatch,
			wantRejected:    true,
		},
		{
			name:            "should return REFERENCE_MISMATCH when the reference is in another currency",
			kind:            wager.KindRefund,
			amount:          "25.00",
			referenceKind:   wager.KindBet,
			referenceAmount: "25.00",
			mutateReference: func(params *wager.NewExternalParams) {
				params.Money = domaintest.MustParseMoney(t, "25.00", "USD")
			},
			conclude: func(reference *wager.Transaction) error {
				return reference.MarkProcessed(domaintest.MustParseMoney(t, "75.00", "USD"))
			},
			wantErr:      domain.FailureCodeReferenceMismatch,
			wantRejected: true,
		},
		{
			name:            "should return REFERENCE_AMOUNT_MISMATCH when a refund is partial",
			kind:            wager.KindRefund,
			amount:          "20.00",
			referenceKind:   wager.KindBet,
			referenceAmount: "25.00",
			mutateReference: unchanged,
			conclude:        processed,
			wantErr:         domain.FailureCodeReferenceAmountMismatch,
			wantRejected:    true,
		},
		{
			name:            "should return INVALID_INPUT when the reference belongs to another provider",
			kind:            wager.KindRefund,
			amount:          "25.00",
			referenceKind:   wager.KindBet,
			referenceAmount: "25.00",
			mutateReference: func(params *wager.NewExternalParams) { params.ProviderID = "provider-b" },
			conclude:        processed,
			wantErr:         domain.FailureCodeInvalidInput,
		},
		{
			name:            "should return INVALID_INPUT when the reference is another operation",
			kind:            wager.KindRefund,
			amount:          "25.00",
			referenceKind:   wager.KindBet,
			referenceAmount: "25.00",
			mutateReference: func(params *wager.NewExternalParams) { params.ExternalTransactionID = "transaction-999" },
			conclude:        processed,
			wantErr:         domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			operationParams := base
			operationParams.ID = domain.NewID()
			operationParams.Kind = test.kind
			operationParams.Money = domaintest.MustParseMoney(t, test.amount, "BRL")
			operationParams.ReferenceExternalTransactionID = "transaction-122"
			operation, err := wager.NewExternal(operationParams)
			require.NoError(t, err)

			referenceParams := base
			referenceParams.ID = domain.NewID()
			referenceParams.Kind = test.referenceKind
			referenceParams.ExternalTransactionID = "transaction-122"
			referenceParams.IdempotencyKey = "provider-a:transaction-122"
			referenceParams.Money = domaintest.MustParseMoney(t, test.referenceAmount, "BRL")
			test.mutateReference(&referenceParams)
			reference, err := wager.NewExternal(referenceParams)
			require.NoError(t, err)
			require.NoError(t, test.conclude(reference))

			err = operation.ResolveReference(reference)

			resolvedID, resolved := operation.ReferenceTransactionID()
			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantRejected, errors.Is(err, domain.ErrRejected))
			assert.Equal(t, test.wantResolved, resolved)
			assert.Equal(t, test.wantResolved, resolvedID == reference.ID())
		})
	}
}
