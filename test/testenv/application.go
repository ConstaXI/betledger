//go:build integration

package testenv

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/app"
	"github.com/davibanfi/betledger/internal/infrastructure/config"
)

// Application is the real application, composed by Fx and listening on a free
// local port.
type Application struct {
	// BaseURL reaches the HTTP server, such as http://127.0.0.1:41234.
	BaseURL string
	fxApp   *fx.App
}

// StartApplication starts the application against the database, stopping it on
// cleanup. Logs are discarded.
func StartApplication(t *testing.T, databaseURL string) *Application {
	t.Helper()

	port := freePort(t)
	fxApp := fx.New(
		fx.Supply(config.Config{HTTPPort: port, DatabaseURL: databaseURL}),
		app.Options(),
		fx.Decorate(func(*slog.Logger) *slog.Logger { return slog.New(slog.DiscardHandler) }),
		fx.NopLogger,
	)
	require.NoError(t, fxApp.Err())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	require.NoError(t, fxApp.Start(ctx))

	application := &Application{BaseURL: "http://127.0.0.1:" + port, fxApp: fxApp}
	t.Cleanup(func() { _ = application.stop() })
	return application
}

// Stop runs the shutdown hooks, as a SIGTERM would.
func (a *Application) Stop(t *testing.T) {
	t.Helper()

	require.NoError(t, a.stop())
}

func (a *Application) stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.fxApp.Stop(ctx)
}

// Get returns the status of a GET to the path.
func (a *Application) Get(t *testing.T, path string) int {
	t.Helper()

	response, err := httpClient().Get(a.BaseURL + path)
	require.NoError(t, err)
	defer response.Body.Close()
	return response.StatusCode
}

// IsListening reports whether the server still accepts connections.
func (a *Application) IsListening() bool {
	response, err := (&http.Client{Timeout: time.Second}).Get(a.BaseURL + "/health/live")
	if err != nil {
		return false
	}
	response.Body.Close()
	return true
}

// OpenWallet returns the status of opening a 100.00 BRL wallet for the player.
func (a *Application) OpenWallet(t *testing.T, playerID string) int {
	t.Helper()

	body := fmt.Sprintf(`{"playerId":%q,"initialBalance":{"amount":"100.00","currency":"BRL"}}`, playerID)
	response, err := httpClient().Post(a.BaseURL+"/wallets", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer response.Body.Close()
	return response.StatusCode
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

func freePort(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)
}
