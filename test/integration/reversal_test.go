//go:build integration

package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/usecase"
	"github.com/davibanfi/betledger/test/testenv"
)

func TestProcessWagerReversesOperationsAtMostOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	brl := money.MustCurrency("BRL")
	w := useCases.MustOpenWallet(t, money.MustNew(10000, brl))

	steps := []struct {
		name            string
		input           usecase.ProcessWagerInput
		wantState       wager.State
		wantFailureCode domain.FailureCode
		wantReplay      bool
		wantBalance     money.Money
	}{
		{
			name:        "should accept when a bet is placed",
			input:       testenv.BetInput(w, "bet", 2500),
			wantState:   wager.StateProcessed,
			wantBalance: money.MustNew(7500, brl),
		},
		{
			name:        "should accept when a win refers to the bet",
			input:       testenv.ReferringInput(w, wager.KindWin, "win", "bet", 4000),
			wantState:   wager.StateProcessed,
			wantBalance: money.MustNew(11500, brl),
		},
		{
			name:        "should accept when the bet is refunded",
			input:       testenv.ReferringInput(w, wager.KindRefund, "refund", "bet", 2500),
			wantState:   wager.StateProcessed,
			wantBalance: money.MustNew(14000, brl),
		},
		{
			name:            "should return REFERENCE_ALREADY_REVERSED when the refunded bet is rolled back",
			input:           testenv.ReferringInput(w, wager.KindRollback, "rollback-bet", "bet", 2500),
			wantState:       wager.StateRejected,
			wantFailureCode: domain.FailureCodeReferenceAlreadyReversed,
		},
		{
			name:        "should accept when the refund itself is rolled back",
			input:       testenv.ReferringInput(w, wager.KindRollback, "rollback-refund", "refund", 2500),
			wantState:   wager.StateProcessed,
			wantBalance: money.MustNew(11500, brl),
		},
		{
			name:      "should report PENDING_REFERENCE when the referenced bet has not arrived",
			input:     testenv.ReferringInput(w, wager.KindRefund, "early-refund", "late-bet", 1000),
			wantState: wager.StatePendingReference,
		},
		{
			name:       "should report PENDING_REFERENCE again when the waiting refund is resent",
			input:      testenv.ReferringInput(w, wager.KindRefund, "early-refund", "late-bet", 1000),
			wantState:  wager.StatePendingReference,
			wantReplay: true,
		},
	}

	for _, step := range steps {
		got, err := useCases.ProcessWager.Execute(ctx, step.input)

		require.NoError(t, err, step.name)
		assert.Equal(t, step.wantState, got.State, step.name)
		assert.Equal(t, step.wantFailureCode, got.FailureCode, step.name)
		assert.Equal(t, step.wantReplay, got.IdempotentReplay, step.name)
		assert.Equal(t, step.wantBalance, got.Balance, step.name)
	}

	balance, version, debits := database.WalletState(t, w.ID())
	assert.Equal(t, int64(11500), balance)
	assert.Equal(t, int64(5), version)
	assert.Equal(t, 2, debits)
	database.AssertLedgerReconciles(t, w.ID())
	assert.Equal(t, map[string]int{
		"WagerTransactionProcessed":        5,
		"WalletBalanceChanged":             5,
		"WagerTransactionRejected":         1,
		"WagerTransactionPendingReference": 1,
	}, database.EventCounts(t, w.ID()))
}

func TestConcurrentReversalsOfTheSameBetCreditOnce(t *testing.T) {
	t.Parallel()

	const (
		rounds    = 10
		reversals = 6
	)
	ctx := context.Background()

	for round := range rounds {
		w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
		_, err := useCases.ProcessWager.Execute(ctx, testenv.BetInput(w, "bet", 2500))
		require.NoError(t, err)

		results := make([]usecase.WagerResult, reversals)
		errs := make([]error, reversals)
		var start, done sync.WaitGroup
		start.Add(1)
		for i := range reversals {
			done.Add(1)
			go func() {
				defer done.Done()
				start.Wait()
				kind := []wager.Kind{wager.KindRefund, wager.KindRollback}[i%2]
				input := testenv.ReferringInput(w, kind, fmt.Sprintf("reversal-%d-%d", round, i), "bet", 2500)
				results[i], errs[i] = useCases.ProcessWager.Execute(ctx, input)
			}()
		}
		start.Done()
		done.Wait()

		states := map[wager.State]int{}
		for i := range reversals {
			require.NoError(t, errs[i])
			states[results[i].State]++
		}
		assert.Equal(t, map[wager.State]int{wager.StateProcessed: 1, wager.StateRejected: reversals - 1}, states)

		balance, _, _ := database.WalletState(t, w.ID())
		assert.Equal(t, int64(10000), balance, "the bet must be returned exactly once")
		database.AssertLedgerReconciles(t, w.ID())
	}
}
