//go:build integration

package testenv

import (
	"context"
	"fmt"
	"log/slog"
	"net"
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
	Client
	fxApp *fx.App
}

// StartApplication starts the application against the database and the
// identity provider, stopping it on cleanup. Options adjust the configuration
// before start. Logs are discarded.
func StartApplication(
	t *testing.T,
	databaseURL string,
	identityProvider *Keycloak,
	options ...func(cfg *config.Config),
) *Application {
	t.Helper()

	cfg := config.Config{
		HTTPPort:         freePort(t),
		DatabaseURL:      databaseURL,
		OIDCIssuerURL:    identityProvider.IssuerURL,
		OIDCDiscoveryURL: identityProvider.IssuerURL,
		OIDCAudience:     Audience,
		// The worker of an application started by a test never runs unless the
		// test shortens the interval, so it does not touch the operations other
		// tests left waiting in the shared database.
		ReferenceMaxAttempts:    8,
		ReferenceRetryBaseDelay: time.Minute,
		ReferenceRetryMaxDelay:  5 * time.Minute,
		ReferenceRetryLease:     30 * time.Second,
		ReferencePollInterval:   time.Hour,
	}
	for _, option := range options {
		option(&cfg)
	}
	fxApp := fx.New(
		fx.Supply(cfg),
		app.Options(),
		fx.Decorate(func(*slog.Logger) *slog.Logger { return slog.New(slog.DiscardHandler) }),
		fx.NopLogger,
	)
	require.NoError(t, fxApp.Err())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	require.NoError(t, fxApp.Start(ctx))

	application := &Application{
		Client: Client{BaseURL: "http://127.0.0.1:" + cfg.HTTPPort, identityProvider: identityProvider},
		fxApp:  fxApp,
	}
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

func freePort(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)
}
