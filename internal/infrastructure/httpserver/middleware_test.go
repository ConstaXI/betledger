package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/davibanfi/betledger/internal/infrastructure/auth"
	"github.com/davibanfi/betledger/internal/infrastructure/logging"
)

func TestWithCorrelationID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		header   string
		wantKept bool
	}{
		{name: "should accept when the header is safe", header: "req-abc_1.2", wantKept: true},
		{name: "should accept when the header has the maximum length", header: strings.Repeat("a", maxCorrelationIDLength), wantKept: true},
		{name: "should generate an id when the header is missing", header: ""},
		{name: "should generate an id when the header carries a line break", header: "req-1\ninjected"},
		{name: "should generate an id when the header carries spaces", header: "req 1"},
		{name: "should generate an id when the header is too long", header: strings.Repeat("a", maxCorrelationIDLength+1)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var seen string
			handler := withCorrelationID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = logging.CorrelationID(r.Context())
			}))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header[CorrelationHeader] = []string{test.header}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			_, err := uuid.Parse(seen)
			assert.Equal(t, test.wantKept, seen == test.header)
			assert.Equal(t, !test.wantKept, err == nil)
			assert.Equal(t, seen, recorder.Header().Get(CorrelationHeader))
		})
	}
}

func TestWithRequestObservation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		// correlationID is the one the context carries, as the correlation
		// middleware leaves it.
		correlationID string
		// handlerStatus is the status the route writes, or zero when it writes
		// the body without setting one.
		handlerStatus int
		wantStatus    int
		wantRecord    map[string]any
		wantMetrics   []string
	}{
		{
			name:          "should report when a request is handled",
			method:        http.MethodPost,
			path:          "/wallets",
			correlationID: "req-1",
			handlerStatus: http.StatusCreated,
			wantStatus:    http.StatusCreated,
			wantRecord: map[string]any{
				"level": "INFO", "msg": "request handled", "method": http.MethodPost, "path": "/wallets",
				"status": float64(http.StatusCreated), "correlationId": "req-1",
			},
			wantMetrics: []string{`POST "POST /wallets" 201`},
		},
		{
			name:       "should report the route of a protected path when it carries an identifier",
			method:     http.MethodGet,
			path:       "/wallets/0192f291-27dd-7d3f-8071-5f8685deef37",
			wantStatus: http.StatusOK,
			wantRecord: map[string]any{
				"level": "INFO", "msg": "request handled", "method": http.MethodGet,
				"path": "/wallets/0192f291-27dd-7d3f-8071-5f8685deef37", "status": float64(http.StatusOK),
			},
			wantMetrics: []string{`GET "GET /wallets/{walletId}" 200`},
		},
		{
			name:       "should report no route when the path matches none",
			method:     http.MethodGet,
			path:       "/unknown",
			wantStatus: http.StatusNotFound,
			wantRecord: map[string]any{
				"level": "INFO", "msg": "request handled", "method": http.MethodGet,
				"path": "/unknown", "status": float64(http.StatusNotFound),
			},
			wantMetrics: []string{`GET "" 404`},
		},
		{
			name:          "should report nothing when liveness is probed",
			method:        http.MethodGet,
			path:          "/health/live",
			handlerStatus: http.StatusOK,
			wantStatus:    http.StatusOK,
			wantRecord:    map[string]any{},
		},
		{
			name:          "should report nothing when the metrics are scraped",
			method:        http.MethodGet,
			path:          "/metrics",
			handlerStatus: http.StatusOK,
			wantStatus:    http.StatusOK,
			wantRecord:    map[string]any{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			route := func(w http.ResponseWriter, _ *http.Request) {
				if test.handlerStatus > 0 {
					w.WriteHeader(test.handlerStatus)
				}
				_, _ = w.Write([]byte("ok"))
			}
			protected := http.NewServeMux()
			protected.HandleFunc("GET /wallets/{walletId}", route)
			mux := http.NewServeMux()
			mux.HandleFunc("POST /wallets", route)
			mux.HandleFunc("GET /health/live", route)
			mux.HandleFunc("GET /metrics", route)
			mux.Handle("/", requireAuthentication(fakeTokenVerifier{principal: walletOperator}, protected))

			var buffer bytes.Buffer
			metrics := &fakeRequestRecorder{}
			handler := withRequestObservation(logging.New(&buffer), metrics, mux)
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("Authorization", "Bearer token")
			request = request.WithContext(logging.WithCorrelationID(request.Context(), test.correlationID))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			record := map[string]any{}
			_ = json.Unmarshal(buffer.Bytes(), &record)
			delete(record, slog.TimeKey)
			delete(record, "durationMs")
			assert.Equal(t, test.wantRecord, record)
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Equal(t, test.wantMetrics, metrics.calls)
		})
	}
}

// fakeRequestRecorder keeps the requests it was told about, as the method, the
// route that matched and the status.
type fakeRequestRecorder struct{ calls []string }

func (f *fakeRequestRecorder) RequestHandled(
	_ context.Context,
	method, route string,
	status int,
	_ time.Duration,
) {
	f.calls = append(f.calls, fmt.Sprintf("%s %q %d", method, route, status))
}

var errRejectedToken = errors.New("fake: token rejected")

var (
	walletOperator = auth.Principal{Roles: []string{auth.RoleWalletOperator}}
	providerA      = auth.Principal{Roles: []string{auth.RoleGameProvider}, ProviderID: "provider-a"}
)

type fakeTokenVerifier struct {
	principal auth.Principal
	err       error
}

func (f fakeTokenVerifier) Verify(context.Context, string) (auth.Principal, error) {
	return f.principal, f.err
}

type probeRoute struct{ calls *int }

func (p probeRoute) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /probe", func(w http.ResponseWriter, _ *http.Request) {
		*p.calls++
		w.WriteHeader(http.StatusNoContent)
	})
}

type publicProbeRoute struct{}

func (publicProbeRoute) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /public", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func TestRequireAuthentication(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		path              string
		authorization     string
		verifierErr       error
		wantStatus        int
		wantCode          string
		wantChallenge     string
		wantProtectedCall int
	}{
		{
			name:              "should accept when the bearer token is valid",
			path:              "/probe",
			authorization:     "Bearer token",
			wantStatus:        http.StatusNoContent,
			wantProtectedCall: 1,
		},
		{
			name:              "should accept when the scheme is written in lower case",
			path:              "/probe",
			authorization:     "bearer token",
			wantStatus:        http.StatusNoContent,
			wantProtectedCall: 1,
		},
		{
			name:          "should return UNAUTHENTICATED when the header is missing",
			path:          "/probe",
			wantStatus:    http.StatusUnauthorized,
			wantCode:      "UNAUTHENTICATED",
			wantChallenge: `Bearer realm="betledger"`,
		},
		{
			name:          "should return UNAUTHENTICATED when the scheme is not bearer",
			path:          "/probe",
			authorization: "Basic dXNlcjpwYXNz",
			wantStatus:    http.StatusUnauthorized,
			wantCode:      "UNAUTHENTICATED",
			wantChallenge: `Bearer realm="betledger"`,
		},
		{
			name:          "should return UNAUTHENTICATED when the token is empty",
			path:          "/probe",
			authorization: "Bearer ",
			wantStatus:    http.StatusUnauthorized,
			wantCode:      "UNAUTHENTICATED",
			wantChallenge: `Bearer realm="betledger"`,
		},
		{
			name:          "should return UNAUTHENTICATED when the verifier rejects the token",
			path:          "/probe",
			authorization: "Bearer token",
			verifierErr:   errRejectedToken,
			wantStatus:    http.StatusUnauthorized,
			wantCode:      "UNAUTHENTICATED",
			wantChallenge: `Bearer realm="betledger"`,
		},
		{
			name:          "should return UNAUTHENTICATED when an unknown path is requested without a token",
			path:          "/unknown",
			wantStatus:    http.StatusUnauthorized,
			wantCode:      "UNAUTHENTICATED",
			wantChallenge: `Bearer realm="betledger"`,
		},
		{
			name:          "should accept when an unknown path is requested with a valid token",
			path:          "/unknown",
			authorization: "Bearer token",
			wantStatus:    http.StatusNotFound,
		},
		{
			name:        "should accept when a public route is requested without a token",
			path:        "/public",
			verifierErr: errRejectedToken,
			wantStatus:  http.StatusNoContent,
		},
		{
			name:        "should accept when liveness is requested without a token",
			path:        "/health/live",
			verifierErr: errRejectedToken,
			wantStatus:  http.StatusOK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			handler := NewHandler(
				[]Route{publicProbeRoute{}},
				[]Route{probeRoute{calls: &calls}},
				nil,
				fakeTokenVerifier{err: test.verifierErr},
				&fakeRequestRecorder{},
				slog.New(slog.DiscardHandler),
			)
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Header.Set("Authorization", test.authorization)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			var body errorResponse
			_ = json.Unmarshal(recorder.Body.Bytes(), &body)
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Equal(t, test.wantCode, body.Error.Code)
			assert.Equal(t, test.wantChallenge, recorder.Header().Get("WWW-Authenticate"))
			assert.Equal(t, test.wantProtectedCall, calls)
		})
	}
}

func TestRequireRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		principal  auth.Principal
		wantStatus int
		wantCode   string
		wantCalls  int
	}{
		{
			name:       "should accept when the caller holds the role",
			principal:  walletOperator,
			wantStatus: http.StatusNoContent,
			wantCalls:  1,
		},
		{
			name:       "should return FORBIDDEN when the caller holds another role",
			principal:  providerA,
			wantStatus: http.StatusForbidden,
			wantCode:   "FORBIDDEN",
		},
		{
			name:       "should return FORBIDDEN when the caller holds no role",
			wantStatus: http.StatusForbidden,
			wantCode:   "FORBIDDEN",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			handler := requireRole(auth.RoleWalletOperator, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request = request.WithContext(context.WithValue(request.Context(), principalKey{}, test.principal))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			var body errorResponse
			_ = json.Unmarshal(recorder.Body.Bytes(), &body)
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Equal(t, test.wantCode, body.Error.Code)
			assert.Equal(t, test.wantCalls, calls)
		})
	}
}
