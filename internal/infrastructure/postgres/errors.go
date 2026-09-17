package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/davibanfi/betledger/internal/usecase"
)

const uniqueViolationCode = "23505"

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == constraint
}

// translate marks errors where the database could not be reached, or did not
// answer before the deadline, as usecase.ErrUnavailable. Errors answered by the
// server are returned unchanged, because they are outcomes rather than outages.
// A deadline is checked on its own because it can expire outside pgconn, such as
// while waiting for a pooled connection, where pgconn.Timeout does not see it.
func translate(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return err
	}
	var connectErr *pgconn.ConnectError
	if pgconn.Timeout(err) || pgconn.SafeToRetry(err) || errors.As(err, &connectErr) ||
		errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", usecase.ErrUnavailable, err)
	}
	return err
}
