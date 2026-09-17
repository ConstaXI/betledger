//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/test/testenv"
)

func TestTwoConcurrentBetsOnDifferentInstancesNeverOverdrawTheWallet(t *testing.T) {
	t.Parallel()

	const rounds = 15
	instances := []*testenv.Process{
		binary.StartProcess(t, database.URL, identityProvider),
		binary.StartProcess(t, database.URL, identityProvider),
		binary.StartProcess(t, database.URL, identityProvider),
	}
	bearer := identityProvider.Bearer(t, "provider-a")
	replayStatusOf := map[int]int{
		http.StatusCreated:             http.StatusOK,
		http.StatusUnprocessableEntity: http.StatusUnprocessableEntity,
	}

	for round := range rounds {
		w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))

		statuses := make([]int, 2)
		responses := make([]testenv.WagerResponse, 2)
		errs := make([]error, 2)
		var start, done sync.WaitGroup
		start.Add(1)
		for i := range 2 {
			done.Add(1)
			go func() {
				defer done.Done()
				start.Wait()
				instance := instances[(round+i)%len(instances)]
				statuses[i], responses[i], errs[i] = instance.PostWager(
					testenv.BetInput(w, fmt.Sprintf("race-%d", i), 8000), bearer)
			}()
		}
		start.Done()
		done.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		assert.ElementsMatch(t, []int{http.StatusCreated, http.StatusUnprocessableEntity}, statuses)
		assert.ElementsMatch(t, []string{"", "INSUFFICIENT_FUNDS"},
			[]string{responses[0].FailureCode, responses[1].FailureCode})

		third := instances[(round+2)%len(instances)]
		for i := range 2 {
			status, response := third.SendWagerAs(t, testenv.BetInput(w, fmt.Sprintf("race-%d", i), 8000), bearer)

			assert.Equal(t, replayStatusOf[statuses[i]], status, "a resend to the third instance keeps the outcome")
			assert.True(t, response.IdempotentReplay)
			assert.Equal(t, responses[i].TransactionID, response.TransactionID)
		}

		balance, version, debits := database.WalletState(t, w.ID())
		assert.Equal(t, int64(2000), balance)
		assert.Equal(t, int64(2), version)
		assert.Equal(t, 1, debits)
		database.AssertLedgerReconciles(t, w.ID())
	}
}

func TestTheSameBetSentFiftyTimesAcrossInstancesDebitsOnce(t *testing.T) {
	t.Parallel()

	const (
		rounds   = 5
		attempts = 50
	)
	instances := []*testenv.Process{
		binary.StartProcess(t, database.URL, identityProvider),
		binary.StartProcess(t, database.URL, identityProvider),
		binary.StartProcess(t, database.URL, identityProvider),
	}
	bearer := identityProvider.Bearer(t, "provider-a")

	for range rounds {
		w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
		input := testenv.BetInput(w, "duplicated", 2500)

		statuses := make([]int, attempts)
		responses := make([]testenv.WagerResponse, attempts)
		errs := make([]error, attempts)
		var start, done sync.WaitGroup
		start.Add(1)
		for i := range attempts {
			done.Add(1)
			go func() {
				defer done.Done()
				start.Wait()
				statuses[i], responses[i], errs[i] = instances[i%len(instances)].PostWager(input, bearer)
			}()
		}
		start.Done()
		done.Wait()

		statusCounts := map[int]int{}
		transactionIDs := map[string]int{}
		for i := range attempts {
			require.NoError(t, errs[i])
			statusCounts[statuses[i]]++
			transactionIDs[responses[i].TransactionID]++
			assert.Equal(t, "75.00", responses[i].Balance.Amount)
		}
		assert.Equal(t, map[int]int{http.StatusCreated: 1, http.StatusOK: attempts - 1}, statusCounts)
		assert.Len(t, transactionIDs, 1)

		balance, version, debits := database.WalletState(t, w.ID())
		assert.Equal(t, int64(7500), balance)
		assert.Equal(t, int64(2), version)
		assert.Equal(t, 1, debits)
		database.AssertLedgerReconciles(t, w.ID())
	}
}
