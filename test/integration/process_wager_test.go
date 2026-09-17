//go:build integration

package integration

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/usecase"
	"github.com/davibanfi/betledger/test/testenv"
)

func TestTwoConcurrentBetsNeverOverdrawTheWallet(t *testing.T) {
	t.Parallel()

	const rounds = 20
	ctx := context.Background()

	for round := range rounds {
		w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))

		results := make([]usecase.WagerResult, 2)
		errs := make([]error, 2)
		var start, done sync.WaitGroup
		start.Add(1)
		for i := range 2 {
			done.Add(1)
			go func() {
				defer done.Done()
				start.Wait()
				input := testenv.BetInput(w, fmt.Sprintf("race-%d-%d", round, i), 8000)
				results[i], errs[i] = useCases.ProcessWager.Execute(ctx, input)
			}()
		}
		start.Done()
		done.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		states := map[wager.State]int{}
		failures := map[domain.FailureCode]int{}
		for _, result := range results {
			states[result.State]++
			failures[result.FailureCode]++
		}
		assert.Equal(t, map[wager.State]int{wager.StateProcessed: 1, wager.StateRejected: 1}, states)
		assert.Equal(t, 1, failures[domain.FailureCodeInsufficientFunds])

		balance, version, debits := database.WalletState(t, w.ID())
		assert.Equal(t, int64(2000), balance)
		assert.Equal(t, int64(2), version)
		assert.Equal(t, 1, debits)
		database.AssertLedgerReconciles(t, w.ID())
	}
}

func TestTheSameBetSentFiftyTimesDebitsOnce(t *testing.T) {
	t.Parallel()

	const (
		rounds   = 10
		attempts = 50
	)
	ctx := context.Background()

	for range rounds {
		w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
		input := testenv.BetInput(w, "duplicated", 2500)

		results := make([]usecase.WagerResult, attempts)
		errs := make([]error, attempts)
		var start, done sync.WaitGroup
		start.Add(1)
		for i := range attempts {
			done.Add(1)
			go func() {
				defer done.Done()
				start.Wait()
				results[i], errs[i] = useCases.ProcessWager.Execute(ctx, input)
			}()
		}
		start.Done()
		done.Wait()

		replays := map[bool]int{}
		transactionIDs := map[domain.ID]int{}
		for i := range attempts {
			require.NoError(t, errs[i])
			replays[results[i].IdempotentReplay]++
			transactionIDs[results[i].TransactionID]++
			assert.Equal(t, money.MustNew(7500, money.MustCurrency("BRL")), results[i].Balance)
		}
		assert.Equal(t, map[bool]int{false: 1, true: attempts - 1}, replays)
		assert.Len(t, transactionIDs, 1)

		balance, version, debits := database.WalletState(t, w.ID())
		assert.Equal(t, int64(7500), balance)
		assert.Equal(t, int64(2), version)
		assert.Equal(t, 1, debits)
		database.AssertLedgerReconciles(t, w.ID())
	}
}

func TestIndependentWalletsAreProcessedConcurrently(t *testing.T) {
	t.Parallel()

	const wallets = 10
	ctx := context.Background()

	opened := make([]*wallet.Wallet, wallets)
	for i := range wallets {
		opened[i] = useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	}

	errs := make([]error, wallets)
	var done sync.WaitGroup
	for i := range wallets {
		done.Add(1)
		go func() {
			defer done.Done()
			_, errs[i] = useCases.ProcessWager.Execute(ctx, testenv.BetInput(opened[i], "independent", 3000))
		}()
	}
	done.Wait()

	for i := range wallets {
		require.NoError(t, errs[i])
		balance, _, debits := database.WalletState(t, opened[i].ID())
		assert.Equal(t, int64(7000), balance)
		assert.Equal(t, 1, debits)
		database.AssertLedgerReconciles(t, opened[i].ID())
	}
}

func TestProcessWagerExecute(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	bet := func(externalID string, amountMinor int64) func(w *wallet.Wallet) usecase.ProcessWagerInput {
		return func(w *wallet.Wallet) usecase.ProcessWagerInput { return testenv.BetInput(w, externalID, amountMinor) }
	}
	plain := func(kind wager.Kind, externalID string, amountMinor int64) func(w *wallet.Wallet) usecase.ProcessWagerInput {
		return func(w *wallet.Wallet) usecase.ProcessWagerInput {
			return testenv.WagerInput(w, kind, externalID, amountMinor)
		}
	}
	referring := func(
		kind wager.Kind,
		externalID, referenceID string,
		amountMinor int64,
	) func(w *wallet.Wallet) usecase.ProcessWagerInput {
		return func(w *wallet.Wallet) usecase.ProcessWagerInput {
			return testenv.ReferringInput(w, kind, externalID, referenceID, amountMinor)
		}
	}
	type step = func(w *wallet.Wallet) usecase.ProcessWagerInput

	tests := []struct {
		name            string
		earlier         []step
		input           step
		wantErr         error
		wantState       wager.State
		wantFailureCode domain.FailureCode
		wantBalance     money.Money
		wantReplay      bool
		wantStored      int64
		wantVersion     int64
		wantDebits      int
		wantEvents      map[string]int
	}{
		{
			name:        "should accept when a win credits the wallet",
			input:       plain(wager.KindWin, "win", 4000),
			wantState:   wager.StateProcessed,
			wantBalance: money.MustNew(14000, brl),
			wantStored:  14000,
			wantVersion: 2,
			wantEvents:  map[string]int{"WagerTransactionProcessed": 2, "WalletBalanceChanged": 2},
		},
		{
			name:        "should accept when a loss records the round without moving the balance",
			input:       plain(wager.KindLoss, "loss", 0),
			wantState:   wager.StateProcessed,
			wantBalance: money.MustNew(10000, brl),
			wantStored:  10000,
			wantVersion: 1,
			wantEvents:  map[string]int{"WagerTransactionProcessed": 2, "WalletBalanceChanged": 1},
		},
		{
			name:        "should report a replay when a loss is resent",
			earlier:     []step{plain(wager.KindLoss, "loss", 0)},
			input:       plain(wager.KindLoss, "loss", 0),
			wantState:   wager.StateProcessed,
			wantBalance: money.MustNew(10000, brl),
			wantReplay:  true,
			wantStored:  10000,
			wantVersion: 1,
			wantEvents:  map[string]int{"WagerTransactionProcessed": 2, "WalletBalanceChanged": 1},
		},
		{
			name:        "should report a replay with the original balance when a bet is resent after other movements",
			earlier:     []step{bet("first", 2500), bet("second", 2500)},
			input:       bet("first", 2500),
			wantState:   wager.StateProcessed,
			wantBalance: money.MustNew(7500, brl),
			wantReplay:  true,
			wantStored:  5000,
			wantVersion: 3,
			wantDebits:  2,
			wantEvents:  map[string]int{"WagerTransactionProcessed": 3, "WalletBalanceChanged": 3},
		},
		{
			name:            "should report a replay of INSUFFICIENT_FUNDS when the rejected bet is resent",
			earlier:         []step{bet("too-large", 12000)},
			input:           bet("too-large", 12000),
			wantState:       wager.StateRejected,
			wantFailureCode: domain.FailureCodeInsufficientFunds,
			wantReplay:      true,
			wantStored:      10000,
			wantVersion:     1,
			wantEvents: map[string]int{
				"WagerTransactionProcessed": 1, "WalletBalanceChanged": 1, "WagerTransactionRejected": 1,
			},
		},
		{
			name:        "should return IDEMPOTENCY_CONFLICT when the key is reused with a different payload",
			earlier:     []step{bet("conflict", 2500)},
			input:       bet("conflict", 3000),
			wantErr:     domain.FailureCodeIdempotencyConflict,
			wantStored:  7500,
			wantVersion: 2,
			wantDebits:  1,
			wantEvents:  map[string]int{"WagerTransactionProcessed": 2, "WalletBalanceChanged": 2},
		},
		{
			name:    "should return IDEMPOTENCY_CONFLICT when the operation is resent with another key",
			earlier: []step{bet("conflict", 2500)},
			input: func(w *wallet.Wallet) usecase.ProcessWagerInput {
				input := testenv.BetInput(w, "conflict", 2500)
				input.IdempotencyKey += ":another-key"
				return input
			},
			wantErr:     domain.FailureCodeIdempotencyConflict,
			wantStored:  7500,
			wantVersion: 2,
			wantDebits:  1,
			wantEvents:  map[string]int{"WagerTransactionProcessed": 2, "WalletBalanceChanged": 2},
		},
		{
			name:        "should accept when a win refers to a processed bet",
			earlier:     []step{bet("bet", 2500)},
			input:       referring(wager.KindWin, "win", "bet", 4000),
			wantState:   wager.StateProcessed,
			wantBalance: money.MustNew(11500, brl),
			wantStored:  11500,
			wantVersion: 3,
			wantDebits:  1,
			wantEvents:  map[string]int{"WagerTransactionProcessed": 3, "WalletBalanceChanged": 3},
		},
		{
			name:        "should accept when a refund returns a processed bet",
			earlier:     []step{bet("bet", 2500)},
			input:       referring(wager.KindRefund, "refund", "bet", 2500),
			wantState:   wager.StateProcessed,
			wantBalance: money.MustNew(10000, brl),
			wantStored:  10000,
			wantVersion: 3,
			wantDebits:  1,
			wantEvents:  map[string]int{"WagerTransactionProcessed": 3, "WalletBalanceChanged": 3},
		},
		{
			name:        "should accept when the refund itself is rolled back",
			earlier:     []step{bet("bet", 2500), referring(wager.KindRefund, "refund", "bet", 2500)},
			input:       referring(wager.KindRollback, "rollback", "refund", 2500),
			wantState:   wager.StateProcessed,
			wantBalance: money.MustNew(7500, brl),
			wantStored:  7500,
			wantVersion: 4,
			wantDebits:  2,
			wantEvents:  map[string]int{"WagerTransactionProcessed": 4, "WalletBalanceChanged": 4},
		},
		{
			name:            "should return REFERENCE_ALREADY_REVERSED when a refunded bet is rolled back",
			earlier:         []step{bet("bet", 2500), referring(wager.KindRefund, "refund", "bet", 2500)},
			input:           referring(wager.KindRollback, "rollback", "bet", 2500),
			wantState:       wager.StateRejected,
			wantFailureCode: domain.FailureCodeReferenceAlreadyReversed,
			wantStored:      10000,
			wantVersion:     3,
			wantDebits:      1,
			wantEvents: map[string]int{
				"WagerTransactionProcessed": 3, "WalletBalanceChanged": 3, "WagerTransactionRejected": 1,
			},
		},
		{
			name:        "should report PENDING_REFERENCE when the referenced bet has not arrived",
			input:       referring(wager.KindRefund, "early-refund", "late-bet", 1000),
			wantState:   wager.StatePendingReference,
			wantStored:  10000,
			wantVersion: 1,
			wantEvents: map[string]int{
				"WagerTransactionProcessed": 1, "WalletBalanceChanged": 1, "WagerTransactionPendingReference": 1,
			},
		},
		{
			name:        "should report a replay of PENDING_REFERENCE when the waiting refund is resent",
			earlier:     []step{referring(wager.KindRefund, "early-refund", "late-bet", 1000)},
			input:       referring(wager.KindRefund, "early-refund", "late-bet", 1000),
			wantState:   wager.StatePendingReference,
			wantReplay:  true,
			wantStored:  10000,
			wantVersion: 1,
			wantEvents: map[string]int{
				"WagerTransactionProcessed": 1, "WalletBalanceChanged": 1, "WagerTransactionPendingReference": 1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			w := useCases.MustOpenWallet(t, money.MustNew(10000, brl))
			var earlierIDs []domain.ID
			for _, earlier := range test.earlier {
				result, err := useCases.ProcessWager.Execute(ctx, earlier(w))
				require.NoError(t, err)
				earlierIDs = append(earlierIDs, result.TransactionID)
			}

			got, err := useCases.ProcessWager.Execute(ctx, test.input(w))

			stored, version, debits := database.WalletState(t, w.ID())
			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantState, got.State)
			assert.Equal(t, test.wantFailureCode, got.FailureCode)
			assert.Equal(t, test.wantBalance, got.Balance)
			assert.Equal(t, test.wantReplay, got.IdempotentReplay)
			assert.Equal(t, test.wantReplay, slices.Contains(earlierIDs, got.TransactionID))
			assert.Equal(t, test.wantStored, stored)
			assert.Equal(t, test.wantVersion, version)
			assert.Equal(t, test.wantDebits, debits)
			assert.Equal(t, test.wantEvents, database.EventCounts(t, w.ID()))
			database.AssertLedgerReconciles(t, w.ID())
		})
	}
}
