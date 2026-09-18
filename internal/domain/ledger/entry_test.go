package ledger_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/domaintest"
	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/money"
)

func TestNewWalletLedgerEntry(t *testing.T) {
	t.Parallel()

	before := domaintest.MustParseMoney(t, "100.00", "BRL")

	tests := []struct {
		name          string
		transactionID domain.ID
		direction     ledger.Direction
		money         money.Money
		balanceBefore money.Money
		balanceAfter  money.Money
		createdAt     time.Time
		wantErr       error
	}{
		{
			name: "should accept when a debit matches the balance arithmetic", transactionID: domain.NewID(), direction: ledger.Debit, createdAt: domaintest.FixedNow,
			money: domaintest.MustParseMoney(t, "25.00", "BRL"), balanceBefore: before, balanceAfter: domaintest.MustParseMoney(t, "75.00", "BRL"),
		},
		{
			name: "should accept when a credit matches the balance arithmetic", transactionID: domain.NewID(), direction: ledger.Credit, createdAt: domaintest.FixedNow,
			money: domaintest.MustParseMoney(t, "25.00", "BRL"), balanceBefore: before, balanceAfter: domaintest.MustParseMoney(t, "125.00", "BRL"),
		},
		{
			name: "should return INVALID_AMOUNT when balanceAfter does not match the arithmetic", transactionID: domain.NewID(), direction: ledger.Debit, createdAt: domaintest.FixedNow,
			money: domaintest.MustParseMoney(t, "25.00", "BRL"), balanceBefore: before, balanceAfter: domaintest.MustParseMoney(t, "80.00", "BRL"),
			wantErr: domain.FailureCodeInvalidAmount,
		},
		{
			name: "should return INVALID_AMOUNT when the entry amount is zero", transactionID: domain.NewID(), direction: ledger.Credit, createdAt: domaintest.FixedNow,
			money: domaintest.MustParseMoney(t, "0.00", "BRL"), balanceBefore: before, balanceAfter: before,
			wantErr: domain.FailureCodeInvalidAmount,
		},
		{
			name: "should return INVALID_INPUT when the direction is unknown", transactionID: domain.NewID(), direction: "TRANSFER", createdAt: domaintest.FixedNow,
			money: domaintest.MustParseMoney(t, "1.00", "BRL"), balanceBefore: before, balanceAfter: domaintest.MustParseMoney(t, "101.00", "BRL"),
			wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return CURRENCY_MISMATCH when the amount is in another currency", transactionID: domain.NewID(), direction: ledger.Credit, createdAt: domaintest.FixedNow,
			money: domaintest.MustParseMoney(t, "1.00", "USD"), balanceBefore: before, balanceAfter: domaintest.MustParseMoney(t, "101.00", "BRL"),
			wantErr: domain.FailureCodeCurrencyMismatch,
		},
		{
			name: "should return INVALID_INPUT when createdAt is missing", transactionID: domain.NewID(), direction: ledger.Credit,
			money: domaintest.MustParseMoney(t, "1.00", "BRL"), balanceBefore: before, balanceAfter: domaintest.MustParseMoney(t, "101.00", "BRL"),
			wantErr: domain.FailureCodeInvalidInput,
		},
		{
			name: "should return INVALID_INPUT when the transaction id is nil", transactionID: domain.NilID, direction: ledger.Credit, createdAt: domaintest.FixedNow,
			money: domaintest.MustParseMoney(t, "1.00", "BRL"), balanceBefore: before, balanceAfter: domaintest.MustParseMoney(t, "101.00", "BRL"),
			wantErr: domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := ledger.NewEntry(domain.NewID(), domain.NewID(), test.transactionID,
				test.direction, test.money, test.balanceBefore, test.balanceAfter, test.createdAt)

			assert.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestLedgerEntrySignedMoney(t *testing.T) {
	t.Parallel()

	before := domaintest.MustParseMoney(t, "100.00", "BRL")
	amount := domaintest.MustParseMoney(t, "25.00", "BRL")

	tests := []struct {
		name         string
		direction    ledger.Direction
		balanceAfter money.Money
		wantResult   money.Money
	}{
		{name: "should return a negative value when the entry is a debit", direction: ledger.Debit, balanceAfter: domaintest.MustParseMoney(t, "75.00", "BRL"), wantResult: domaintest.MustParseSignedMoney(t, "-25.00", "BRL")},
		{name: "should return a positive value when the entry is a credit", direction: ledger.Credit, balanceAfter: domaintest.MustParseMoney(t, "125.00", "BRL"), wantResult: amount},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			entry, err := ledger.NewEntry(domain.NewID(), domain.NewID(), domain.NewID(),
				test.direction, amount, before, test.balanceAfter, domaintest.FixedNow)
			require.NoError(t, err)

			got, err := entry.SignedMoney()

			assert.NoError(t, err)
			assert.Equal(t, test.wantResult, got)
		})
	}
}
