// Command workers runs the background workers of betledger: the retry of
// operations waiting for a reference, the publication of the outbox and it
// consumes the operations sent to the wagering queue. It composes its own
// dependencies with Fx and serves only health endpoints, and several instances
// may run at once, because all coordination lives in the database and in the
// queue.
package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/davibanfi/betledger/internal/infrastructure/config"
	"github.com/davibanfi/betledger/internal/infrastructure/httpserver"
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
			usecase.NewProcessInboxMessage,
			newReferenceRetryPolicy,
			usecase.NewResolvePendingReferences,
			newPublicationPolicy,
			usecase.NewPublishOutbox,
			messaging.NewClient,
			fx.Annotate(messaging.NewEventPublisher, fx.As(fx.Self()), fx.As(new(usecase.EventPublisher))),
			messaging.NewWagerConsumer,
			fx.Annotate(newPostgresHealthCheck, fx.ResultTags(`group:"readiness"`)),
			fx.Annotate(newEventsQueueHealthCheck, fx.ResultTags(`group:"readiness"`)),
			fx.Annotate(newWagerQueueHealthCheck, fx.ResultTags(`group:"readiness"`)),
		),
		postgres.Module,
		httpserver.HealthModule,
		worker.Module,
	)
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

func newClock() usecase.Clock {
	return time.Now
}

func newPostgresHealthCheck(pool *pgxpool.Pool) httpserver.HealthCheck {
	return httpserver.HealthCheck{Name: "postgres", Check: pool.Ping}
}

func newEventsQueueHealthCheck(publisher *messaging.EventPublisher) httpserver.HealthCheck {
	return httpserver.HealthCheck{Name: "sqs-events", Check: publisher.Check}
}

func newWagerQueueHealthCheck(consumer *messaging.WagerConsumer) httpserver.HealthCheck {
	return httpserver.HealthCheck{Name: "sqs-wagering", Check: consumer.Check}
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
