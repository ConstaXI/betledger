//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres"
)

func TestTransactorRollsBackWhenTheUnitOfWorkFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	w, err := wallet.Open(domain.NewID(), domain.NewID(), money.MustNew(0, money.MustCurrency("BRL")))
	require.NoError(t, err)
	errAbort := errors.New("abort")

	err = postgres.NewTransactor(database.Pool).WithinTransaction(ctx, func(ctx context.Context) error {
		require.NoError(t, postgres.NewWalletRepository().Create(ctx, w))
		return errAbort
	})

	assert.ErrorIs(t, err, errAbort)
	var count int
	require.NoError(t, database.Pool.QueryRow(ctx, "SELECT count(*) FROM wallets WHERE id = $1", w.ID()).Scan(&count))
	assert.Equal(t, 0, count)
}

func TestRepositoriesRejectWritesOutsideTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	w, err := wallet.Open(domain.NewID(), domain.NewID(), money.MustNew(0, money.MustCurrency("BRL")))
	require.NoError(t, err)

	err = postgres.NewWalletRepository().Create(ctx, w)

	assert.Error(t, err)
	var count int
	require.NoError(t, database.Pool.QueryRow(ctx, "SELECT count(*) FROM wallets WHERE id = $1", w.ID()).Scan(&count))
	assert.Equal(t, 0, count)
}
