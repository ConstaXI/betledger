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

// Module starts the workers with the application. Fx stops them before the
// dependencies they use, such as the database pool, are closed.
var Module = fx.Module("worker",
	fx.Invoke(
		func(lc fx.Lifecycle, uc *usecase.ResolvePendingReferences, cfg config.Config, logger *slog.Logger) {
			newPoller(lc, "pending reference worker", uc, cfg.ReferencePollInterval, logger)
		},
		func(lc fx.Lifecycle, uc *usecase.PublishOutbox, cfg config.Config, logger *slog.Logger) {
			newPoller(lc, "outbox publisher", uc, cfg.OutboxPollInterval, logger)
		},
	),
)

type job interface {
	// Execute does one round of work and reports how many items it took.
	Execute(ctx context.Context) (int, error)
}

// poller runs a job once per interval while idle, starting one interval after
// start, and back to back while the job keeps finding work. On stop it takes no
// further work and waits for the round in progress; if the stop deadline runs
// out first, the round is cancelled, its database transaction rolls back and
// its leases hand the work to another instance.
type poller struct {
	name     string
	job      job
	interval time.Duration
	logger   *slog.Logger
	stopping chan struct{}
	done     chan struct{}
}

func newPoller(lc fx.Lifecycle, name string, job job, interval time.Duration, logger *slog.Logger) *poller {
	p := &poller{
		name:     name,
		job:      job,
		interval: interval,
		logger:   logger,
		stopping: make(chan struct{}),
		done:     make(chan struct{}),
	}
	workCtx, cancelWork := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go p.run(workCtx)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			close(p.stopping)
			select {
			case <-p.done:
				cancelWork()
				return nil
			case <-ctx.Done():
				cancelWork()
				<-p.done
				return ctx.Err()
			}
		},
	})
	return p
}

func (p *poller) run(ctx context.Context) {
	defer close(p.done)
	p.logger.InfoContext(ctx, p.name+" started", "pollInterval", p.interval.String())
	defer p.logger.Info(p.name + " stopped")

	timer := time.NewTimer(p.interval)
	defer timer.Stop()
	for {
		select {
		case <-p.stopping:
			return
		case <-timer.C:
		}

		taken, err := p.job.Execute(ctx)
		if err != nil && ctx.Err() == nil {
			p.logger.ErrorContext(ctx, p.name+" round failed", "error", err, "taken", taken)
		}
		if taken > 0 {
			timer.Reset(0)
		} else {
			timer.Reset(p.interval)
		}
	}
}
