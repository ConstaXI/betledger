//go:build integration

package postgres_test

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
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/usecase"
)

func TestTwoConcurrentBetsNeverOverdrawTheWallet(t *testing.T) {
	t.Parallel()

	const rounds = 20
	ctx := context.Background()

	for round := range rounds {
		w := openTestWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))

		results := make([]usecase.WagerResult, 2)
		errs := make([]error, 2)
		var start, done sync.WaitGroup
		start.Add(1)
		for i := range 2 {
			done.Add(1)
			go func() {
				defer done.Done()
				start.Wait()
				input := betInput(w, fmt.Sprintf("race-%d-%d", round, i), 8000)
				results[i], errs[i] = processWager.Execute(ctx, input)
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

		balance, version, debits := walletState(t, w.ID())
		assert.Equal(t, int64(2000), balance)
		assert.Equal(t, int64(2), version)
		assert.Equal(t, 1, debits)
		assertLedgerReconciles(t, w.ID())
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
		w := openTestWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
		input := betInput(w, "duplicated", 2500)

		results := make([]usecase.WagerResult, attempts)
		errs := make([]error, attempts)
		var start, done sync.WaitGroup
		start.Add(1)
		for i := range attempts {
			done.Add(1)
			go func() {
				defer done.Done()
				start.Wait()
				results[i], errs[i] = processWager.Execute(ctx, input)
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

		balance, version, debits := walletState(t, w.ID())
		assert.Equal(t, int64(7500), balance)
		assert.Equal(t, int64(2), version)
		assert.Equal(t, 1, debits)
		assertLedgerReconciles(t, w.ID())
	}
}

func TestIndependentWalletsAreProcessedConcurrently(t *testing.T) {
	t.Parallel()

	const wallets = 10
	ctx := context.Background()

	opened := make([]*wallet.Wallet, wallets)
	for i := range wallets {
		opened[i] = openTestWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	}

	errs := make([]error, wallets)
	var done sync.WaitGroup
	for i := range wallets {
		done.Add(1)
		go func() {
			defer done.Done()
			_, errs[i] = processWager.Execute(ctx, betInput(opened[i], "independent", 3000))
		}()
	}
	done.Wait()

	for i := range wallets {
		require.NoError(t, errs[i])
		balance, _, debits := walletState(t, opened[i].ID())
		assert.Equal(t, int64(7000), balance)
		assert.Equal(t, 1, debits)
		assertLedgerReconciles(t, opened[i].ID())
	}
}

func TestProcessWagerPersistsRejectionsAndReplaysTheOriginalBalance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	brl := money.MustCurrency("BRL")
	w := openTestWallet(t, money.MustNew(10000, brl))

	first, err := processWager.Execute(ctx, betInput(w, "first", 2500))
	require.NoError(t, err)
	_, err = processWager.Execute(ctx, betInput(w, "second", 2500))
	require.NoError(t, err)
	rejected, err := processWager.Execute(ctx, betInput(w, "too-large", 9000))
	require.NoError(t, err)

	replayedFirst, err := processWager.Execute(ctx, betInput(w, "first", 2500))
	require.NoError(t, err)
	replayedRejection, err := processWager.Execute(ctx, betInput(w, "too-large", 9000))
	require.NoError(t, err)

	assert.Equal(t, first.TransactionID, replayedFirst.TransactionID)
	assert.True(t, replayedFirst.IdempotentReplay)
	assert.Equal(t, money.MustNew(7500, brl), replayedFirst.Balance)

	assert.Equal(t, wager.StateRejected, rejected.State)
	assert.Equal(t, rejected.TransactionID, replayedRejection.TransactionID)
	assert.Equal(t, domain.FailureCodeInsufficientFunds, replayedRejection.FailureCode)
	assert.True(t, replayedRejection.IdempotentReplay)

	balance, version, debits := walletState(t, w.ID())
	assert.Equal(t, int64(5000), balance)
	assert.Equal(t, int64(3), version)
	assert.Equal(t, 2, debits)
	assertLedgerReconciles(t, w.ID())

	var rejectedEvents int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'WagerTransactionRejected'",
		w.ID()).Scan(&rejectedEvents))
	assert.Equal(t, 1, rejectedEvents)
}

func TestProcessWagerReportsIdempotencyConflicts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	w := openTestWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	_, err := processWager.Execute(ctx, betInput(w, "conflict", 2500))
	require.NoError(t, err)

	changedPayload := betInput(w, "conflict", 3000)
	otherKey := betInput(w, "conflict", 2500)
	otherKey.IdempotencyKey = "provider-a:conflict:another-key"

	_, errChangedPayload := processWager.Execute(ctx, changedPayload)
	_, errOtherKey := processWager.Execute(ctx, otherKey)

	assert.ErrorIs(t, errChangedPayload, domain.FailureCodeIdempotencyConflict)
	assert.ErrorIs(t, errOtherKey, domain.FailureCodeIdempotencyConflict)
	balance, _, debits := walletState(t, w.ID())
	assert.Equal(t, int64(7500), balance)
	assert.Equal(t, 1, debits)
}

func openTestWallet(t *testing.T, balance money.Money) *wallet.Wallet {
	t.Helper()

	w, err := openWallet.Execute(context.Background(), usecase.OpenWalletInput{
		PlayerID:       domain.NewID(),
		InitialBalance: balance,
		CorrelationID:  "req-seed",
	})
	require.NoError(t, err)
	return w
}

func betInput(w *wallet.Wallet, externalID string, amountMinor int64) usecase.ProcessWagerInput {
	scoped := w.ID().String() + ":" + externalID
	return usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: scoped,
		IdempotencyKey:        "provider-a:" + scoped,
		PlayerID:              w.PlayerID(),
		WalletID:              w.ID(),
		RoundID:               "round-1",
		GameID:                "fortune-chimp",
		Kind:                  wager.KindBet,
		Money:                 money.MustNew(amountMinor, w.Currency()),
		CorrelationID:         "req-" + externalID,
	}
}

func walletState(t *testing.T, walletID domain.ID) (balance, version int64, debits int) {
	t.Helper()

	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT w.balance_minor, w.version,
			(SELECT count(*) FROM wallet_ledger_entries l WHERE l.wallet_id = w.id AND l.direction = 'DEBIT')
		FROM wallets w WHERE w.id = $1`, walletID).Scan(&balance, &version, &debits))
	return balance, version, debits
}

func assertLedgerReconciles(t *testing.T, walletID domain.ID) {
	t.Helper()

	var stored, rebuilt int64
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT w.balance_minor,
			COALESCE(SUM(CASE l.direction WHEN 'CREDIT' THEN l.amount_minor ELSE -l.amount_minor END), 0)
		FROM wallets w
		LEFT JOIN wallet_ledger_entries l ON l.wallet_id = w.id
		WHERE w.id = $1
		GROUP BY w.balance_minor`, walletID).Scan(&stored, &rebuilt))
	assert.Equal(t, stored, rebuilt, "stored balance must equal credits minus debits in the ledger")
}
