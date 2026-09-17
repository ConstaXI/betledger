//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/usecase"
	"github.com/davibanfi/betledger/test/testenv"
)

func TestPublishOutboxDeliversEventsCommittedBeforeThePublisherRan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	isolated := testenv.MustStartPostgres(t)
	useCases := isolated.NewUseCases()
	queueName, queueURL := broker.CreateEventsQueue(t)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	_, err := useCases.ProcessWager.Execute(ctx, testenv.BetInput(w, "bet", 2500))
	require.NoError(t, err)
	_, err = useCases.ProcessWager.Execute(ctx, testenv.WagerInput(w, wager.KindWin, "win", 4000))
	require.NoError(t, err)
	publisher := isolated.NewPublisher(t, broker, queueName, usecase.PublicationPolicy{
		BaseDelay: time.Second, MaxDelay: time.Minute, Lease: time.Minute, BatchSize: 10,
	})

	for taken := 1; taken > 0; {
		taken, err = publisher.Execute(ctx)
		require.NoError(t, err)
	}

	recorded := isolated.OutboxEvents(t)
	received := broker.ReceiveEvents(t, queueURL, len(recorded), 10*time.Second)
	assert.Len(t, received, 6)
	assert.Equal(t, recorded.IDs(), received.IDs(), "events must arrive once each, in commit order")
	for i, e := range received {
		assert.Equal(t, w.ID().String(), e.GroupID, "the wallet is the message group")
		assert.Equal(t, e.EventID, e.DeduplicationID, "the event id deduplicates republications")
		assert.True(t, recorded[i].Published)
	}
}

func TestPublishOutboxRepublishesEventsLeftUnconfirmedOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	isolated := testenv.MustStartPostgres(t)
	useCases := isolated.NewUseCases()
	queueName, queueURL := broker.CreateEventsQueue(t)
	useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	publisher := isolated.NewPublisher(t, broker, queueName, usecase.PublicationPolicy{
		BaseDelay: time.Second, MaxDelay: time.Minute, Lease: time.Minute, BatchSize: 10,
	})

	abandoned := isolated.AbandonOutboxAfterPublishing(t, broker, queueName, 500*time.Millisecond)
	takenWhileLeased, err := publisher.Execute(ctx)
	require.NoError(t, err)
	assert.Eventually(t, func() bool {
		_, err := publisher.Execute(ctx)
		return err == nil && isolated.OutboxEvents(t).Published() == 4
	}, 10*time.Second, 100*time.Millisecond, "the abandoned events must be published again once the lease runs out")

	received := broker.ReceiveEvents(t, queueURL, 0, 3*time.Second)
	assert.Equal(t, 2, abandoned)
	assert.Equal(t, 0, takenWhileLeased, "a leased event, and the later events of its wallet, must wait")
	assert.ElementsMatch(t, isolated.OutboxEvents(t).IDs(), received.IDs(),
		"the queue must drop the republication of an event it already has")
}

func TestConcurrentPublishersDeliverEachEventOnceInWalletOrder(t *testing.T) {
	t.Parallel()

	const (
		wallets    = 8
		bets       = 3
		publishers = 3
	)
	ctx := context.Background()
	isolated := testenv.MustStartPostgres(t)
	useCases := isolated.NewUseCases()
	queueName, queueURL := broker.CreateEventsQueue(t)
	opened := make([]*wallet.Wallet, wallets)
	for i := range wallets {
		opened[i] = useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
		for bet := range bets {
			_, err := useCases.ProcessWager.Execute(ctx, testenv.BetInput(opened[i], fmt.Sprintf("bet-%d", bet), 1000))
			require.NoError(t, err)
		}
	}

	var start, done sync.WaitGroup
	start.Add(1)
	for range publishers {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			publisher := isolated.NewPublisher(t, broker, queueName, usecase.PublicationPolicy{
				BaseDelay: time.Second, MaxDelay: time.Minute, Lease: time.Minute, BatchSize: 2,
			})
			for {
				taken, err := publisher.Execute(ctx)
				if err != nil || taken == 0 {
					return
				}
			}
		}()
	}
	start.Done()
	done.Wait()

	recorded := isolated.OutboxEvents(t)
	received := broker.ReceiveEvents(t, queueURL, len(recorded), 30*time.Second)
	assert.Equal(t, len(recorded), recorded.Published())
	assert.ElementsMatch(t, recorded.IDs(), received.IDs(), "every event must be delivered exactly once")
	for _, w := range opened {
		var sent, arrived []string
		for _, e := range recorded {
			if e.AggregateID == w.ID().String() {
				sent = append(sent, e.EventID)
			}
		}
		for _, e := range received {
			if e.GroupID == w.ID().String() {
				arrived = append(arrived, e.EventID)
			}
		}
		assert.Equal(t, sent, arrived, "the events of a wallet must arrive in commit order")
	}
}

func TestWorkersPublishEventsCommittedByTheAPI(t *testing.T) {
	t.Parallel()

	isolated := testenv.MustStartPostgres(t)
	useCases := isolated.NewUseCases()
	queueName, queueURL := broker.CreateEventsQueue(t)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	application := binary.StartAPI(t, isolated.URL, identityProvider, broker)
	binary.StartWorkers(t, isolated.URL, broker,
		testenv.WithEnv("EVENTS_QUEUE_NAME", queueName),
		testenv.WithEnv("OUTBOX_POLL_INTERVAL", "50ms"))

	status, _ := application.SendWager(t, testenv.BetInput(w, "bet", 2500))
	received := broker.ReceiveEvents(t, queueURL, 4, 15*time.Second)

	require.Equal(t, http.StatusCreated, status)
	assert.Equal(t, isolated.OutboxEvents(t).IDs(), received.IDs())
	assert.Equal(t, http.StatusOK, application.Get(t, "/health/ready"))
}

func TestWorkersReportTheirDependencies(t *testing.T) {
	t.Parallel()

	isolated := testenv.MustStartPostgres(t)
	workers := binary.StartWorkers(t, isolated.URL, broker)

	assert.Equal(t, http.StatusOK, workers.Ready(t), "workers must report ready with the database and the broker up")

	isolated.Pause(t)

	assert.Eventually(t, func() bool { return workers.Ready(t) == http.StatusServiceUnavailable },
		15*time.Second, 200*time.Millisecond, "readiness must fail while the database does not answer")

	isolated.Unpause(t)

	assert.Eventually(t, func() bool { return workers.Ready(t) == http.StatusOK },
		15*time.Second, 200*time.Millisecond, "readiness must recover once the database is back")
}
