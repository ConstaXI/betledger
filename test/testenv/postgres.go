//go:build integration

// Package testenv provides the real infrastructure used by the integration
// tests: disposable containers, the running application and helpers to reach
// them. It is imported only from tests.
package testenv

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/davibanfi/betledger/internal/infrastructure/postgres"
)

// Postgres is a disposable PostgreSQL container with every migration applied.
type Postgres struct {
	// URL connects to the migrated database.
	URL string
	// Pool is shared by the tests that query the database directly.
	Pool      *pgxpool.Pool
	container *tcpostgres.PostgresContainer
}

// StartPostgres starts a container and applies the migrations. The caller must
// call Terminate when done.
func StartPostgres(ctx context.Context) (*Postgres, error) {
	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("betledger"),
		tcpostgres.WithUsername("betledger"),
		tcpostgres.WithPassword("betledger"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, err
	}
	database := &Postgres{container: container}

	database.URL, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return database, err
	}
	if err := migrate(ctx, database.URL); err != nil {
		return database, err
	}
	database.Pool, err = pgxpool.New(ctx, database.URL)
	return database, err
}

// MustStartPostgres starts a container owned by the test, terminated on cleanup.
func MustStartPostgres(t *testing.T) *Postgres {
	t.Helper()

	database, err := StartPostgres(context.Background())
	t.Cleanup(database.Terminate)
	require.NoError(t, err)
	return database
}

// Terminate closes the pool and removes the container.
func (p *Postgres) Terminate() {
	if p == nil {
		return
	}
	if p.Pool != nil {
		p.Pool.Close()
	}
	_ = testcontainers.TerminateContainer(p.container)
}

// Pause freezes the database process, so that it stops answering without
// closing connections.
func (p *Postgres) Pause(t *testing.T) {
	t.Helper()

	docker := dockerClient(t)
	_, err := docker.ContainerPause(context.Background(), p.container.GetContainerID(), client.ContainerPauseOptions{})
	require.NoError(t, err)
}

// Unpause resumes a paused database.
func (p *Postgres) Unpause(t *testing.T) {
	t.Helper()

	docker := dockerClient(t)
	_, err := docker.ContainerUnpause(context.Background(), p.container.GetContainerID(), client.ContainerUnpauseOptions{})
	require.NoError(t, err)
}

// URLWithApplicationName tags the connections opened with the returned URL, so
// that they can be counted in pg_stat_activity.
func (p *Postgres) URLWithApplicationName(applicationName string) string {
	return p.URL + "&application_name=" + applicationName
}

func migrate(ctx context.Context, databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	provider, err := postgres.NewMigrationProvider(db)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

func dockerClient(t *testing.T) *testcontainers.DockerClient {
	t.Helper()

	docker, err := testcontainers.NewDockerClientWithOpts(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = docker.Close() })
	return docker
}
