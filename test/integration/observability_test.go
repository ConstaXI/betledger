//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/test/testenv"
)

func TestMetricsReportTheOperationsOfTheAPI(t *testing.T) {
	t.Parallel()

	application := binary.StartAPI(t, database.URL, identityProvider, broker)
	w := useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))

	status, _ := application.SendWager(t, testenv.BetInput(w, "metrics-bet", 2500))
	scraped := application.Scrape(t)

	require.Equal(t, http.StatusCreated, status)
	assert.Contains(t, scraped,
		`betledger_wager_operations_total{failure_code="",kind="BET",replay="false",status="PROCESSED"} 1`,
		"the operation must be counted by kind, status and failure code")
	assert.Contains(t, scraped,
		`betledger_http_duration_seconds_count{method="POST",route="POST /wagering/transactions",status="201"} 1`,
		"the request must be timed under the route that matched, never the path")
	assert.NotContains(t, scraped, w.ID().String(), "a metric must carry no identifier")
}

func TestMetricsReportThePublicationsOfTheWorkers(t *testing.T) {
	t.Parallel()

	isolated := testenv.MustStartPostgres(t)
	useCases := isolated.NewUseCases()
	queueName, _ := broker.CreateEventsQueue(t)
	useCases.MustOpenWallet(t, money.MustNew(10000, money.MustCurrency("BRL")))
	workers := binary.StartWorkers(t, isolated.URL, broker,
		testenv.WithEnv("EVENTS_QUEUE_NAME", queueName),
		testenv.WithEnv("OUTBOX_POLL_INTERVAL", "50ms"))

	assert.Eventually(t, func() bool {
		return strings.Contains(workers.Scrape(t), `betledger_outbox_publications_total{published="true"} 2`)
	}, 15*time.Second, 200*time.Millisecond, "the workers must count the events they published")
	assert.Contains(t, workers.Scrape(t), "betledger_outbox_delay_seconds_count 2",
		"the delay between recording an event and publishing it must be measured")
}
