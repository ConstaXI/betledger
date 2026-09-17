package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/infrastructure/config"
	"github.com/davibanfi/betledger/internal/usecase"
)

// Module provides the connection pool and the PostgreSQL implementations of the
// use case ports.
var Module = fx.Module("postgres",
	fx.Provide(
		NewPool,
		fx.Annotate(NewTransactor, fx.As(new(usecase.Transactor))),
		fx.Annotate(NewWalletRepository, fx.As(new(usecase.WalletRepository))),
		fx.Annotate(NewTransactionRepository, fx.As(new(usecase.TransactionRepository))),
		fx.Annotate(NewLedgerRepository, fx.As(new(usecase.LedgerRepository))),
		fx.Annotate(NewOutboxRepository, fx.As(new(usecase.OutboxRepository))),
		fx.Annotate(NewInboxRepository, fx.As(new(usecase.InboxRepository))),
	),
)

// NewPool builds the connection pool. The database is pinged on start, so an
// unreachable database stops the application from starting, and the pool is
// closed on stop, after the components that use it.
func NewPool(lc fx.Lifecycle, cfg config.Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := pool.Ping(ctx); err != nil {
				return fmt.Errorf("ping database: %w", err)
			}
			return nil
		},
		OnStop: func(context.Context) error {
			pool.Close()
			return nil
		},
	})
	return pool, nil
}
