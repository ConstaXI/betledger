package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/api"
	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/usecase"
)

func TestResponsesMatchTheOpenAPIContract(t *testing.T) {
	t.Parallel()

	spec, err := openapi3.NewLoader().LoadFromData(api.OpenAPI)
	require.NoError(t, err)
	require.NoError(t, spec.Validate(context.Background()))
	router, err := gorillamux.NewRouter(spec)
	require.NoError(t, err)

	opened, err := wallet.Open(domain.NewID(), domain.NewID(), money.MustNew(100000, money.MustCurrency("BRL")))
	require.NoError(t, err)

	const validBody = `{"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","initialBalance":{"amount":"1000.00","currency":"BRL"}}`
	healthy := HealthCheck{Name: "postgres", Check: func(context.Context) error { return nil }}
	failing := HealthCheck{Name: "postgres", Check: func(context.Context) error { return errors.New("down") }}

	processed := usecase.WagerResult{
		TransactionID: domain.NewID(),
		State:         wager.StateProcessed,
		Balance:       money.MustNew(97500, money.MustCurrency("BRL")),
	}
	replayed := processed
	replayed.IdempotentReplay = true
	rejected := usecase.WagerResult{
		TransactionID: domain.NewID(),
		State:         wager.StateRejected,
		FailureCode:   domain.FailureCodeInsufficientFunds,
	}

	tests := []struct {
		name           string
		method         string
		path           string
		body           string
		idempotencyKey string
		wagerResult    usecase.WagerResult
		verifierErr    error
		useCaseErr     error
		checks         []HealthCheck
		wantStatus     int
	}{
		{
			name:       "should match the contract when a wallet is opened",
			method:     http.MethodPost,
			path:       "/wallets",
			body:       validBody,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "should match the contract when the body is malformed",
			method:     http.MethodPost,
			path:       "/wallets",
			body:       `{"playerId":`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "should match the contract when the amount exceeds the scale",
			method:     http.MethodPost,
			path:       "/wallets",
			body:       `{"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","initialBalance":{"amount":"10.123","currency":"BRL"}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "should match the contract when the wallet already exists",
			method:     http.MethodPost,
			path:       "/wallets",
			body:       validBody,
			useCaseErr: domain.ConflictError(domain.FailureCodeWalletAlreadyExists, "wallet already exists"),
			wantStatus: http.StatusConflict,
		},
		{
			name:       "should match the contract when the database is unavailable",
			method:     http.MethodPost,
			path:       "/wallets",
			body:       validBody,
			useCaseErr: usecase.ErrUnavailable,
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "should match the contract when an unexpected error happens",
			method:     http.MethodPost,
			path:       "/wallets",
			body:       validBody,
			useCaseErr: errors.New("boom"),
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:           "should match the contract when an operation is applied",
			method:         http.MethodPost,
			path:           "/wagering/transactions",
			body:           validWagerBody,
			idempotencyKey: "provider-a:transaction-123",
			wagerResult:    processed,
			wantStatus:     http.StatusCreated,
		},
		{
			name:           "should match the contract when an operation is replayed",
			method:         http.MethodPost,
			path:           "/wagering/transactions",
			body:           validWagerBody,
			idempotencyKey: "provider-a:transaction-123",
			wagerResult:    replayed,
			wantStatus:     http.StatusOK,
		},
		{
			name:           "should match the contract when an operation is rejected",
			method:         http.MethodPost,
			path:           "/wagering/transactions",
			body:           validWagerBody,
			idempotencyKey: "provider-a:transaction-123",
			wagerResult:    rejected,
			wantStatus:     http.StatusUnprocessableEntity,
		},
		{
			name:       "should match the contract when the idempotency key is missing",
			method:     http.MethodPost,
			path:       "/wagering/transactions",
			body:       validWagerBody,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:           "should match the contract when the wallet of the operation does not exist",
			method:         http.MethodPost,
			path:           "/wagering/transactions",
			body:           validWagerBody,
			idempotencyKey: "provider-a:transaction-123",
			useCaseErr:     domain.NotFoundError(domain.FailureCodeWalletNotFound, "wallet not found"),
			wantStatus:     http.StatusNotFound,
		},
		{
			name:           "should match the contract when the idempotency key conflicts",
			method:         http.MethodPost,
			path:           "/wagering/transactions",
			body:           validWagerBody,
			idempotencyKey: "provider-a:transaction-123",
			useCaseErr:     domain.ConflictError(domain.FailureCodeIdempotencyConflict, "conflict"),
			wantStatus:     http.StatusConflict,
		},
		{
			name:           "should match the contract when the database is unavailable for an operation",
			method:         http.MethodPost,
			path:           "/wagering/transactions",
			body:           validWagerBody,
			idempotencyKey: "provider-a:transaction-123",
			useCaseErr:     usecase.ErrUnavailable,
			wantStatus:     http.StatusServiceUnavailable,
		},
		{
			name:        "should match the contract when a wallet is opened without a valid token",
			method:      http.MethodPost,
			path:        "/wallets",
			body:        validBody,
			verifierErr: errRejectedToken,
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:           "should match the contract when an operation is sent without a valid token",
			method:         http.MethodPost,
			path:           "/wagering/transactions",
			body:           validWagerBody,
			idempotencyKey: "provider-a:transaction-123",
			verifierErr:    errRejectedToken,
			wantStatus:     http.StatusUnauthorized,
		},
		{
			name:       "should match the contract when the process is alive",
			method:     http.MethodGet,
			path:       "/health/live",
			wantStatus: http.StatusOK,
		},
		{
			name:       "should match the contract when every dependency is ready",
			method:     http.MethodGet,
			path:       "/health/ready",
			checks:     []HealthCheck{healthy},
			wantStatus: http.StatusOK,
		},
		{
			name:       "should match the contract when a dependency is unavailable",
			method:     http.MethodGet,
			path:       "/health/ready",
			checks:     []HealthCheck{failing},
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			opener := &fakeWalletOpener{wallet: opened, err: test.useCaseErr}
			processor := &fakeWagerProcessor{result: test.wagerResult, err: test.useCaseErr}
			handler := NewHandler(
				nil,
				[]Route{
					&WalletHandler{openWallet: opener, logger: slog.New(slog.DiscardHandler)},
					&WageringHandler{processWager: processor, logger: slog.New(slog.DiscardHandler)},
				},
				test.checks,
				fakeTokenVerifier{err: test.verifierErr},
				slog.New(slog.DiscardHandler),
			)
			request := httptest.NewRequest(test.method, "http://localhost:8080"+test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(IdempotencyKeyHeader, test.idempotencyKey)
			request.Header.Set("Authorization", "Bearer token")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			route, pathParams, err := router.FindRoute(request)
			require.NoError(t, err)
			err = openapi3filter.ValidateResponse(context.Background(), &openapi3filter.ResponseValidationInput{
				RequestValidationInput: &openapi3filter.RequestValidationInput{
					Request:    request,
					PathParams: pathParams,
					Route:      route,
				},
				Status:  recorder.Code,
				Header:  recorder.Header(),
				Body:    io.NopCloser(recorder.Body),
				Options: &openapi3filter.Options{IncludeResponseStatus: true},
			})

			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.NoError(t, err)
		})
	}
}

func TestDocsHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		path            string
		wantContentType string
		wantBody        []byte
	}{
		{
			name:            "should serve the specification when the OpenAPI document is requested",
			path:            "/openapi.yaml",
			wantContentType: "application/yaml",
			wantBody:        api.OpenAPI,
		},
		{
			name:            "should serve Swagger UI when the docs page is requested",
			path:            "/docs",
			wantContentType: "text/html; charset=utf-8",
			wantBody:        api.SwaggerUI,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			handler := NewHandler([]Route{NewDocsHandler()}, nil, nil, fakeTokenVerifier{err: errRejectedToken}, slog.New(slog.DiscardHandler))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))

			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, test.wantContentType, recorder.Header().Get("Content-Type"))
			assert.Equal(t, test.wantBody, recorder.Body.Bytes())
		})
	}
}
