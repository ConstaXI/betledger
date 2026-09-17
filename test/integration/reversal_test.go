//go:build integration

package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/usecase"
	"github.com/davibanfi/betledger/test/testenv"
)

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
