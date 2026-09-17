// Package app composes the application with Fx. It is the composition root,
// shared by the entrypoint and by the tests that start the real application.
package app

import (
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/davibanfi/betledger/internal/infrastructure/auth"
	"github.com/davibanfi/betledger/internal/infrastructure/config"
	"github.com/davibanfi/betledger/internal/infrastructure/httpserver"
	"github.com/davibanfi/betledger/internal/infrastructure/messaging"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres"
	"github.com/davibanfi/betledger/internal/infrastructure/worker"
	"github.com/davibanfi/betledger/internal/usecase"
)

// Options composes the application without its configuration, which the
// entrypoint loads from the environment and tests supply directly.
func Options() fx.Option {
	return fx.Options(
		fx.WithLogger(func(logger *slog.Logger) fxevent.Logger {
			return &fxevent.SlogLogger{Logger: logger}
		}),
		fx.Provide(
			newLogger,
			newClock,
			usecase.NewOpenWallet,
			usecase.NewProcessWager,
			newReferenceRetryPolicy,
			usecase.NewResolvePendingReferences,
			newPublicationPolicy,
			usecase.NewPublishOutbox,
			messaging.NewClient,
			fx.Annotate(messaging.NewEventPublisher, fx.As(fx.Self()), fx.As(new(usecase.EventPublisher))),
			fx.Annotate(newSQSHealthCheck, fx.ResultTags(`group:"readiness"`)),
			fx.Annotate(newPostgresHealthCheck, fx.ResultTags(`group:"readiness"`)),
			fx.Annotate(auth.NewTokenVerifier, fx.As(new(httpserver.TokenVerifier))),
		),
		postgres.Module,
		httpserver.Module,
		worker.Module,
	)
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

func newClock() usecase.Clock {
	return time.Now
}

// pendingReferenceBatchSize bounds how many waiting operations a worker takes
// in one go, so that a single run never holds many leases at once.
const pendingReferenceBatchSize = 50

func newReferenceRetryPolicy(cfg config.Config) usecase.ReferenceRetryPolicy {
	return usecase.ReferenceRetryPolicy{
		MaxAttempts: cfg.ReferenceMaxAttempts,
		BaseDelay:   cfg.ReferenceRetryBaseDelay,
		MaxDelay:    cfg.ReferenceRetryMaxDelay,
		Lease:       cfg.ReferenceRetryLease,
		BatchSize:   pendingReferenceBatchSize,
	}
}

// outboxBatchSize bounds how many events a publisher takes in one go.
const outboxBatchSize = 100

func newPublicationPolicy(cfg config.Config) usecase.PublicationPolicy {
	return usecase.PublicationPolicy{
		BaseDelay: cfg.OutboxRetryBaseDelay,
		MaxDelay:  cfg.OutboxRetryMaxDelay,
		Lease:     cfg.OutboxLease,
		BatchSize: outboxBatchSize,
	}
}

func newSQSHealthCheck(publisher *messaging.EventPublisher) httpserver.HealthCheck {
	return httpserver.HealthCheck{Name: "sqs", Check: publisher.Check}
}

func newPostgresHealthCheck(pool *pgxpool.Pool) httpserver.HealthCheck {
	return httpserver.HealthCheck{Name: "postgres", Check: pool.Ping}
}
