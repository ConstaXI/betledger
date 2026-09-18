package domain

import "time"

// RequireTime rejects an uninitialized instant, naming the field in the error.
// The domain never reads the clock: every instant it records arrives as an
// argument, so an entity is as deterministic as the caller that builds it.
func RequireTime(at time.Time, field string) error {
	if at.IsZero() {
		return ValidationError(FailureCodeInvalidInput, "%s is required", field)
	}
	return nil
}
