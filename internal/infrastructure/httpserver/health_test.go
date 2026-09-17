package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadinessHandler(t *testing.T) {
	t.Parallel()

	healthy := HealthCheck{Name: "postgres", Check: func(context.Context) error { return nil }}
	failing := HealthCheck{Name: "postgres", Check: func(context.Context) error { return errors.New("connection refused") }}

	tests := []struct {
		name       string
		checks     []HealthCheck
		wantStatus int
		wantBody   healthResponse
	}{
		{
			name:       "should return 200 when every dependency is healthy",
			checks:     []HealthCheck{healthy},
			wantStatus: http.StatusOK,
			wantBody:   healthResponse{Status: "ready"},
		},
		{
			name:       "should return 503 naming the dependency when a check fails",
			checks:     []HealthCheck{failing},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   healthResponse{Status: "unavailable", Failing: []string{"postgres"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			handler := NewHandler(nil, nil, test.checks, fakeTokenVerifier{err: errRejectedToken}, slog.New(slog.DiscardHandler))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

			var body healthResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Equal(t, test.wantBody, body)
			assert.NotContains(t, recorder.Body.String(), "connection refused")
		})
	}
}
