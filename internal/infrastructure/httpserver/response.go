package httpserver

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/usecase"
)

const (
	maxRequestBodyBytes = 1 << 20

	codeUnauthenticated    = "UNAUTHENTICATED"
	codeForbidden          = "FORBIDDEN"
	codeServiceUnavailable = "SERVICE_UNAVAILABLE"
	codeInternalError      = "INTERNAL_ERROR"
)

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// decodeJSON reads a single JSON object, rejecting unknown fields, trailing
// content and bodies above the size limit.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return domain.ValidationError(domain.FailureCodeInvalidInput, "malformed request body: %v", err)
	}
	if decoder.More() {
		return domain.ValidationError(domain.FailureCodeInvalidInput, "request body must hold a single JSON object")
	}
	return nil
}

// writeJSON encodes the body before touching the response, so an encoding
// failure can still be answered with an error status.
func writeJSON(w http.ResponseWriter, status int, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
	return nil
}

// writeError answers with the status and stable code that match the error.
// Server-side failures are logged, and their details never reach the client.
func writeError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	status, detail := classifyError(err)
	if status >= http.StatusInternalServerError {
		logger.ErrorContext(r.Context(), "request failed",
			"error", err,
			"correlationId", correlationID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
		)
	}
	_ = writeJSON(w, status, errorResponse{Error: detail})
}

func classifyError(err error) (int, errorDetail) {
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		return domainErrorStatus(err), errorDetail{Code: string(domainErr.Code()), Message: domainErr.Message()}
	}
	if errors.Is(err, usecase.ErrUnavailable) {
		return http.StatusServiceUnavailable, errorDetail{
			Code:    codeServiceUnavailable,
			Message: "service temporarily unavailable, retry later",
		}
	}
	return http.StatusInternalServerError, errorDetail{Code: codeInternalError, Message: "internal error"}
}

func domainErrorStatus(err error) int {
	switch {
	case errors.Is(err, domain.ErrValidation):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrRejected):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}
