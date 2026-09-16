package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
)

func TestNewWalletLedgerEntry(t *testing.T) {
	t.Parallel()

	before := mustMoney(t, "100.00", "BRL")

	tests := []struct {
		name          string
		transactionID domain.ID
		direction     domain.LedgerDirection
		money         domain.Money
		balanceBefore domain.Money
		balanceAfter  domain.Money
		wantErr       error
	}{
		{
			name: "should accept when a debit matches the balance arithmetic", transactionID: domain.NewID(), direction: domain.DirectionDebit,
			money: mustMoney(t, "25.00", "BRL"), balanceBefore: before, balanceAfter: mustMoney(t, "75.00", "BRL"),
		},
		{
			name: "should accept when a credit matches the balance arithmetic", transactionID: domain.NewID(), direction: domain.DirectionCredit,
			money: mustMoney(t, "25.00", "BRL"), balanceBefore: before, balanceAfter: mustMoney(t, "125.00", "BRL"),
		},
		{
			name: "should return INVALID_AMOUNT when balanceAfter does not match the arithmetic", transactionID: domain.NewID(), direction: domain.DirectionDebit,
			money: mustMoney(t, "25.00", "BRL"), balanceBefore: before, balanceAfter: mustMoney(t, "80.00", "BRL"),
			wantErr: domain.FailureCodeInvalidAmount,
		},
		{
			name: "should return INVALID_AMOUNT when the entry amount is zero", transactionID: domain.NewID(), direction: domain.DirectionCredit,
			money: mustMoney(t, "0.00", "BRL"), balanceBefore: before, balanceAfter: before,
			wantErr: domain.FailureCodeInvalidAmount,
		},
		{
			name: "should return INVALID_INPUT when the direction is unknown", transactionID: domain.NewID(), direction: "TRANSFER",
			money: mustMoney(t, "1.00", "BRL"), balanceBefore: before, balanceAfter: mustMoney(t, "101.00", "BRL"),
			wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return CURRENCY_MISMATCH when the amount is in another currency", transactionID: domain.NewID(), direction: domain.DirectionCredit,
			money: mustMoney(t, "1.00", "USD"), balanceBefore: before, balanceAfter: mustMoney(t, "101.00", "BRL"),
			wantErr: domain.FailureCodeCurrencyMismatch,
		},
		{
			name: "should return INVALID_INPUT when the transaction id is nil", transactionID: domain.NilID, direction: domain.DirectionCredit,
			money: mustMoney(t, "1.00", "BRL"), balanceBefore: before, balanceAfter: mustMoney(t, "101.00", "BRL"),
			wantErr: domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := domain.NewWalletLedgerEntry(domain.NewID(), domain.NewID(), test.transactionID,
				test.direction, test.money, test.balanceBefore, test.balanceAfter)

			assert.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestLedgerEntrySignedMoney(t *testing.T) {
	t.Parallel()

	before := mustMoney(t, "100.00", "BRL")
	money := mustMoney(t, "25.00", "BRL")

	tests := []struct {
		name         string
		direction    domain.LedgerDirection
		balanceAfter domain.Money
		wantResult   domain.Money
	}{
		{name: "should return a negative value when the entry is a debit", direction: domain.DirectionDebit, balanceAfter: mustMoney(t, "75.00", "BRL"), wantResult: mustSignedMoney(t, "-25.00", "BRL")},
		{name: "should return a positive value when the entry is a credit", direction: domain.DirectionCredit, balanceAfter: mustMoney(t, "125.00", "BRL"), wantResult: money},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			entry, err := domain.NewWalletLedgerEntry(domain.NewID(), domain.NewID(), domain.NewID(),
				test.direction, money, before, test.balanceAfter)
			require.NoError(t, err)

			got, err := entry.SignedMoney()

			assert.NoError(t, err)
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func mustSignedMoney(t *testing.T, amount, currency string) domain.Money {
	t.Helper()

	money, err := domain.ParseSignedMoney(amount, currency)
	require.NoError(t, err)
	return money
}
