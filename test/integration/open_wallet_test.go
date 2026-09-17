//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/usecase"
	"github.com/davibanfi/betledger/test/testenv"
)

func TestOpenWalletExecute(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	openingRecords := testenv.PlayerRecords{
		Wallets:      []testenv.WalletRecord{{BalanceMinor: 100000, Version: 1}},
		Transactions: []testenv.TransactionRecord{{Kind: "OPENING", State: "PROCESSED"}},
		Entries:      []testenv.EntryRecord{{Direction: "CREDIT", BalanceBeforeMinor: 0, BalanceAfterMinor: 100000}},
		Events: []testenv.EventRecord{
			{Type: "WagerTransactionProcessed", CorrelationID: "req-opening"},
			{Type: "WalletBalanceChanged", CorrelationID: "req-opening"},
		},
	}

	tests := []struct {
		name           string
		initial        money.Money
		openingsBefore int
		wantErr        error
		wantRecords    testenv.PlayerRecords
	}{
		{
			name:        "should accept when the initial balance is positive, recording the opening atomically",
			initial:     money.MustNew(100000, brl),
			wantRecords: openingRecords,
		},
		{
			name:    "should accept when the initial balance is zero, recording only the wallet",
			initial: money.MustNew(0, brl),
			wantRecords: testenv.PlayerRecords{
				Wallets: []testenv.WalletRecord{{BalanceMinor: 0, Version: 1}},
			},
		},
		{
			name:           "should return WALLET_ALREADY_EXISTS when the player already holds the wallet, without side effects",
			initial:        money.MustNew(100000, brl),
			openingsBefore: 1,
			wantErr:        domain.FailureCodeWalletAlreadyExists,
			wantRecords:    openingRecords,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			input := usecase.OpenWalletInput{
				PlayerID:       domain.NewID(),
				InitialBalance: test.initial,
				CorrelationID:  "req-opening",
			}
			for range test.openingsBefore {
				_, err := useCases.OpenWallet.Execute(ctx, input)
				require.NoError(t, err)
			}

			_, err := useCases.OpenWallet.Execute(ctx, input)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantRecords, database.PlayerRecords(t, input.PlayerID))
		})
	}
}
