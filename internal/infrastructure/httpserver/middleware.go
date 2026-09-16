package httpserver

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// CorrelationHeader carries the correlation identifier of a request.
const CorrelationHeader = "X-Correlation-Id"

const maxCorrelationIDLength = 128

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
