package postgres

import (
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

// translate marks errors where the database could not be reached as
// usecase.ErrUnavailable. Errors answered by the server are returned unchanged,
// because they are outcomes rather than outages.
func translate(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return err
	}
	var connectErr *pgconn.ConnectError
	if pgconn.Timeout(err) || pgconn.SafeToRetry(err) || errors.As(err, &connectErr) {
		return fmt.Errorf("%w: %w", usecase.ErrUnavailable, err)
	}
	return err
}
