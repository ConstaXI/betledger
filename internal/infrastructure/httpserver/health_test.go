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

func TestHealthHandler(t *testing.T) {
	t.Parallel()

	healthy := HealthCheck{Name: "postgres", Check: func(context.Context) error { return nil }}
	failing := HealthCheck{Name: "sqs", Check: func(context.Context) error { return errors.New("down") }}

	tests := []struct {
		name        string
		path        string
		checks      []HealthCheck
		wantStatus  int
		wantFailing []string
	}{
		{
			name:       "should report alive when liveness is requested",
			path:       "/health/live",
			checks:     []HealthCheck{failing},
			wantStatus: http.StatusOK,
		},
		{
			name:       "should report ready when every dependency answers",
			path:       "/health/ready",
			checks:     []HealthCheck{healthy},
			wantStatus: http.StatusOK,
		},
		{
			name:        "should report unavailable when a dependency fails",
			path:        "/health/ready",
			checks:      []HealthCheck{healthy, failing},
			wantStatus:  http.StatusServiceUnavailable,
			wantFailing: []string{"sqs"},
		},
		{
			name:       "should return 404 when a business route is requested",
			path:       "/wallets",
			checks:     []HealthCheck{healthy},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			handler := NewHealthHandler(test.checks, slog.New(slog.DiscardHandler))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))

			var body healthResponse
			_ = json.Unmarshal(recorder.Body.Bytes(), &body)
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Equal(t, test.wantFailing, body.Failing)
		})
	}
}
