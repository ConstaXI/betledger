package wallet_test

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
	"github.com/davibanfi/betledger/internal/domain/wallet"
)

func TestOpen(t *testing.T) {
	t.Parallel()

	id := domain.NewID()
	playerID := domain.NewID()
	negative, err := money.ParseSigned("-1.00", "BRL")
	require.NoError(t, err)

	tests := []struct {
		name           string
		id             domain.ID
		playerID       domain.ID
		initialBalance money.Money
		openedAt       time.Time
		wantResult     *wallet.Wallet
		wantErr        error
	}{
		{
			name:           "should accept when the initial balance is positive",
			id:             id,
			playerID:       playerID,
			initialBalance: domaintest.MustParseMoney(t, "1000.00", "BRL"),
			openedAt:       domaintest.FixedNow,
			wantResult:     domaintest.MustRehydrateWallet(t, id, playerID, "1000.00", 1),
		},
		{
			name:           "should accept when the initial balance is zero",
			id:             id,
			playerID:       playerID,
			initialBalance: domaintest.MustParseMoney(t, "0.00", "BRL"),
			openedAt:       domaintest.FixedNow,
			wantResult:     domaintest.MustRehydrateWallet(t, id, playerID, "0.00", 1),
		},
		{
			name:           "should return INVALID_AMOUNT when the initial balance is negative",
			id:             id,
			playerID:       playerID,
			initialBalance: negative,
			openedAt:       domaintest.FixedNow,
			wantErr:        domain.FailureCodeInvalidAmount,
		},
		{
			name:     "should return INVALID_INPUT when the initial balance is uninitialized",
			id:       id,
			playerID: playerID,
			openedAt: domaintest.FixedNow,
			wantErr:  domain.FailureCodeInvalidInput,
		},
		{
			name:           "should return INVALID_INPUT when the wallet id is nil",
			id:             domain.NilID,
			playerID:       playerID,
			initialBalance: domaintest.MustParseMoney(t, "1000.00", "BRL"),
			openedAt:       domaintest.FixedNow,
			wantErr:        domain.FailureCodeInvalidInput,
		},
		{
			name:           "should return INVALID_INPUT when the player id is nil",
			id:             id,
			playerID:       domain.NilID,
			initialBalance: domaintest.MustParseMoney(t, "1000.00", "BRL"),
			openedAt:       domaintest.FixedNow,
			wantErr:        domain.FailureCodeInvalidInput,
		},
		{
			name:           "should return INVALID_INPUT when the opening instant is missing",
			id:             id,
			playerID:       playerID,
			initialBalance: domaintest.MustParseMoney(t, "1000.00", "BRL"),
			wantErr:        domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := wallet.Open(test.id, test.playerID, test.initialBalance, test.openedAt)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestWalletOpeningLedgerEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		initialBalance string
		transactionID  domain.ID
		at             time.Time
		wantErr        error
		wantEntry      bool
	}{
		{
			name:           "should accept when the opening has a positive balance",
			initialBalance: "1000.00",
			transactionID:  domain.NewID(),
			at:             domaintest.FixedNow,
			wantEntry:      true,
		},
		{
			name:           "should return INVALID_AMOUNT when the opening has a zero balance",
			initialBalance: "0.00",
			transactionID:  domain.NewID(),
			at:             domaintest.FixedNow,
			wantErr:        domain.FailureCodeInvalidAmount,
		},
		{
			name:           "should return INVALID_INPUT when the transaction id is nil",
			initialBalance: "1000.00",
			transactionID:  domain.NilID,
			at:             domaintest.FixedNow,
			wantErr:        domain.FailureCodeInvalidInput,
		},
		{
			name:           "should return INVALID_INPUT when the instant is missing",
			initialBalance: "1000.00",
			transactionID:  domain.NewID(),
			wantErr:        domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			initial := domaintest.MustParseMoney(t, test.initialBalance, "BRL")
			w := domaintest.MustOpenWallet(t, initial)

			got, err := w.OpeningLedgerEntry(test.transactionID, test.at)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantEntry, got != nil)
			assert.Equal(t, initial, w.Balance(), "the opening entry never moves the balance again")
			assert.Equal(t, int64(1), w.Version(), "the opening entry never bumps the version")
		})
	}
}

func TestWalletCredit(t *testing.T) {
	t.Parallel()

	later := domaintest.FixedNow.Add(time.Minute)

	tests := []struct {
		name          string
		amount        money.Money
		transactionID domain.ID
		at            time.Time
		wantErr       error
		wantEntry     bool
		wantBalance   money.Money
		wantVersion   int64
		wantUpdatedAt time.Time
	}{
		{
			name:          "should accept when the credit matches the wallet currency",
			amount:        domaintest.MustParseMoney(t, "5.00", "BRL"),
			transactionID: domain.NewID(),
			at:            later,
			wantEntry:     true,
			wantBalance:   domaintest.MustParseMoney(t, "105.00", "BRL"),
			wantVersion:   2,
			wantUpdatedAt: later,
		},
		{
			name:          "should return CURRENCY_MISMATCH when the credit is in a foreign currency",
			amount:        domaintest.MustParseMoney(t, "5.00", "USD"),
			transactionID: domain.NewID(),
			at:            later,
			wantErr:       domain.FailureCodeCurrencyMismatch,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_AMOUNT when the credit is zero",
			amount:        domaintest.MustParseMoney(t, "0.00", "BRL"),
			transactionID: domain.NewID(),
			at:            later,
			wantErr:       domain.FailureCodeInvalidAmount,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_INPUT when the credit is uninitialized",
			transactionID: domain.NewID(),
			at:            later,
			wantErr:       domain.FailureCodeInvalidInput,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_INPUT when the transaction id is nil",
			amount:        domaintest.MustParseMoney(t, "5.00", "BRL"),
			at:            later,
			wantErr:       domain.FailureCodeInvalidInput,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_INPUT when the instant is missing",
			amount:        domaintest.MustParseMoney(t, "5.00", "BRL"),
			transactionID: domain.NewID(),
			wantErr:       domain.FailureCodeInvalidInput,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := domaintest.MustOpenWallet(t, domaintest.MustParseMoney(t, "100.00", "BRL"))

			got, err := w.Credit(test.amount, test.transactionID, test.at)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantEntry, got != nil)
			assert.Equal(t, test.wantBalance, w.Balance())
			assert.Equal(t, test.wantVersion, w.Version())
			assert.Equal(t, test.wantUpdatedAt, w.UpdatedAt())
		})
	}
}

func TestWalletDebit(t *testing.T) {
	t.Parallel()

	later := domaintest.FixedNow.Add(time.Minute)

	tests := []struct {
		name                  string
		amount                money.Money
		transactionID         domain.ID
		at                    time.Time
		wantErr               error
		wantInsufficientFunds bool
		wantEntry             bool
		wantBalance           money.Money
		wantVersion           int64
		wantUpdatedAt         time.Time
	}{
		{
			name:          "should accept when the balance covers the debit",
			amount:        domaintest.MustParseMoney(t, "80.00", "BRL"),
			transactionID: domain.NewID(),
			at:            later,
			wantEntry:     true,
			wantBalance:   domaintest.MustParseMoney(t, "20.00", "BRL"),
			wantVersion:   2,
			wantUpdatedAt: later,
		},
		{
			name:          "should accept when the debit takes the whole balance",
			amount:        domaintest.MustParseMoney(t, "100.00", "BRL"),
			transactionID: domain.NewID(),
			at:            later,
			wantEntry:     true,
			wantBalance:   domaintest.MustParseMoney(t, "0.00", "BRL"),
			wantVersion:   2,
			wantUpdatedAt: later,
		},
		{
			name:                  "should return INSUFFICIENT_FUNDS when the debit exceeds the balance",
			amount:                domaintest.MustParseMoney(t, "100.01", "BRL"),
			transactionID:         domain.NewID(),
			at:                    later,
			wantErr:               domain.FailureCodeInsufficientFunds,
			wantInsufficientFunds: true,
			wantBalance:           domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:           1,
			wantUpdatedAt:         domaintest.FixedNow,
		},
		{
			name:          "should return CURRENCY_MISMATCH when the debit is in a foreign currency",
			amount:        domaintest.MustParseMoney(t, "5.00", "USD"),
			transactionID: domain.NewID(),
			at:            later,
			wantErr:       domain.FailureCodeCurrencyMismatch,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_AMOUNT when the debit is zero",
			amount:        domaintest.MustParseMoney(t, "0.00", "BRL"),
			transactionID: domain.NewID(),
			at:            later,
			wantErr:       domain.FailureCodeInvalidAmount,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_INPUT when the instant is missing",
			amount:        domaintest.MustParseMoney(t, "5.00", "BRL"),
			transactionID: domain.NewID(),
			wantErr:       domain.FailureCodeInvalidInput,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := domaintest.MustOpenWallet(t, domaintest.MustParseMoney(t, "100.00", "BRL"))

			got, err := w.Debit(test.amount, test.transactionID, test.at)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantInsufficientFunds, errors.Is(err, domain.ErrInsufficientFunds))
			assert.Equal(t, test.wantInsufficientFunds, errors.Is(err, domain.ErrRejected))
			assert.Equal(t, test.wantEntry, got != nil)
			assert.Equal(t, test.wantBalance, w.Balance())
			assert.Equal(t, test.wantVersion, w.Version())
			assert.Equal(t, test.wantUpdatedAt, w.UpdatedAt())
		})
	}
}

func TestWalletApply(t *testing.T) {
	t.Parallel()

	later := domaintest.FixedNow.Add(time.Minute)

	tests := []struct {
		name          string
		kind          wager.Kind
		amount        string
		referenceKind wager.Kind
		mutate        func(params *wager.NewExternalParams)
		at            time.Time
		wantErr       error
		wantRejected  bool
		wantEntry     bool
		wantBalance   money.Money
		wantVersion   int64
		wantUpdatedAt time.Time
	}{
		{
			name:          "should accept when the balance covers the bet",
			kind:          wager.KindBet,
			amount:        "25.00",
			mutate:        func(*wager.NewExternalParams) {},
			at:            later,
			wantEntry:     true,
			wantBalance:   domaintest.MustParseMoney(t, "75.00", "BRL"),
			wantVersion:   2,
			wantUpdatedAt: later,
		},
		{
			name:          "should accept when the bet takes the whole balance",
			kind:          wager.KindBet,
			amount:        "100.00",
			mutate:        func(*wager.NewExternalParams) {},
			at:            later,
			wantEntry:     true,
			wantBalance:   domaintest.MustParseMoney(t, "0.00", "BRL"),
			wantVersion:   2,
			wantUpdatedAt: later,
		},
		{
			name:          "should return INSUFFICIENT_FUNDS when the bet exceeds the balance",
			kind:          wager.KindBet,
			amount:        "100.01",
			mutate:        func(*wager.NewExternalParams) {},
			at:            later,
			wantErr:       domain.FailureCodeInsufficientFunds,
			wantRejected:  true,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should accept when a win credits the wallet",
			kind:          wager.KindWin,
			amount:        "40.00",
			mutate:        func(*wager.NewExternalParams) {},
			at:            later,
			wantEntry:     true,
			wantBalance:   domaintest.MustParseMoney(t, "140.00", "BRL"),
			wantVersion:   2,
			wantUpdatedAt: later,
		},
		{
			name:          "should accept when a loss moves nothing",
			kind:          wager.KindLoss,
			amount:        "0.00",
			mutate:        func(*wager.NewExternalParams) {},
			at:            later,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return WALLET_PLAYER_MISMATCH when the player does not own the wallet",
			kind:          wager.KindWin,
			amount:        "40.00",
			mutate:        func(params *wager.NewExternalParams) { params.PlayerID = domain.NewID() },
			at:            later,
			wantErr:       domain.FailureCodeWalletPlayerMismatch,
			wantRejected:  true,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return CURRENCY_MISMATCH when a loss is in another currency",
			kind:          wager.KindLoss,
			amount:        "0.00",
			mutate:        func(params *wager.NewExternalParams) { params.Money = domaintest.MustParseMoney(t, "0.00", "USD") },
			at:            later,
			wantErr:       domain.FailureCodeCurrencyMismatch,
			wantRejected:  true,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_INPUT when the operation belongs to another wallet",
			kind:          wager.KindBet,
			amount:        "25.00",
			mutate:        func(params *wager.NewExternalParams) { params.WalletID = domain.NewID() },
			at:            later,
			wantErr:       domain.FailureCodeInvalidInput,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should accept when a refund credits the bet back",
			kind:          wager.KindRefund,
			amount:        "25.00",
			referenceKind: wager.KindBet,
			mutate:        func(*wager.NewExternalParams) {},
			at:            later,
			wantEntry:     true,
			wantBalance:   domaintest.MustParseMoney(t, "125.00", "BRL"),
			wantVersion:   2,
			wantUpdatedAt: later,
		},
		{
			name:          "should accept when a rollback of a bet credits it back",
			kind:          wager.KindRollback,
			amount:        "25.00",
			referenceKind: wager.KindBet,
			mutate:        func(*wager.NewExternalParams) {},
			at:            later,
			wantEntry:     true,
			wantBalance:   domaintest.MustParseMoney(t, "125.00", "BRL"),
			wantVersion:   2,
			wantUpdatedAt: later,
		},
		{
			name:          "should accept when a rollback of a win debits it",
			kind:          wager.KindRollback,
			amount:        "25.00",
			referenceKind: wager.KindWin,
			mutate:        func(*wager.NewExternalParams) {},
			at:            later,
			wantEntry:     true,
			wantBalance:   domaintest.MustParseMoney(t, "75.00", "BRL"),
			wantVersion:   2,
			wantUpdatedAt: later,
		},
		{
			name:          "should accept when a rollback of a refund debits it",
			kind:          wager.KindRollback,
			amount:        "25.00",
			referenceKind: wager.KindRefund,
			mutate:        func(*wager.NewExternalParams) {},
			at:            later,
			wantEntry:     true,
			wantBalance:   domaintest.MustParseMoney(t, "75.00", "BRL"),
			wantVersion:   2,
			wantUpdatedAt: later,
		},
		{
			name:          "should return INSUFFICIENT_FUNDS_FOR_REVERSAL when a rollback exceeds the balance",
			kind:          wager.KindRollback,
			amount:        "100.01",
			referenceKind: wager.KindWin,
			mutate:        func(*wager.NewExternalParams) {},
			at:            later,
			wantErr:       domain.FailureCodeInsufficientFundsForReversal,
			wantRejected:  true,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
		{
			name:          "should return INVALID_INPUT when a reversal was not resolved",
			kind:          wager.KindRefund,
			amount:        "25.00",
			mutate:        func(*wager.NewExternalParams) {},
			at:            later,
			wantErr:       domain.FailureCodeInvalidInput,
			wantBalance:   domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:   1,
			wantUpdatedAt: domaintest.FixedNow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := domaintest.MustOpenWallet(t, domaintest.MustParseMoney(t, "100.00", "BRL"))
			params := domaintest.ValidExternalParams(t, test.kind, test.amount)
			params.WalletID = w.ID()
			params.PlayerID = w.PlayerID()
			test.mutate(&params)
			operation, err := wager.NewExternal(params)
			require.NoError(t, err)
			reference := domaintest.MustProcessedReference(t, w, test.referenceKind, test.amount)
			domaintest.MustResolveReference(t, operation, reference)

			got, err := w.Apply(operation, reference, test.at)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantRejected, errors.Is(err, domain.ErrRejected))
			assert.Equal(t, test.wantEntry, got != nil)
			assert.Equal(t, test.wantBalance, w.Balance())
			assert.Equal(t, test.wantVersion, w.Version())
			assert.Equal(t, test.wantUpdatedAt, w.UpdatedAt())
		})
	}
}

func TestRehydrate(t *testing.T) {
	t.Parallel()

	balance := domaintest.MustParseMoney(t, "10.00", "BRL")
	negative, err := money.ParseSigned("-1.00", "BRL")
	require.NoError(t, err)

	tests := []struct {
		name      string
		id        domain.ID
		playerID  domain.ID
		balance   money.Money
		version   int64
		createdAt time.Time
		updatedAt time.Time
		wantErr   error
	}{
		{name: "should accept when all fields are valid", id: domain.NewID(), playerID: domain.NewID(), balance: balance, version: 1, createdAt: domaintest.FixedNow, updatedAt: domaintest.FixedNow},
		{name: "should accept when the version is above one", id: domain.NewID(), playerID: domain.NewID(), balance: balance, version: 42, createdAt: domaintest.FixedNow, updatedAt: domaintest.FixedNow},
		{name: "should return INVALID_INPUT when the version is below one", id: domain.NewID(), playerID: domain.NewID(), balance: balance, version: 0, createdAt: domaintest.FixedNow, updatedAt: domaintest.FixedNow, wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_AMOUNT when the balance is negative", id: domain.NewID(), playerID: domain.NewID(), balance: negative, version: 1, createdAt: domaintest.FixedNow, updatedAt: domaintest.FixedNow, wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_INPUT when the wallet id is nil", id: domain.NilID, playerID: domain.NewID(), balance: balance, version: 1, createdAt: domaintest.FixedNow, updatedAt: domaintest.FixedNow, wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the player id is nil", id: domain.NewID(), playerID: domain.NilID, balance: balance, version: 1, createdAt: domaintest.FixedNow, updatedAt: domaintest.FixedNow, wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the balance is uninitialized", id: domain.NewID(), playerID: domain.NewID(), balance: money.Money{}, version: 1, createdAt: domaintest.FixedNow, updatedAt: domaintest.FixedNow, wantErr: domain.FailureCodeInvalidInput},
		{name: "should accept when the wallet moved after the opening", id: domain.NewID(), playerID: domain.NewID(), balance: balance, version: 2, createdAt: domaintest.FixedNow, updatedAt: domaintest.FixedNow.Add(time.Minute)},
		{name: "should return INVALID_INPUT when createdAt is missing", id: domain.NewID(), playerID: domain.NewID(), balance: balance, version: 1, updatedAt: domaintest.FixedNow, wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when updatedAt is missing", id: domain.NewID(), playerID: domain.NewID(), balance: balance, version: 1, createdAt: domaintest.FixedNow, wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := wallet.Rehydrate(test.id, test.playerID, test.balance, test.version, test.createdAt, test.updatedAt)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantErr == nil, got != nil)
		})
	}
}
