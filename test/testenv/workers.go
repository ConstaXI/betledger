//go:build integration

package testenv

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/app"
	"github.com/davibanfi/betledger/internal/infrastructure/config"
)

// Workers is the background application, composed by Fx as the workers
// entrypoint composes it.
type Workers struct {
	fxApp *fx.App
}

// StartWorkers starts the workers against the database and the broker,
// stopping them on cleanup. Options adjust the configuration before start;
// without them the workers poll once an hour, which keeps them from touching
// the work of other tests. Logs are discarded.
func StartWorkers(
	t *testing.T,
	databaseURL string,
	broker *LocalStack,
	options ...func(cfg *config.Config),
) *Workers {
	t.Helper()

	cfg := config.Config{
		HTTPPort:                freePort(t),
		DatabaseURL:             databaseURL,
		OIDCIssuerURL:           "http://identity.invalid/realms/betledger",
		OIDCAudience:            Audience,
		ReferenceMaxAttempts:    8,
		ReferenceRetryBaseDelay: time.Millisecond,
		ReferenceRetryMaxDelay:  time.Second,
		ReferenceRetryLease:     30 * time.Second,
		ReferencePollInterval:   time.Hour,
		AWSRegion:               "us-east-1",
		AWSEndpointURL:          broker.EndpointURL,
		EventsQueueName:         EventsQueueName,
		OutboxPollInterval:      time.Hour,
		OutboxRetryBaseDelay:    time.Millisecond,
		OutboxRetryMaxDelay:     time.Second,
		OutboxLease:             30 * time.Second,
	}
	for _, option := range options {
		option(&cfg)
	}
	fxApp := fx.New(
		fx.Supply(cfg),
		app.Workers(),
		fx.Decorate(func(*slog.Logger) *slog.Logger { return slog.New(slog.DiscardHandler) }),
		fx.NopLogger,
	)
	require.NoError(t, fxApp.Err())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	require.NoError(t, fxApp.Start(ctx))

	workers := &Workers{fxApp: fxApp}
	t.Cleanup(func() { _ = workers.stop() })
	return workers
}

// Stop runs the shutdown hooks, as a SIGTERM would.
func (w *Workers) Stop(t *testing.T) {
	t.Helper()

	require.NoError(t, w.stop())
}

func (w *Workers) stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return w.fxApp.Stop(ctx)
}
