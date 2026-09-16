package postgres

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// NewMigrationProvider builds a goose provider over the embedded migrations. It
// takes a PostgreSQL advisory lock while migrating, so concurrent runs from
// several instances apply each migration once.
func NewMigrationProvider(db *sql.DB) (*goose.Provider, error) {
	migrations, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("create migration lock: %w", err)
	}
	return goose.NewProvider(goose.DialectPostgres, db, migrations, goose.WithSessionLocker(locker))
}
