// Package domain holds what every part of the betledger business model shares:
// identifiers and classifiable errors. The model itself lives in subpackages,
// independent of Fx, HTTP, SQS and persistence libraries.
package domain

import (
	"errors"
	"fmt"
)

// FailureCode identifies, in a stable way, the reason for a rejection or
// failure. These codes are part of the external contract.
//
// A FailureCode is itself an error, so it can be matched directly:
// errors.Is(err, FailureCodeInsufficientFunds).
type FailureCode string

func (c FailureCode) Error() string { return string(c) }

const (
	FailureCodeInvalidInput     FailureCode = "INVALID_INPUT"
	FailureCodeInvalidAmount    FailureCode = "INVALID_AMOUNT"
	FailureCodeAmountOverflow   FailureCode = "AMOUNT_OVERFLOW"
	FailureCodeCurrencyMismatch FailureCode = "CURRENCY_MISMATCH"

	FailureCodeInsufficientFunds            FailureCode = "INSUFFICIENT_FUNDS"
	FailureCodeInsufficientFundsForReversal FailureCode = "INSUFFICIENT_FUNDS_FOR_REVERSAL"

	FailureCodeReferenceNotFound        FailureCode = "REFERENCE_NOT_FOUND"
	FailureCodeReferenceNotProcessed    FailureCode = "REFERENCE_NOT_PROCESSED"
	FailureCodeReferenceMismatch        FailureCode = "REFERENCE_MISMATCH"
	FailureCodeReferenceAmountMismatch  FailureCode = "REFERENCE_AMOUNT_MISMATCH"
	FailureCodeReferenceAlreadyReversed FailureCode = "REFERENCE_ALREADY_REVERSED"

	FailureCodeInvalidStateTransition FailureCode = "INVALID_STATE_TRANSITION"
	FailureCodeKindNotAllowed         FailureCode = "TRANSACTION_KIND_NOT_ALLOWED"
	FailureCodeIdempotencyConflict    FailureCode = "IDEMPOTENCY_CONFLICT"
	FailureCodeWalletAlreadyExists    FailureCode = "WALLET_ALREADY_EXISTS"
	FailureCodeWalletNotFound         FailureCode = "WALLET_NOT_FOUND"
	FailureCodeWalletPlayerMismatch   FailureCode = "WALLET_PLAYER_MISMATCH"
	FailureCodeTransactionNotFound    FailureCode = "TRANSACTION_NOT_FOUND"
)

// Domain error classes. Use errors.Is to classify an operation result and
// errors.As with *Error to recover the FailureCode.
var (
	ErrValidation = errors.New("domain: validation")
	ErrRejected   = errors.New("domain: business rejection")
	ErrConflict   = errors.New("domain: conflict")
	ErrNotFound   = errors.New("domain: not found")
)

// ErrInsufficientFunds lets the application layer recognize a shortfall and
// pick the FailureCode that matches the operation kind.
var ErrInsufficientFunds = errors.New("domain: insufficient funds")

// Error is the domain error. It carries a class, for errors.Is, and a stable
// FailureCode for the external contract.
type Error struct {
	// class is the sentinel matched by errors.Is, such as ErrRejected.
	class error
	// cause is an optional sentinel exposed through Unwrap, such as
	// ErrInsufficientFunds.
	cause error
	// code is the stable FailureCode exposed in the external contract.
	code FailureCode
	// message is the human-readable description, without the code prefix.
	message string
}

func newError(class, cause error, code FailureCode, format string, args ...any) *Error {
	return &Error{
		class:   class,
		cause:   cause,
		code:    code,
		message: fmt.Sprintf(format, args...),
	}
}

// ValidationError builds a rejection caused by invalid input.
func ValidationError(code FailureCode, format string, args ...any) *Error {
	return newError(ErrValidation, nil, code, format, args...)
}

// RejectionError builds a definitive refusal by a business rule.
func RejectionError(code FailureCode, format string, args ...any) *Error {
	return newError(ErrRejected, nil, code, format, args...)
}

// RejectionErrorWithCause builds a refusal preserving a sentinel error as cause.
func RejectionErrorWithCause(cause error, code FailureCode, format string, args ...any) *Error {
	return newError(ErrRejected, cause, code, format, args...)
}

// ConflictError builds an error for a clash with persisted state.
func ConflictError(code FailureCode, format string, args ...any) *Error {
	return newError(ErrConflict, nil, code, format, args...)
}

// NotFoundError builds an error for a missing record.
func NotFoundError(code FailureCode, format string, args ...any) *Error {
	return newError(ErrNotFound, nil, code, format, args...)
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.code, e.message)
}

// Code returns the stable FailureCode of the error.
func (e *Error) Code() FailureCode { return e.code }

// Message returns the human-readable description, without the code prefix.
func (e *Error) Message() string { return e.message }

// Is matches the error against its class, enabling errors.Is(err, ErrRejected),
// or against its FailureCode.
func (e *Error) Is(target error) bool {
	if code, ok := target.(FailureCode); ok {
		return code == e.code
	}
	return target == e.class
}

// Unwrap exposes the cause, enabling errors.Is with sentinels such as
// ErrInsufficientFunds.
func (e *Error) Unwrap() error { return e.cause }

// CodeOf extracts the FailureCode from a domain error, if there is one.
func CodeOf(err error) (FailureCode, bool) {
	if domainErr, ok := errors.AsType[*Error](err); ok {
		return domainErr.Code(), true
	}
	return "", false
}
