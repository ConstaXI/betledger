package httpserver

import (
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
