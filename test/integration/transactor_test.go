//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres"
)

func TestTransactorWithinTransaction(t *testing.T) {
	t.Parallel()

	transactor := postgres.NewTransactor(database.Pool)
	repository := postgres.NewWalletRepository()

	tests := []struct {
		name          string
		write         func(ctx context.Context, w *wallet.Wallet) error
		wantFailed    bool
		wantPersisted bool
	}{
		{
			name: "should accept when the unit of work succeeds, committing the write",
			write: func(ctx context.Context, w *wallet.Wallet) error {
				return transactor.WithinTransaction(ctx, func(ctx context.Context) error {
					return repository.Create(ctx, w)
				})
			},
			wantPersisted: true,
		},
		{
			name: "should roll back when the unit of work fails after writing",
			write: func(ctx context.Context, w *wallet.Wallet) error {
				return transactor.WithinTransaction(ctx, func(ctx context.Context) error {
					return errors.Join(repository.Create(ctx, w), errors.New("abort"))
				})
			},
			wantFailed: true,
		},
		{
			name: "should refuse when a repository writes outside a transaction",
			write: func(ctx context.Context, w *wallet.Wallet) error {
				return repository.Create(ctx, w)
			},
			wantFailed: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w, err := wallet.Open(domain.NewID(), domain.NewID(), money.MustNew(0, money.MustCurrency("BRL")), time.Now())
			require.NoError(t, err)

			err = test.write(context.Background(), w)

			assert.Equal(t, test.wantFailed, err != nil)
			assert.Equal(t, test.wantPersisted, database.WalletExists(t, w.ID()))
		})
	}
}
