// Command migrate applies and reverts the database schema migrations.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/davibanfi/betledger/internal/infrastructure/postgres"
)

const usage = "usage: migrate up|down|status"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New(usage)
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	provider, err := postgres.NewMigrationProvider(db)
	if err != nil {
		return err
	}

	switch args[0] {
	case "up":
		results, err := provider.Up(ctx)
		for _, result := range results {
			fmt.Println(result)
		}
		return err
	case "down":
		result, err := provider.Down(ctx)
		if errors.Is(err, goose.ErrNoNextVersion) {
			fmt.Println("no migration to revert")
			return nil
		}
		if result != nil {
			fmt.Println(result)
		}
		return err
	case "status":
		statuses, err := provider.Status(ctx)
		for _, status := range statuses {
			fmt.Printf("%-8s %s\n", status.State, status.Source.Path)
		}
		return err
	default:
		return errors.New(usage)
	}
}
