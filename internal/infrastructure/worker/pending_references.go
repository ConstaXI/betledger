// Package worker runs the background jobs of the application, with their
// lifecycle managed by Fx.
package worker

import (
	"context"
	"log/slog"
	"time"

	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/infrastructure/config"
	"github.com/davibanfi/betledger/internal/usecase"
)

// Module starts the workers with the application and stops them before the
// dependencies they use are closed.
var Module = fx.Module("worker",
	fx.Provide(NewPendingReferences),
	fx.Invoke(func(*PendingReferences) {}),
)

type referenceResolver interface {
	Execute(ctx context.Context) (int, error)
}

// PendingReferences retries the operations waiting for a reference, polling
// for due ones once per interval while idle, starting one interval after start,
// and draining them back to back while there is work.
type PendingReferences struct {
	resolver referenceResolver
	interval time.Duration
	logger   *slog.Logger
	stopping chan struct{}
	done     chan struct{}
}

// NewPendingReferences builds the worker and registers it in the lifecycle.
// On stop it takes no further work and waits for the attempt in progress; if
// the stop deadline runs out first, the attempt is cancelled and its database
// transaction rolls back, leaving the lease to hand the operations to another
// worker.
func NewPendingReferences(
	lc fx.Lifecycle,
	resolver *usecase.ResolvePendingReferences,
	cfg config.Config,
	logger *slog.Logger,
) *PendingReferences {
	return newPendingReferences(lc, resolver, cfg.ReferencePollInterval, logger)
}

func newPendingReferences(
	lc fx.Lifecycle,
	resolver referenceResolver,
	interval time.Duration,
	logger *slog.Logger,
) *PendingReferences {
	worker := &PendingReferences{
		resolver: resolver,
		interval: interval,
		logger:   logger,
		stopping: make(chan struct{}),
		done:     make(chan struct{}),
	}
	workCtx, cancelWork := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go worker.run(workCtx)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			close(worker.stopping)
			select {
			case <-worker.done:
				cancelWork()
				return nil
			case <-ctx.Done():
				cancelWork()
				<-worker.done
				return ctx.Err()
			}
		},
	})
	return worker
}

func (w *PendingReferences) run(ctx context.Context) {
	defer close(w.done)
	w.logger.InfoContext(ctx, "pending reference worker started", "pollInterval", w.interval.String())
	defer w.logger.Info("pending reference worker stopped")

	timer := time.NewTimer(w.interval)
	defer timer.Stop()
	for {
		select {
		case <-w.stopping:
			return
		case <-timer.C:
		}

		taken, err := w.resolver.Execute(ctx)
		if err != nil && ctx.Err() == nil {
			w.logger.ErrorContext(ctx, "pending reference attempt failed", "error", err, "taken", taken)
		}
		if taken > 0 {
			timer.Reset(0)
		} else {
			timer.Reset(w.interval)
		}
	}
}
