package httpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/davibanfi/betledger/internal/infrastructure/auth"
	"github.com/davibanfi/betledger/internal/infrastructure/logging"
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
		next.ServeHTTP(w, r.WithContext(logging.WithCorrelationID(r.Context(), id)))
	})
}

// RequestRecorder records how requests ended, for the metrics endpoint.
type RequestRecorder interface {
	RequestHandled(ctx context.Context, method, route string, status int, elapsed time.Duration)
}

// withRequestObservation logs how each request ended, with the correlation
// identifier its context carries, and records its latency. Health endpoints and
// the metrics endpoint are left out: they are probed every few seconds and
// would bury the requests that carry work. The metric is labelled by the route
// that matched, which the router leaves on the request, because the path
// carries identifiers and each one would open a series of its own.
func withRequestObservation(logger *slog.Logger, metrics RequestRecorder, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, healthPathPrefix) || r.URL.Path == metricsPath {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		elapsed := time.Since(started)
		logger.InfoContext(r.Context(), "request handled",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"durationMs", elapsed.Milliseconds(),
		)
		metrics.RequestHandled(r.Context(), r.Method, r.Pattern, recorder.status, elapsed)
	})
}

// statusRecorder keeps the status written to the response, which the request
// log reports and http.ResponseWriter does not expose.
type statusRecorder struct {
	http.ResponseWriter
	// status is the one written by the handler, or 200 when it wrote a body
	// without setting one.
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
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

// TokenVerifier validates the bearer token of a request and identifies the
// caller.
type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (auth.Principal, error)
}

type principalKey struct{}

// requireAuthentication answers 401 unless the request carries a bearer token
// accepted by the verifier, and makes the caller available to the handlers.
// Carrying the caller takes a copy of the request, on which the router of the
// protected routes leaves the pattern it matched; the copy is short lived, so
// the pattern is handed back for the observation of the request, which runs
// above this middleware and would otherwise see only the catch-all route.
func requireAuthentication(verifier TokenVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
		if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
			writeUnauthenticated(w, "a bearer token is required")
			return
		}
		principal, err := verifier.Verify(r.Context(), token)
		if err != nil {
			writeUnauthenticated(w, "the bearer token is invalid or expired")
			return
		}
		authenticated := r.WithContext(context.WithValue(r.Context(), principalKey{}, principal))
		next.ServeHTTP(w, authenticated)
		r.Pattern = authenticated.Pattern
	})
}

// requireRole answers 403 unless the authenticated caller was granted the role.
func requireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !principalFrom(r.Context()).HasRole(role) {
			writeForbidden(w, fmt.Sprintf("the %s role is required", role))
			return
		}
		next(w, r)
	}
}

// principalFrom returns the authenticated caller, or the zero Principal, which
// holds no role, when the request was not authenticated.
func principalFrom(ctx context.Context) auth.Principal {
	principal, _ := ctx.Value(principalKey{}).(auth.Principal)
	return principal
}

func writeForbidden(w http.ResponseWriter, message string) {
	_ = writeJSON(w, http.StatusForbidden, errorResponse{Error: errorDetail{
		Code:    codeForbidden,
		Message: message,
	}})
}

func writeUnauthenticated(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="betledger"`)
	_ = writeJSON(w, http.StatusUnauthorized, errorResponse{Error: errorDetail{
		Code:    codeUnauthenticated,
		Message: message,
	}})
}
