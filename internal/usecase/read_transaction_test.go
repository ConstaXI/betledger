package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/usecase"
)

func TestReadTransactionByID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		providerID     string
		unknownID      bool
		wantErr        error
		wantTransation bool
	}{
		{
			name:           "should accept when the operation belongs to the provider asking",
			providerID:     "provider-a",
			wantTransation: true,
		},
		{
			name:       "should return TRANSACTION_NOT_FOUND when the operation is of another provider",
			providerID: "provider-b",
			wantErr:    domain.FailureCodeTransactionNotFound,
		},
		{
			name:       "should return TRANSACTION_NOT_FOUND when no operation has the identifier",
			providerID: "provider-a",
			unknownID:  true,
			wantErr:    domain.FailureCodeTransactionNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			transactor, applied := applyBet(t)
			uc := usecase.NewReadTransaction(transactor, fakeTransactions{})
			transactionID := applied.TransactionID
			if test.unknownID {
				transactionID = domain.NewID()
			}

			got, err := uc.ByID(ctx, test.providerID, transactionID)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantTransation, got != nil)
			assert.Equal(t, test.wantTransation, got != nil && got.ID() == applied.TransactionID)
		})
	}
}

func TestReadTransactionByExternalID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		providerID     string
		externalID     string
		wantErr        error
		wantTransation bool
	}{
		{
			name:           "should accept when the provider asks for its own operation",
			providerID:     "provider-a",
			externalID:     "transaction-123",
			wantTransation: true,
		},
		{
			name:       "should return TRANSACTION_NOT_FOUND when another provider uses the same identifier",
			providerID: "provider-b",
			externalID: "transaction-123",
			wantErr:    domain.FailureCodeTransactionNotFound,
		},
		{
			name:       "should return TRANSACTION_NOT_FOUND when the provider never sent the operation",
			providerID: "provider-a",
			externalID: "transaction-999",
			wantErr:    domain.FailureCodeTransactionNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			transactor, applied := applyBet(t)
			uc := usecase.NewReadTransaction(transactor, fakeTransactions{})

			got, err := uc.ByExternalID(ctx, test.providerID, test.externalID)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantTransation, got != nil)
			assert.Equal(t, test.wantTransation, got != nil && got.ID() == applied.TransactionID)
		})
	}
}

// applyBet records one processed bet of provider-a, which the read tests query.
func applyBet(t *testing.T) (*fakeTransactor, usecase.WagerResult) {
	t.Helper()

	playerID := domain.NewID()
	w, err := wallet.Open(domain.NewID(), playerID, money.MustNew(10000, money.MustCurrency("BRL")))
	require.NoError(t, err)
	transactor := newFakeTransactor(w)
	processWager := usecase.NewProcessWager(transactor, fakeWallets{}, fakeTransactions{}, fakeLedger{},
		fakeOutbox{}, func() time.Time { return fixedNow }, &fakeMetrics{})

	input := betInput(w.ID(), playerID, "transaction-123")
	input.Kind = wager.KindBet
	applied, err := processWager.Execute(context.Background(), input)
	require.NoError(t, err)
	return transactor, applied
}
