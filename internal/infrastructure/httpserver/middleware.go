package httpserver

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CorrelationHeader carries the correlation identifier of a request.
const CorrelationHeader = "X-Correlation-Id"

const (
	maxCorrelationIDLength = 128
	// requestTimeout stays below the server WriteTimeout, so that a request
	// waiting on an unresponsive dependency is answered with 503 before the
	// connection is cut.
	requestTimeout = 5 * time.Second
)

// withRequestTimeout bounds the work of each request. When a dependency stops
// answering, the expired context aborts the pending call and the request is
// answered as a transient unavailability instead of hanging.
func withRequestTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type correlationKey struct{}

// withCorrelationID propagates the caller's correlation identifier, or generates
// one. Values outside a safe character set are replaced, so that a client
// cannot inject content into logs and events.
func withCorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(CorrelationHeader)
		if !isValidCorrelationID(id) {
			id = uuid.NewString()
		}
		w.Header().Set(CorrelationHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), correlationKey{}, id)))
	})
}

func correlationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return id
}

func isValidCorrelationID(id string) bool {
	if id == "" || len(id) > maxCorrelationIDLength {
		return false
	}
	for _, char := range id {
		isAlphanumeric := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
		if !isAlphanumeric && char != '-' && char != '_' && char != '.' {
			return false
		}
	}
	return true
}

// TokenVerifier validates the bearer token of a request.
type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) error
}

// requireAuthentication answers 401 unless the request carries a bearer token
// accepted by the verifier.
func requireAuthentication(verifier TokenVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
		if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
			writeUnauthenticated(w, "a bearer token is required")
			return
		}
		if err := verifier.Verify(r.Context(), token); err != nil {
			writeUnauthenticated(w, "the bearer token is invalid or expired")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeUnauthenticated(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="betledger"`)
	_ = writeJSON(w, http.StatusUnauthorized, errorResponse{Error: errorDetail{
		Code:    codeUnauthenticated,
		Message: message,
	}})
}
