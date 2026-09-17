// Command workers runs the background workers of betledger: the retry of
// operations waiting for a reference and the publication of the outbox. It
// composes its own dependencies with Fx, serves no HTTP, and several instances
// may run at once, because all coordination lives in the database.
package main

import (
	"log/slog"
	"os"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/davibanfi/betledger/internal/infrastructure/config"
	"github.com/davibanfi/betledger/internal/infrastructure/messaging"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres"
	"github.com/davibanfi/betledger/internal/infrastructure/worker"
	"github.com/davibanfi/betledger/internal/usecase"
)

// pendingReferenceBatchSize bounds how many waiting operations a worker takes
// in one go, so that a single round never holds many leases at once.
const pendingReferenceBatchSize = 50

// outboxBatchSize bounds how many events a publisher takes in one go.
const outboxBatchSize = 100

func main() {
	fx.New(fx.Provide(config.Load), options()).Run()
}

func options() fx.Option {
	return fx.Options(
		fx.WithLogger(func(logger *slog.Logger) fxevent.Logger {
			return &fxevent.SlogLogger{Logger: logger}
		}),
		fx.Provide(
			newLogger,
			newClock,
			usecase.NewProcessWager,
			newReferenceRetryPolicy,
			usecase.NewResolvePendingReferences,
			newPublicationPolicy,
			usecase.NewPublishOutbox,
			messaging.NewClient,
			fx.Annotate(messaging.NewEventPublisher, fx.As(new(usecase.EventPublisher))),
		),
		postgres.Module,
		worker.Module,
	)
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

func newClock() usecase.Clock {
	return time.Now
}

func newReferenceRetryPolicy(cfg config.Config) usecase.ReferenceRetryPolicy {
	return usecase.ReferenceRetryPolicy{
		MaxAttempts: cfg.ReferenceMaxAttempts,
		BaseDelay:   cfg.ReferenceRetryBaseDelay,
		MaxDelay:    cfg.ReferenceRetryMaxDelay,
		Lease:       cfg.ReferenceRetryLease,
		BatchSize:   pendingReferenceBatchSize,
	}
}

func newPublicationPolicy(cfg config.Config) usecase.PublicationPolicy {
	return usecase.PublicationPolicy{
		BaseDelay: cfg.OutboxRetryBaseDelay,
		MaxDelay:  cfg.OutboxRetryMaxDelay,
		Lease:     cfg.OutboxLease,
		BatchSize: outboxBatchSize,
	}
}
