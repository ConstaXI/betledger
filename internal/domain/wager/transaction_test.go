package wager_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/domaintest"
	"github.com/davibanfi/betledger/internal/domain/money"
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
		{
			name: "should return INVALID_INPUT when createdAt is missing", kind: wager.KindBet, amount: "25.00",
			mutate: func(p *wager.NewExternalParams) { p.CreatedAt = time.Time{} }, wantErr: domain.FailureCodeInvalidInput,
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

func TestNewOpening(t *testing.T) {
	t.Parallel()

	id := domain.NewID()
	walletID := domain.NewID()
	playerID := domain.NewID()
	initial := domaintest.MustParseMoney(t, "1000.00", "BRL")

	tests := []struct {
		name           string
		id             domain.ID
		walletID       domain.ID
		playerID       domain.ID
		initialBalance money.Money
		createdAt      time.Time
		wantResult     *wager.Transaction
		wantErr        error
	}{
		{
			name:           "should accept when the opening has a positive balance, without provider metadata",
			id:             id,
			walletID:       walletID,
			playerID:       playerID,
			initialBalance: initial,
			createdAt:      domaintest.FixedNow,
			wantResult: domaintest.MustRehydrateTransaction(t, wager.RehydrateParams{
				ID:            id,
				Kind:          wager.KindOpening,
				State:         wager.StateProcessed,
				WalletID:      walletID,
				PlayerID:      playerID,
				Money:         initial,
				ResultBalance: &initial,
			}),
		},
		{
			name:           "should return INVALID_AMOUNT when the opening has a zero balance",
			id:             id,
			walletID:       walletID,
			playerID:       playerID,
			initialBalance: domaintest.MustParseMoney(t, "0.00", "BRL"),
			createdAt:      domaintest.FixedNow,
			wantErr:        domain.FailureCodeInvalidAmount,
		},
		{
			name:           "should return INVALID_INPUT when the transaction id is nil",
			id:             domain.NilID,
			walletID:       walletID,
			playerID:       playerID,
			initialBalance: initial,
			createdAt:      domaintest.FixedNow,
			wantErr:        domain.FailureCodeInvalidInput,
		},
		{
			name:           "should return INVALID_INPUT when the wallet id is nil",
			id:             id,
			walletID:       domain.NilID,
			playerID:       playerID,
			initialBalance: initial,
			createdAt:      domaintest.FixedNow,
			wantErr:        domain.FailureCodeInvalidInput,
		},
		{
			name:           "should return INVALID_INPUT when the player id is nil",
			id:             id,
			walletID:       walletID,
			playerID:       domain.NilID,
			initialBalance: initial,
			createdAt:      domaintest.FixedNow,
			wantErr:        domain.FailureCodeInvalidInput,
		},
		{
			name:           "should return INVALID_INPUT when createdAt is missing",
			id:             id,
			walletID:       walletID,
			playerID:       playerID,
			initialBalance: initial,
			wantErr:        domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := wager.NewOpening(test.id, test.walletID, test.playerID, test.initialBalance, test.createdAt)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestTransactionMarkProcessed(t *testing.T) {
	t.Parallel()

	later := domaintest.FixedNow.Add(time.Minute)

	pending := func(*wager.Transaction) error { return nil }
	processed := func(transaction *wager.Transaction) error {
		return transaction.MarkProcessed(domaintest.MustParseMoney(t, "75.00", "BRL"), domaintest.FixedNow)
	}
	rejected := func(transaction *wager.Transaction) error {
		return transaction.MarkRejected(domain.FailureCodeInsufficientFunds, domaintest.FixedNow)
	}
	waiting := func(transaction *wager.Transaction) error {
		return transaction.MarkPendingReference(domaintest.FixedNow)
	}

	tests := []struct {
		name              string
		kind              wager.Kind
		prepare           func(transaction *wager.Transaction) error
		balance           money.Money
		at                time.Time
		wantErr           error
		wantState         wager.State
		wantResultBalance money.Money
		wantHasBalance    bool
		wantUpdatedAt     time.Time
	}{
		{
			name:              "should accept when the operation is pending",
			kind:              wager.KindBet,
			prepare:           pending,
			balance:           domaintest.MustParseMoney(t, "50.00", "BRL"),
			at:                later,
			wantState:         wager.StateProcessed,
			wantResultBalance: domaintest.MustParseMoney(t, "50.00", "BRL"),
			wantHasBalance:    true,
			wantUpdatedAt:     later,
		},
		{
			name:              "should accept when the operation waited for its reference",
			kind:              wager.KindRefund,
			prepare:           waiting,
			balance:           domaintest.MustParseMoney(t, "125.00", "BRL"),
			at:                later,
			wantState:         wager.StateProcessed,
			wantResultBalance: domaintest.MustParseMoney(t, "125.00", "BRL"),
			wantHasBalance:    true,
			wantUpdatedAt:     later,
		},
		{
			name:              "should return INVALID_STATE_TRANSITION when the operation was already processed",
			kind:              wager.KindBet,
			prepare:           processed,
			balance:           domaintest.MustParseMoney(t, "10.00", "BRL"),
			at:                later,
			wantErr:           domain.FailureCodeInvalidStateTransition,
			wantState:         wager.StateProcessed,
			wantResultBalance: domaintest.MustParseMoney(t, "75.00", "BRL"),
			wantHasBalance:    true,
			wantUpdatedAt:     domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_STATE_TRANSITION when the operation was rejected",
			kind:          wager.KindBet,
			prepare:       rejected,
			balance:       domaintest.MustParseMoney(t, "10.00", "BRL"),
			at:            later,
			wantErr:       domain.FailureCodeInvalidStateTransition,
			wantState:     wager.StateRejected,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return CURRENCY_MISMATCH when the balance is in another currency",
			kind:          wager.KindBet,
			prepare:       pending,
			balance:       domaintest.MustParseMoney(t, "50.00", "USD"),
			at:            later,
			wantErr:       domain.FailureCodeCurrencyMismatch,
			wantState:     wager.StatePending,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_INPUT when the balance is uninitialized",
			kind:          wager.KindBet,
			prepare:       pending,
			at:            later,
			wantErr:       domain.FailureCodeInvalidInput,
			wantState:     wager.StatePending,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_INPUT when the instant is missing",
			kind:          wager.KindBet,
			prepare:       pending,
			balance:       domaintest.MustParseMoney(t, "50.00", "BRL"),
			wantErr:       domain.FailureCodeInvalidInput,
			wantState:     wager.StatePending,
			wantUpdatedAt: domaintest.FixedNow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			transaction := domaintest.MustExternalTransaction(t, test.kind, "25.00")
			require.NoError(t, test.prepare(transaction))

			err := transaction.MarkProcessed(test.balance, test.at)

			balance, hasBalance := transaction.ResultBalance()
			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantUpdatedAt, transaction.UpdatedAt())
			assert.Equal(t, test.wantState, transaction.State())
			assert.Equal(t, test.wantResultBalance, balance)
			assert.Equal(t, test.wantHasBalance, hasBalance)
		})
	}
}

func TestTransactionMarkRejected(t *testing.T) {
	t.Parallel()

	later := domaintest.FixedNow.Add(time.Minute)

	pending := func(*wager.Transaction) error { return nil }
	processed := func(transaction *wager.Transaction) error {
		return transaction.MarkProcessed(domaintest.MustParseMoney(t, "75.00", "BRL"), domaintest.FixedNow)
	}
	waiting := func(transaction *wager.Transaction) error {
		return transaction.MarkPendingReference(domaintest.FixedNow)
	}

	tests := []struct {
		name            string
		kind            wager.Kind
		prepare         func(transaction *wager.Transaction) error
		code            domain.FailureCode
		at              time.Time
		wantErr         error
		wantState       wager.State
		wantFailureCode domain.FailureCode
		wantUpdatedAt   time.Time
	}{
		{
			name:            "should accept when the operation is pending",
			kind:            wager.KindBet,
			prepare:         pending,
			code:            domain.FailureCodeInsufficientFunds,
			at:              later,
			wantState:       wager.StateRejected,
			wantFailureCode: domain.FailureCodeInsufficientFunds,
			wantUpdatedAt:   later,
		},
		{
			name:            "should accept when the operation waited for its reference",
			kind:            wager.KindRefund,
			prepare:         waiting,
			code:            domain.FailureCodeReferenceNotFound,
			at:              later,
			wantState:       wager.StateRejected,
			wantFailureCode: domain.FailureCodeReferenceNotFound,
			wantUpdatedAt:   later,
		},
		{
			name:          "should return INVALID_INPUT when the failure code is empty",
			kind:          wager.KindBet,
			prepare:       pending,
			at:            later,
			wantErr:       domain.FailureCodeInvalidInput,
			wantState:     wager.StatePending,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_STATE_TRANSITION when the operation was already processed",
			kind:          wager.KindBet,
			prepare:       processed,
			code:          domain.FailureCodeInsufficientFunds,
			at:            later,
			wantErr:       domain.FailureCodeInvalidStateTransition,
			wantState:     wager.StateProcessed,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_INPUT when the instant is missing",
			kind:          wager.KindBet,
			prepare:       pending,
			code:          domain.FailureCodeInsufficientFunds,
			wantErr:       domain.FailureCodeInvalidInput,
			wantState:     wager.StatePending,
			wantUpdatedAt: domaintest.FixedNow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			transaction := domaintest.MustExternalTransaction(t, test.kind, "25.00")
			require.NoError(t, test.prepare(transaction))

			err := transaction.MarkRejected(test.code, test.at)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantUpdatedAt, transaction.UpdatedAt())
			assert.Equal(t, test.wantState, transaction.State())
			assert.Equal(t, test.wantFailureCode, transaction.FailureCode())
		})
	}
}

func TestTransactionMarkFailed(t *testing.T) {
	t.Parallel()

	later := domaintest.FixedNow.Add(time.Minute)

	pending := func(*wager.Transaction) error { return nil }
	rejected := func(transaction *wager.Transaction) error {
		return transaction.MarkRejected(domain.FailureCodeInsufficientFunds, domaintest.FixedNow)
	}

	tests := []struct {
		name            string
		prepare         func(transaction *wager.Transaction) error
		code            domain.FailureCode
		at              time.Time
		wantErr         error
		wantState       wager.State
		wantFailureCode domain.FailureCode
		wantUpdatedAt   time.Time
	}{
		{
			name:            "should accept when the operation is pending",
			prepare:         pending,
			code:            domain.FailureCodeReferenceNotFound,
			at:              later,
			wantState:       wager.StateFailed,
			wantFailureCode: domain.FailureCodeReferenceNotFound,
			wantUpdatedAt:   later,
		},
		{
			name:          "should return INVALID_INPUT when the failure code is empty",
			prepare:       pending,
			at:            later,
			wantErr:       domain.FailureCodeInvalidInput,
			wantState:     wager.StatePending,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:            "should return INVALID_STATE_TRANSITION when the operation was rejected",
			prepare:         rejected,
			code:            domain.FailureCodeReferenceNotFound,
			at:              later,
			wantErr:         domain.FailureCodeInvalidStateTransition,
			wantState:       wager.StateRejected,
			wantFailureCode: domain.FailureCodeInsufficientFunds,
			wantUpdatedAt:   domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_INPUT when the instant is missing",
			prepare:       pending,
			code:          domain.FailureCodeReferenceNotFound,
			wantErr:       domain.FailureCodeInvalidInput,
			wantState:     wager.StatePending,
			wantUpdatedAt: domaintest.FixedNow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			transaction := domaintest.MustExternalTransaction(t, wager.KindBet, "25.00")
			require.NoError(t, test.prepare(transaction))

			err := transaction.MarkFailed(test.code, test.at)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantUpdatedAt, transaction.UpdatedAt())
			assert.Equal(t, test.wantState, transaction.State())
			assert.Equal(t, test.wantFailureCode, transaction.FailureCode())
		})
	}
}

func TestTransactionMarkPendingReference(t *testing.T) {
	t.Parallel()

	later := domaintest.FixedNow.Add(time.Minute)

	pending := func(*wager.Transaction) error { return nil }
	processed := func(transaction *wager.Transaction) error {
		return transaction.MarkProcessed(domaintest.MustParseMoney(t, "75.00", "BRL"), domaintest.FixedNow)
	}

	tests := []struct {
		name          string
		kind          wager.Kind
		prepare       func(transaction *wager.Transaction) error
		at            time.Time
		wantErr       error
		wantState     wager.State
		wantUpdatedAt time.Time
	}{
		{
			name:          "should accept when a pending reversal waits for its reference",
			kind:          wager.KindRefund,
			prepare:       pending,
			at:            later,
			wantState:     wager.StatePendingReference,
			wantUpdatedAt: later,
		},
		{
			name:          "should return INVALID_STATE_TRANSITION when the operation refers to nothing",
			kind:          wager.KindBet,
			prepare:       pending,
			at:            later,
			wantErr:       domain.FailureCodeInvalidStateTransition,
			wantState:     wager.StatePending,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_STATE_TRANSITION when the reversal was already processed",
			kind:          wager.KindRefund,
			prepare:       processed,
			at:            later,
			wantErr:       domain.FailureCodeInvalidStateTransition,
			wantState:     wager.StateProcessed,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_INPUT when the instant is missing",
			kind:          wager.KindRefund,
			prepare:       pending,
			wantErr:       domain.FailureCodeInvalidInput,
			wantState:     wager.StatePending,
			wantUpdatedAt: domaintest.FixedNow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			transaction := domaintest.MustExternalTransaction(t, test.kind, "25.00")
			require.NoError(t, test.prepare(transaction))

			err := transaction.MarkPendingReference(test.at)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantUpdatedAt, transaction.UpdatedAt())
			assert.Equal(t, test.wantState, transaction.State())
		})
	}
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
		return reference.MarkProcessed(domaintest.MustParseMoney(t, "75.00", "BRL"), domaintest.FixedNow)
	}
	rejected := func(reference *wager.Transaction) error {
		return reference.MarkRejected(domain.FailureCodeInsufficientFunds, domaintest.FixedNow)
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
				return reference.MarkProcessed(domaintest.MustParseMoney(t, "75.00", "USD"), domaintest.FixedNow)
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

func TestTransactionRecordMissingReference(t *testing.T) {
	t.Parallel()

	later := domaintest.FixedNow.Add(time.Minute)

	pending := func(*wager.Transaction) error { return nil }
	waiting := func(transaction *wager.Transaction) error {
		return transaction.MarkPendingReference(domaintest.FixedNow)
	}
	waitedTwice := func(transaction *wager.Transaction) error {
		return errors.Join(
			transaction.MarkPendingReference(domaintest.FixedNow),
			transaction.RecordMissingReference(8, domaintest.FixedNow),
			transaction.RecordMissingReference(8, domaintest.FixedNow),
		)
	}

	tests := []struct {
		name            string
		prepare         func(transaction *wager.Transaction) error
		maxAttempts     int
		at              time.Time
		wantErr         error
		wantState       wager.State
		wantFailureCode domain.FailureCode
		wantAttempts    int
		wantUpdatedAt   time.Time
	}{
		{
			name:          "should accept when attempts remain, keeping the operation waiting",
			prepare:       waiting,
			maxAttempts:   3,
			at:            later,
			wantState:     wager.StatePendingReference,
			wantAttempts:  1,
			wantUpdatedAt: later,
		},
		{
			name:            "should report REFERENCE_NOT_FOUND when the last attempt is spent",
			prepare:         waitedTwice,
			maxAttempts:     3,
			at:              later,
			wantState:       wager.StateRejected,
			wantFailureCode: domain.FailureCodeReferenceNotFound,
			wantAttempts:    3,
			wantUpdatedAt:   later,
		},
		{
			name:            "should report REFERENCE_NOT_FOUND when a single attempt is allowed",
			prepare:         waiting,
			maxAttempts:     1,
			at:              later,
			wantState:       wager.StateRejected,
			wantFailureCode: domain.FailureCodeReferenceNotFound,
			wantAttempts:    1,
			wantUpdatedAt:   later,
		},
		{
			name:          "should return INVALID_STATE_TRANSITION when the operation is not waiting",
			prepare:       pending,
			maxAttempts:   3,
			at:            later,
			wantErr:       domain.FailureCodeInvalidStateTransition,
			wantState:     wager.StatePending,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_INPUT when no attempt is allowed",
			prepare:       waiting,
			maxAttempts:   0,
			at:            later,
			wantErr:       domain.FailureCodeInvalidInput,
			wantState:     wager.StatePendingReference,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_INPUT when the instant is missing",
			prepare:       waiting,
			maxAttempts:   3,
			wantErr:       domain.FailureCodeInvalidInput,
			wantState:     wager.StatePendingReference,
			wantUpdatedAt: domaintest.FixedNow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			transaction := domaintest.MustExternalTransaction(t, wager.KindRefund, "25.00")
			require.NoError(t, test.prepare(transaction))

			err := transaction.RecordMissingReference(test.maxAttempts, test.at)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantUpdatedAt, transaction.UpdatedAt())
			assert.Equal(t, test.wantState, transaction.State())
			assert.Equal(t, test.wantFailureCode, transaction.FailureCode())
			assert.Equal(t, test.wantAttempts, transaction.ReferenceAttempts())
		})
	}
}
