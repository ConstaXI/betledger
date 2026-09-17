package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithCorrelationIDKeepsValidHeader(t *testing.T) {
	t.Parallel()

	var seen string
	handler := withCorrelationID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = correlationID(r.Context())
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(CorrelationHeader, "req-abc_1.2")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assert.Equal(t, "req-abc_1.2", seen)
	assert.Equal(t, "req-abc_1.2", recorder.Header().Get(CorrelationHeader))
}

func TestWithCorrelationIDReplacesUnsafeHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
	}{
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
				seen = correlationID(r.Context())
			}))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header[CorrelationHeader] = []string{test.header}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			_, err := uuid.Parse(seen)
			require.NoError(t, err)
			assert.Equal(t, seen, recorder.Header().Get(CorrelationHeader))
		})
	}
}

var errRejectedToken = errors.New("fake: token rejected")

type fakeTokenVerifier struct{ err error }

func (f fakeTokenVerifier) Verify(context.Context, string) error { return f.err }

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
