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
	"github.com/davibanfi/betledger/internal/infrastructure/httpserver"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres"
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
			fx.Annotate(newPostgresHealthCheck, fx.ResultTags(`group:"readiness"`)),
			fx.Annotate(auth.NewTokenVerifier, fx.As(new(httpserver.TokenVerifier))),
		),
		postgres.Module,
		httpserver.Module,
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
