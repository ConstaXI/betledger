//go:build integration

package integration

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/infrastructure/postgres"
)

func TestMigrationsAreReversible(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, err := database.Pool.Exec(ctx, "CREATE DATABASE migrations_roundtrip")
	require.NoError(t, err)

	db, err := sql.Open("pgx", strings.Replace(database.URL, "/betledger?", "/migrations_roundtrip?", 1))
	require.NoError(t, err)
	defer db.Close()

	provider, err := postgres.NewMigrationProvider(db)
	require.NoError(t, err)

	applied, err := provider.Up(ctx)
	require.NoError(t, err)
	assert.Len(t, applied, len(provider.ListSources()))

	for range applied {
		_, err := provider.Down(ctx)
		require.NoError(t, err)
	}
	_, err = provider.Down(ctx)
	assert.ErrorIs(t, err, goose.ErrNoNextVersion)

	reapplied, err := provider.Up(ctx)
	require.NoError(t, err)
	assert.Len(t, reapplied, len(provider.ListSources()))
}
