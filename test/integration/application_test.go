//go:build integration

package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/test/testenv"
)

func TestApplicationStartsServesAndReleasesResources(t *testing.T) {
	t.Parallel()

	const applicationName = "betledger-lifecycle"
	application := testenv.StartApplication(t, database.URLWithApplicationName(applicationName))
	playerID := domain.NewID().String()

	assert.Equal(t, http.StatusOK, application.Get(t, "/health/ready"))
	assert.Equal(t, http.StatusCreated, application.OpenWallet(t, playerID))
	assert.Equal(t, http.StatusConflict, application.OpenWallet(t, playerID))
	assert.Positive(t, database.Connections(t, applicationName), "the running application should hold connections")

	application.Stop(t)

	assert.False(t, application.IsListening(), "the server must stop accepting connections")
	assert.Eventually(t, func() bool { return database.Connections(t, applicationName) == 0 },
		5*time.Second, 100*time.Millisecond, "the connection pool must be closed on stop")
}

func TestApplicationKeepsStateAcrossRestart(t *testing.T) {
	t.Parallel()

	playerID := domain.NewID().String()

	first := testenv.StartApplication(t, database.URL)
	assert.Equal(t, http.StatusCreated, first.OpenWallet(t, playerID))
	first.Stop(t)

	restarted := testenv.StartApplication(t, database.URL)

	assert.Equal(t, http.StatusConflict, restarted.OpenWallet(t, playerID),
		"the wallet opened before the restart must still exist")
}

func TestApplicationAnswersUnavailableWhileTheDatabaseIsDown(t *testing.T) {
	t.Parallel()

	isolated := testenv.MustStartPostgres(t)
	application := testenv.StartApplication(t, isolated.URL)
	require.Equal(t, http.StatusOK, application.Get(t, "/health/ready"))

	isolated.Pause(t)

	assert.Equal(t, http.StatusServiceUnavailable, application.Get(t, "/health/ready"),
		"readiness must fail while the database does not answer")
	assert.Equal(t, http.StatusOK, application.Get(t, "/health/live"),
		"liveness does not depend on the database")
	started := time.Now()
	assert.Equal(t, http.StatusServiceUnavailable, application.OpenWallet(t, domain.NewID().String()),
		"a write must be answered as transient unavailability instead of hanging")
	assert.Less(t, time.Since(started), 8*time.Second)

	isolated.Unpause(t)

	assert.Eventually(t, func() bool { return application.Get(t, "/health/ready") == http.StatusOK },
		15*time.Second, 200*time.Millisecond, "readiness must recover once the database is back")
}
