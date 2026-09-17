//go:build integration

package integration

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/infrastructure/config"
	"github.com/davibanfi/betledger/internal/usecase"
	"github.com/davibanfi/betledger/test/testenv"
)

// The cases share a database of their own and run one after another, because
// the resolver takes every due operation in the database, including those other
// cases left waiting.
func TestResolvePendingReferencesExecute(t *testing.T) {
	t.Parallel()

	isolated := testenv.MustStartPostgres(t)
	useCases := isolated.NewUseCases()
	type step = func(w *wallet.Wallet) usecase.ProcessWagerInput
	refundOfBet := func(w *wallet.Wallet) usecase.ProcessWagerInput {
		return testenv.ReferringInput(w, wager.KindRefund, "refund", "bet", 2500)
	}
	bet := func(w *wallet.Wallet) usecase.ProcessWagerInput { return testenv.BetInput(w, "bet", 2500) }

	tests := []struct {
		name            string
		maxAttempts     int
		earlier         []step
		runsBefore      int
		later           []step
		wantState       string
		wantFailureCode string
		wantAttempts    int
		wantStored      int64
	}{
		{
			name:        "should accept when the bet arrived after the refund",
			maxAttempts: 3,
			earlier:     []step{refundOfBet, bet},
			wantState:   "PROCESSED",
			wantStored:  10000,
		},
		{
			name:         "should report the refund still waiting when the bet has not arrived",
			maxAttempts:  3,
			earlier:      []step{refundOfBet},
			wantState:    "PENDING_REFERENCE",
			wantAttempts: 1,
			wantStored:   10000,
		},
		{
			name:         "should accept when the bet arrives between attempts",
			maxAttempts:  3,
			earlier:      []step{refundOfBet},
			runsBefore:   1,
			later:        []step{bet},
			wantState:    "PROCESSED",
			wantAttempts: 1,
			wantStored:   10000,
		},
		{
			name:            "should return REFERENCE_NOT_FOUND when the attempts run out",
			maxAttempts:     2,
			earlier:         []step{refundOfBet},
			runsBefore:      1,
			wantState:       "REJECTED",
			wantFailureCode: "REFERENCE_NOT_FOUND",
			wantAttempts:    2,
			wantStored:      10000,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			resolver := isolated.NewResolver(usecase.ReferenceRetryPolicy{
				MaxAttempts: test.maxAttempts,
				BaseDelay:   time.Nanosecond,
				MaxDelay:    time.Nanosecond,
				Lease:       time.Minute,
				BatchSize:   50,
			})
			w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
			for _, earlier := range test.earlier {
				_, err := useCases.ProcessWager.Execute(ctx, earlier(w))
				require.NoError(t, err)
			}
			for range test.runsBefore {
				_, err := resolver.Execute(ctx)
				require.NoError(t, err)
			}
			for _, later := range test.later {
				_, err := useCases.ProcessWager.Execute(ctx, later(w))
				require.NoError(t, err)
			}

			_, err := resolver.Execute(ctx)

			stored, _, _ := isolated.WalletState(t, w.ID())
			assert.NoError(t, err)
			assert.Equal(t, testenv.OperationRecord{
				State:             test.wantState,
				FailureCode:       test.wantFailureCode,
				ReferenceAttempts: test.wantAttempts,
			}, isolated.Operation(t, w, "refund"))
			assert.Equal(t, test.wantStored, stored)
			isolated.AssertLedgerReconciles(t, w.ID())
		})
	}
}

func TestConcurrentResolversConcludeEachOperationOnce(t *testing.T) {
	t.Parallel()

	const (
		wallets   = 20
		resolvers = 4
	)
	ctx := context.Background()
	isolated := testenv.MustStartPostgres(t)
	useCases := isolated.NewUseCases()

	opened := make([]*wallet.Wallet, wallets)
	for i := range wallets {
		opened[i] = useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
		_, err := useCases.ProcessWager.Execute(ctx, testenv.ReferringInput(opened[i], wager.KindRefund, "refund", "bet", 2500))
		require.NoError(t, err)
		_, err = useCases.ProcessWager.Execute(ctx, testenv.BetInput(opened[i], "bet", 2500))
		require.NoError(t, err)
	}

	var start, done sync.WaitGroup
	start.Add(1)
	for range resolvers {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			resolver := isolated.NewResolver(usecase.ReferenceRetryPolicy{
				MaxAttempts: 8, BaseDelay: time.Second, MaxDelay: time.Second, Lease: time.Minute, BatchSize: 3,
			})
			for {
				taken, err := resolver.Execute(ctx)
				if err != nil || taken == 0 {
					return
				}
			}
		}()
	}
	start.Done()
	done.Wait()

	for _, w := range opened {
		stored, version, _ := isolated.WalletState(t, w.ID())
		assert.Equal(t, "PROCESSED", isolated.Operation(t, w, "refund").State)
		assert.Equal(t, int64(10000), stored, "the bet must be refunded exactly once")
		assert.Equal(t, int64(3), version)
		assert.Equal(t, map[string]int{"WagerTransactionProcessed": 3, "WalletBalanceChanged": 3,
			"WagerTransactionPendingReference": 1}, isolated.EventCounts(t, w.ID()))
		isolated.AssertLedgerReconciles(t, w.ID())
	}
}

func TestAbandonedPendingReferencesAreTakenAgainWhenTheLeaseRunsOut(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	isolated := testenv.MustStartPostgres(t)
	useCases := isolated.NewUseCases()
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	_, err := useCases.ProcessWager.Execute(ctx, testenv.ReferringInput(w, wager.KindRefund, "refund", "bet", 2500))
	require.NoError(t, err)
	_, err = useCases.ProcessWager.Execute(ctx, testenv.BetInput(w, "bet", 2500))
	require.NoError(t, err)
	resolver := isolated.NewResolver(usecase.ReferenceRetryPolicy{
		MaxAttempts: 8, BaseDelay: time.Second, MaxDelay: time.Second, Lease: time.Minute, BatchSize: 50,
	})

	abandoned := isolated.AbandonPendingReferences(t, 500*time.Millisecond)
	takenWhileLeased, err := resolver.Execute(ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, abandoned)
	assert.Equal(t, 0, takenWhileLeased, "a leased operation must not be taken by another worker")
	assert.Eventually(t, func() bool {
		_, err := resolver.Execute(ctx)
		return err == nil && isolated.Operation(t, w, "refund").State == "PROCESSED"
	}, 5*time.Second, 100*time.Millisecond, "the operation must be taken again once the lease runs out")
}

func TestApplicationResolvesPendingReferencesAfterRestart(t *testing.T) {
	t.Parallel()

	isolated := testenv.MustStartPostgres(t)
	useCases := isolated.NewUseCases()
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))

	first := testenv.StartApplication(t, isolated.URL, identityProvider, broker)
	status, _ := first.SendWager(t, testenv.ReferringInput(w, wager.KindRefund, "refund", "bet", 2500))
	require.Equal(t, http.StatusAccepted, status)
	first.Stop(t)

	_, err := useCases.ProcessWager.Execute(context.Background(), testenv.BetInput(w, "bet", 2500))
	require.NoError(t, err)
	testenv.StartWorkers(t, isolated.URL, broker, func(cfg *config.Config) {
		cfg.ReferencePollInterval = 50 * time.Millisecond
	})

	assert.Eventually(t, func() bool { return isolated.Operation(t, w, "refund").State == "PROCESSED" },
		10*time.Second, 100*time.Millisecond, "the workers started afterwards must resolve the refund")
	stored, _, _ := isolated.WalletState(t, w.ID())
	assert.Equal(t, int64(10000), stored)
	isolated.AssertLedgerReconciles(t, w.ID())
}
