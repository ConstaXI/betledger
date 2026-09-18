package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/usecase"
)

func TestReadWalletLedger(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")

	tests := []struct {
		name           string
		bets           int
		limit          int
		pagesBefore    int
		wantErr        error
		wantEntries    int
		wantMoreToRead bool
	}{
		{
			name:        "should return every entry when the page covers the ledger",
			bets:        2,
			limit:       10,
			wantEntries: 2,
		},
		{
			name:           "should point at the next page when entries remain",
			bets:           3,
			limit:          2,
			wantEntries:    2,
			wantMoreToRead: true,
		},
		{
			name:        "should continue after the cursor when a page was already read",
			bets:        3,
			limit:       2,
			pagesBefore: 1,
			wantEntries: 1,
		},
		{
			name:        "should default the page size when no limit is given",
			bets:        2,
			wantEntries: 2,
		},
		{
			name:    "should return INVALID_INPUT when the limit is above the maximum",
			bets:    1,
			limit:   500,
			wantErr: domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			playerID := domain.NewID()
			w, err := wallet.Open(domain.NewID(), playerID, money.MustNew(100000, brl), fixedNow)
			require.NoError(t, err)
			transactor := newFakeTransactor(w)
			processWager := usecase.NewProcessWager(transactor, fakeWallets{}, fakeTransactions{}, fakeLedger{},
				fakeOutbox{}, func() time.Time { return fixedNow }, &fakeMetrics{})
			for bet := range test.bets {
				_, err := processWager.Execute(ctx, betInput(w.ID(), playerID, string(rune('a'+bet))))
				require.NoError(t, err)
			}
			uc := usecase.NewReadWallet(transactor, fakeWallets{}, fakeLedger{}, &fakeMetrics{})
			var cursor *usecase.LedgerCursor
			for range test.pagesBefore {
				page, err := uc.Ledger(ctx, w.ID(), cursor, test.limit)
				require.NoError(t, err)
				cursor = page.NextCursor
			}

			got, err := uc.Ledger(ctx, w.ID(), cursor, test.limit)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Len(t, got.Entries, test.wantEntries)
			assert.Equal(t, test.wantMoreToRead, got.NextCursor != nil)
		})
	}
}

func TestReadWalletReconcile(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")

	tests := []struct {
		name               string
		bets               int
		unknownWallet      bool
		wantErr            error
		wantStored         money.Money
		wantCalculated     money.Money
		wantDifference     money.Money
		wantConsistent     bool
		wantCheckedEntries int
		wantMetrics        []string
	}{
		{
			name:               "should report consistent when the opening alone explains the balance",
			wantStored:         money.MustNew(100000, brl),
			wantCalculated:     money.MustNew(100000, brl),
			wantDifference:     money.MustNew(0, brl),
			wantConsistent:     true,
			wantCheckedEntries: 1,
			wantMetrics:        []string{"reconciliation true"},
		},
		{
			name:               "should report consistent when the bets are in the ledger",
			bets:               2,
			wantStored:         money.MustNew(98000, brl),
			wantCalculated:     money.MustNew(98000, brl),
			wantDifference:     money.MustNew(0, brl),
			wantConsistent:     true,
			wantCheckedEntries: 3,
			wantMetrics:        []string{"reconciliation true"},
		},
		{
			name:          "should return WALLET_NOT_FOUND when the wallet does not exist",
			unknownWallet: true,
			wantErr:       domain.FailureCodeWalletNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			clock := func() time.Time { return fixedNow }
			transactor := newFakeTransactor()
			openWallet := usecase.NewOpenWallet(transactor, fakeWallets{}, fakeTransactions{}, fakeLedger{},
				fakeOutbox{}, clock)
			playerID := domain.NewID()
			opened, err := openWallet.Execute(ctx, usecase.OpenWalletInput{
				PlayerID:       playerID,
				InitialBalance: money.MustNew(100000, brl),
				CorrelationID:  "req-1",
			})
			require.NoError(t, err)
			processWager := usecase.NewProcessWager(transactor, fakeWallets{}, fakeTransactions{}, fakeLedger{},
				fakeOutbox{}, clock, &fakeMetrics{})
			for bet := range test.bets {
				_, err := processWager.Execute(ctx, betInput(opened.ID(), playerID, string(rune('a'+bet))))
				require.NoError(t, err)
			}
			metrics := &fakeMetrics{}
			uc := usecase.NewReadWallet(transactor, fakeWallets{}, fakeLedger{}, metrics)
			walletID := opened.ID()
			if test.unknownWallet {
				walletID = domain.NewID()
			}

			got, err := uc.Reconcile(ctx, walletID)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantStored, got.Stored)
			assert.Equal(t, test.wantCalculated, got.Calculated)
			assert.Equal(t, test.wantDifference, got.Difference)
			assert.Equal(t, test.wantConsistent, got.Consistent)
			assert.Equal(t, test.wantCheckedEntries, got.CheckedEntries)
			assert.Equal(t, test.wantMetrics, metrics.calls)
		})
	}
}

// betInput is the operation the read tests apply to build a ledger.
func betInput(walletID, playerID domain.ID, externalID string) usecase.ProcessWagerInput {
	return usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: externalID,
		IdempotencyKey:        "provider-a:" + externalID,
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-987",
		GameID:                "fortune-chimp",
		Kind:                  "BET",
		Money:                 money.MustNew(1000, money.MustCurrency("BRL")),
		CorrelationID:         "req-1",
	}
}
