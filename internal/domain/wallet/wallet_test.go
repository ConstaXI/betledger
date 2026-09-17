package wallet_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/domaintest"
	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
)

func TestOpenWalletStartsAtVersionOne(t *testing.T) {
	t.Parallel()

	initial := domaintest.MustParseMoney(t, "1000.00", "BRL")

	wallet, err := wallet.Open(domain.NewID(), domain.NewID(), initial)

	require.NoError(t, err)
	assert.Equal(t, int64(1), wallet.Version())
	assert.Equal(t, initial, wallet.Balance())
	assert.Equal(t, initial.Currency(), wallet.Currency())
}

func TestOpeningLedgerEntry(t *testing.T) {
	t.Parallel()

	initial := domaintest.MustParseMoney(t, "1000.00", "BRL")
	wallet := domaintest.MustOpenWallet(t, initial)

	entry, err := wallet.OpeningLedgerEntry(domain.NewID())

	require.NoError(t, err)
	assert.Equal(t, domaintest.MustParseMoney(t, "0.00", "BRL"), entry.BalanceBefore())
	assert.Equal(t, initial, entry.BalanceAfter())
	assert.Equal(t, ledger.Credit, entry.Direction())
	assert.Equal(t, int64(1), wallet.Version(), "opening must not bump the version")

	empty := domaintest.MustOpenWallet(t, domaintest.MustParseMoney(t, "0.00", "BRL"))
	_, err = empty.OpeningLedgerEntry(domain.NewID())
	assert.ErrorIs(t, err, domain.FailureCodeInvalidAmount)
}

func TestWalletDebitAndCredit(t *testing.T) {
	t.Parallel()

	wallet := domaintest.MustOpenWallet(t, domaintest.MustParseMoney(t, "100.00", "BRL"))

	entry, err := wallet.Debit(domaintest.MustParseMoney(t, "80.00", "BRL"), domain.NewID())

	require.NoError(t, err)
	assert.Equal(t, ledger.Debit, entry.Direction())
	assert.Equal(t, domaintest.MustParseMoney(t, "100.00", "BRL"), entry.BalanceBefore())
	assert.Equal(t, domaintest.MustParseMoney(t, "20.00", "BRL"), entry.BalanceAfter())
	assert.Equal(t, domaintest.MustParseMoney(t, "20.00", "BRL"), wallet.Balance())
	assert.Equal(t, int64(2), wallet.Version())

	_, err = wallet.Credit(domaintest.MustParseMoney(t, "5.00", "BRL"), domain.NewID())

	require.NoError(t, err)
	assert.Equal(t, domaintest.MustParseMoney(t, "25.00", "BRL"), wallet.Balance())
	assert.Equal(t, int64(3), wallet.Version())
}

func TestWalletDebitRejectsNegativeBalance(t *testing.T) {
	t.Parallel()

	wallet := domaintest.MustOpenWallet(t, domaintest.MustParseMoney(t, "100.00", "BRL"))

	_, err := wallet.Debit(domaintest.MustParseMoney(t, "100.01", "BRL"), domain.NewID())

	assert.ErrorIs(t, err, domain.ErrInsufficientFunds)
	assert.ErrorIs(t, err, domain.ErrRejected)
	assert.ErrorIs(t, err, domain.FailureCodeInsufficientFunds)
	assert.Equal(t, domaintest.MustParseMoney(t, "100.00", "BRL"), wallet.Balance(), "a rejection must not move the balance")
	assert.Equal(t, int64(1), wallet.Version(), "a rejection must not bump the version")
}

func TestWalletMovementValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		money   money.Money
		wantErr error
	}{
		{name: "should accept when the movement matches the wallet currency", money: domaintest.MustParseMoney(t, "10.00", "BRL")},
		{name: "should return CURRENCY_MISMATCH when the movement is in a foreign currency", money: domaintest.MustParseMoney(t, "10.00", "USD"), wantErr: domain.FailureCodeCurrencyMismatch},
		{name: "should return INVALID_AMOUNT when the movement amount is zero", money: domaintest.MustParseMoney(t, "0.00", "BRL"), wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_INPUT when the movement amount is uninitialized", money: money.Money{}, wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			wallet := domaintest.MustOpenWallet(t, domaintest.MustParseMoney(t, "100.00", "BRL"))

			_, err := wallet.Credit(test.money, domain.NewID())

			assert.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestWalletApply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		kind         wager.Kind
		amount       string
		mutate       func(params *wager.NewExternalParams)
		wantErr      error
		wantRejected bool
		wantEntry    bool
		wantBalance  money.Money
		wantVersion  int64
	}{
		{
			name:        "should accept when the balance covers the bet",
			kind:        wager.KindBet,
			amount:      "25.00",
			mutate:      func(*wager.NewExternalParams) {},
			wantEntry:   true,
			wantBalance: domaintest.MustParseMoney(t, "75.00", "BRL"),
			wantVersion: 2,
		},
		{
			name:        "should accept when the bet takes the whole balance",
			kind:        wager.KindBet,
			amount:      "100.00",
			mutate:      func(*wager.NewExternalParams) {},
			wantEntry:   true,
			wantBalance: domaintest.MustParseMoney(t, "0.00", "BRL"),
			wantVersion: 2,
		},
		{
			name:         "should return INSUFFICIENT_FUNDS when the bet exceeds the balance",
			kind:         wager.KindBet,
			amount:       "100.01",
			mutate:       func(*wager.NewExternalParams) {},
			wantErr:      domain.FailureCodeInsufficientFunds,
			wantRejected: true,
			wantBalance:  domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:  1,
		},
		{
			name:        "should accept when a win credits the wallet",
			kind:        wager.KindWin,
			amount:      "40.00",
			mutate:      func(*wager.NewExternalParams) {},
			wantEntry:   true,
			wantBalance: domaintest.MustParseMoney(t, "140.00", "BRL"),
			wantVersion: 2,
		},
		{
			name:        "should accept when a loss moves nothing",
			kind:        wager.KindLoss,
			amount:      "0.00",
			mutate:      func(*wager.NewExternalParams) {},
			wantBalance: domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion: 1,
		},
		{
			name:         "should return WALLET_PLAYER_MISMATCH when the player does not own the wallet",
			kind:         wager.KindWin,
			amount:       "40.00",
			mutate:       func(params *wager.NewExternalParams) { params.PlayerID = domain.NewID() },
			wantErr:      domain.FailureCodeWalletPlayerMismatch,
			wantRejected: true,
			wantBalance:  domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:  1,
		},
		{
			name:         "should return CURRENCY_MISMATCH when a loss is in another currency",
			kind:         wager.KindLoss,
			amount:       "0.00",
			mutate:       func(params *wager.NewExternalParams) { params.Money = domaintest.MustParseMoney(t, "0.00", "USD") },
			wantErr:      domain.FailureCodeCurrencyMismatch,
			wantRejected: true,
			wantBalance:  domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion:  1,
		},
		{
			name:        "should return INVALID_INPUT when the operation belongs to another wallet",
			kind:        wager.KindBet,
			amount:      "25.00",
			mutate:      func(params *wager.NewExternalParams) { params.WalletID = domain.NewID() },
			wantErr:     domain.FailureCodeInvalidInput,
			wantBalance: domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion: 1,
		},
		{
			name:        "should return INVALID_INPUT when the operation is a reversal",
			kind:        wager.KindRefund,
			amount:      "25.00",
			mutate:      func(*wager.NewExternalParams) {},
			wantErr:     domain.FailureCodeInvalidInput,
			wantBalance: domaintest.MustParseMoney(t, "100.00", "BRL"),
			wantVersion: 1,
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

			got, err := w.Apply(operation)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantRejected, errors.Is(err, domain.ErrRejected))
			assert.Equal(t, test.wantEntry, got != nil)
			assert.Equal(t, test.wantBalance, w.Balance())
			assert.Equal(t, test.wantVersion, w.Version())
		})
	}
}

func TestRehydrateWallet(t *testing.T) {
	t.Parallel()

	balance := domaintest.MustParseMoney(t, "10.00", "BRL")
	negative, err := money.ParseSigned("-1.00", "BRL")
	require.NoError(t, err)

	tests := []struct {
		name     string
		id       domain.ID
		playerID domain.ID
		balance  money.Money
		version  int64
		wantErr  error
	}{
		{name: "should accept when all fields are valid", id: domain.NewID(), playerID: domain.NewID(), balance: balance, version: 1},
		{name: "should accept when the version is above one", id: domain.NewID(), playerID: domain.NewID(), balance: balance, version: 42},
		{name: "should return INVALID_INPUT when the version is below one", id: domain.NewID(), playerID: domain.NewID(), balance: balance, version: 0, wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_AMOUNT when the balance is negative", id: domain.NewID(), playerID: domain.NewID(), balance: negative, version: 1, wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_INPUT when the wallet id is nil", id: domain.NilID, playerID: domain.NewID(), balance: balance, version: 1, wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the player id is nil", id: domain.NewID(), playerID: domain.NilID, balance: balance, version: 1, wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the balance is uninitialized", id: domain.NewID(), playerID: domain.NewID(), balance: money.Money{}, version: 1, wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := wallet.Rehydrate(test.id, test.playerID, test.balance, test.version)

			assert.ErrorIs(t, err, test.wantErr)
		})
	}
}
