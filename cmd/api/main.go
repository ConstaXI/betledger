// Command api serves the HTTP endpoints of betledger. It composes its own
// dependencies with Fx and runs no background worker, so that serving requests
// and draining work scale apart. It never reaches the broker: events are
// recorded in the outbox, within the same commit, and published by the workers.
package main

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
	"github.com/davibanfi/betledger/internal/infrastructure/logging"
	"github.com/davibanfi/betledger/internal/infrastructure/metrics"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres"
	"github.com/davibanfi/betledger/internal/usecase"
)

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
			usecase.NewOpenWallet,
			usecase.NewProcessWager,
			usecase.NewReadWallet,
			usecase.NewReadTransaction,
			fx.Annotate(auth.NewTokenVerifier, fx.As(new(httpserver.TokenVerifier))),
			newRequestRecorder,
			fx.Annotate(metrics.NewRoute, fx.As(new(httpserver.Route)), fx.ResultTags(`group:"public_routes"`)),
			fx.Annotate(newPostgresHealthCheck, fx.ResultTags(`group:"readiness"`)),
		),
		metrics.Module,
		postgres.Module,
		httpserver.Module,
	)
}

func newLogger() *slog.Logger {
	return logging.New(os.Stdout)
}

// newClock reads the time at the precision PostgreSQL stores, so that an entity
// reports the same instants before and after it is persisted: the response to
// the request that created it and a later read must agree.
func newClock() usecase.Clock {
	return func() time.Time {
		return time.Now().UTC().Truncate(time.Microsecond)
	}
}

// newRequestRecorder hands the server the part of the recorder it uses, which
// keeps the metrics of the requests out of the routes and the handlers.
func newRequestRecorder(recorder *metrics.Recorder) httpserver.RequestRecorder {
	return recorder
}

func newPostgresHealthCheck(pool *pgxpool.Pool) httpserver.HealthCheck {
	return httpserver.HealthCheck{Name: "postgres", Check: pool.Ping}
}
