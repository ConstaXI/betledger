package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
)

func TestOpenWalletStartsAtVersionOne(t *testing.T) {
	t.Parallel()

	initial := mustMoney(t, "1000.00", "BRL")

	wallet, err := domain.OpenWallet(domain.NewID(), domain.NewID(), initial)

	require.NoError(t, err)
	assert.Equal(t, int64(1), wallet.Version())
	assert.Equal(t, initial, wallet.Balance())
	assert.Equal(t, initial.Currency(), wallet.Currency())
}

func TestOpeningLedgerEntry(t *testing.T) {
	t.Parallel()

	initial := mustMoney(t, "1000.00", "BRL")
	wallet := mustWallet(t, initial)

	entry, err := wallet.OpeningLedgerEntry(domain.NewID())

	require.NoError(t, err)
	assert.Equal(t, mustMoney(t, "0.00", "BRL"), entry.BalanceBefore())
	assert.Equal(t, initial, entry.BalanceAfter())
	assert.Equal(t, domain.DirectionCredit, entry.Direction())
	assert.Equal(t, int64(1), wallet.Version(), "opening must not bump the version")

	empty := mustWallet(t, mustMoney(t, "0.00", "BRL"))
	_, err = empty.OpeningLedgerEntry(domain.NewID())
	assert.ErrorIs(t, err, domain.FailureCodeInvalidAmount)
}

func TestWalletDebitAndCredit(t *testing.T) {
	t.Parallel()

	wallet := mustWallet(t, mustMoney(t, "100.00", "BRL"))

	entry, err := wallet.Debit(mustMoney(t, "80.00", "BRL"), domain.NewID())

	require.NoError(t, err)
	assert.Equal(t, domain.DirectionDebit, entry.Direction())
	assert.Equal(t, mustMoney(t, "100.00", "BRL"), entry.BalanceBefore())
	assert.Equal(t, mustMoney(t, "20.00", "BRL"), entry.BalanceAfter())
	assert.Equal(t, mustMoney(t, "20.00", "BRL"), wallet.Balance())
	assert.Equal(t, int64(2), wallet.Version())

	_, err = wallet.Credit(mustMoney(t, "5.00", "BRL"), domain.NewID())

	require.NoError(t, err)
	assert.Equal(t, mustMoney(t, "25.00", "BRL"), wallet.Balance())
	assert.Equal(t, int64(3), wallet.Version())
}

func TestWalletDebitRejectsNegativeBalance(t *testing.T) {
	t.Parallel()

	wallet := mustWallet(t, mustMoney(t, "100.00", "BRL"))

	_, err := wallet.Debit(mustMoney(t, "100.01", "BRL"), domain.NewID())

	assert.ErrorIs(t, err, domain.ErrInsufficientFunds)
	assert.ErrorIs(t, err, domain.ErrRejected)
	assert.ErrorIs(t, err, domain.FailureCodeInsufficientFunds)
	assert.Equal(t, mustMoney(t, "100.00", "BRL"), wallet.Balance(), "a rejection must not move the balance")
	assert.Equal(t, int64(1), wallet.Version(), "a rejection must not bump the version")
}

func TestWalletMovementValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		money   domain.Money
		wantErr error
	}{
		{name: "should accept when the movement matches the wallet currency", money: mustMoney(t, "10.00", "BRL")},
		{name: "should return CURRENCY_MISMATCH when the movement is in a foreign currency", money: mustMoney(t, "10.00", "USD"), wantErr: domain.FailureCodeCurrencyMismatch},
		{name: "should return INVALID_AMOUNT when the movement amount is zero", money: mustMoney(t, "0.00", "BRL"), wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_INPUT when the movement amount is uninitialized", money: domain.Money{}, wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			wallet := mustWallet(t, mustMoney(t, "100.00", "BRL"))

			_, err := wallet.Credit(test.money, domain.NewID())

			assert.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestRehydrateWallet(t *testing.T) {
	t.Parallel()

	balance := mustMoney(t, "10.00", "BRL")
	negative, err := domain.ParseSignedMoney("-1.00", "BRL")
	require.NoError(t, err)

	tests := []struct {
		name     string
		id       domain.ID
		playerID domain.ID
		balance  domain.Money
		version  int64
		wantErr  error
	}{
		{name: "should accept when all fields are valid", id: domain.NewID(), playerID: domain.NewID(), balance: balance, version: 1},
		{name: "should accept when the version is above one", id: domain.NewID(), playerID: domain.NewID(), balance: balance, version: 42},
		{name: "should return INVALID_INPUT when the version is below one", id: domain.NewID(), playerID: domain.NewID(), balance: balance, version: 0, wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_AMOUNT when the balance is negative", id: domain.NewID(), playerID: domain.NewID(), balance: negative, version: 1, wantErr: domain.FailureCodeInvalidAmount},
		{name: "should return INVALID_INPUT when the wallet id is nil", id: domain.NilID, playerID: domain.NewID(), balance: balance, version: 1, wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the player id is nil", id: domain.NewID(), playerID: domain.NilID, balance: balance, version: 1, wantErr: domain.FailureCodeInvalidInput},
		{name: "should return INVALID_INPUT when the balance is uninitialized", id: domain.NewID(), playerID: domain.NewID(), balance: domain.Money{}, version: 1, wantErr: domain.FailureCodeInvalidInput},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := domain.RehydrateWallet(test.id, test.playerID, test.balance, test.version)

			assert.ErrorIs(t, err, test.wantErr)
		})
	}
}

func mustMoney(t *testing.T, amount, currency string) domain.Money {
	t.Helper()

	money, err := domain.ParseMoney(amount, currency)
	require.NoError(t, err)
	return money
}

func mustWallet(t *testing.T, initialBalance domain.Money) *domain.Wallet {
	t.Helper()

	wallet, err := domain.OpenWallet(domain.NewID(), domain.NewID(), initialBalance)
	require.NoError(t, err)
	return wallet
}
