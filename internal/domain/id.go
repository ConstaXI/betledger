package domain

import "github.com/google/uuid"

// ID is the internal entity identifier. UUIDv7 gives stable time ordering,
// which the ledger cursor pagination relies on.
type ID = uuid.UUID

// NilID is the uninitialized identifier.
var NilID = uuid.Nil

// NewID generates a UUIDv7. An entropy failure is an environment error, not a
// business rejection, hence the panic.
func NewID() ID {
	return uuid.Must(uuid.NewV7())
}

// ParseID validates an identifier received from outside the domain.
func ParseID(value string) (ID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return NilID, ValidationError(FailureCodeInvalidInput, "identifier %q is invalid", value)
	}
	if id == NilID {
		return NilID, ValidationError(FailureCodeInvalidInput, "identifier cannot be nil")
	}
	return id, nil
}

// IsNilID reports whether the identifier is uninitialized.
func IsNilID(id ID) bool { return id == NilID }

func requireID(id ID, field string) error {
	if IsNilID(id) {
		return ValidationError(FailureCodeInvalidInput, "%s is required", field)
	}
	return nil
}
