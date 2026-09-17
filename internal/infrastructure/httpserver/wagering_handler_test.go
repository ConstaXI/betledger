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

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/usecase"
)

type fakeWagerProcessor struct {
	result usecase.WagerResult
	err    error
	calls  int
	input  usecase.ProcessWagerInput
}

func (f *fakeWagerProcessor) Execute(_ context.Context, input usecase.ProcessWagerInput) (usecase.WagerResult, error) {
	f.calls++
	f.input = input
	return f.result, f.err
}

type wagerResponseBody struct {
	TransactionID    string        `json:"transactionId"`
	Status           string        `json:"status"`
	Balance          *moneyPayload `json:"balance"`
	FailureCode      string        `json:"failureCode"`
	IdempotentReplay bool          `json:"idempotentReplay"`
	Error            struct {
		Code string `json:"code"`
	} `json:"error"`
}

const validWagerBody = `{"providerId":"provider-a","externalTransactionId":"transaction-123",` +
	`"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","walletId":"0192f291-27dd-7d3f-8071-5f8685deef37",` +
	`"roundId":"round-987","gameId":"fortune-chimp","kind":"BET","money":{"amount":"25.00","currency":"BRL"}}`

func TestProcessWagerHandler(t *testing.T) {
	t.Parallel()

	transactionID := domain.NewID()
	brl := money.MustCurrency("BRL")
	processed := usecase.WagerResult{
		TransactionID: transactionID,
		State:         wager.StateProcessed,
		Balance:       money.MustNew(97500, brl),
	}
	replayed := processed
	replayed.IdempotentReplay = true
	rejected := usecase.WagerResult{
		TransactionID: transactionID,
		State:         wager.StateRejected,
		FailureCode:   domain.FailureCodeInsufficientFunds,
	}

	tests := []struct {
		name            string
		idempotencyKey  string
		body            string
		result          usecase.WagerResult
		processorErr    error
		wantStatus      int
		wantCode        string
		wantState       string
		wantBalance     *moneyPayload
		wantFailureCode string
		wantReplay      bool
		wantCalls       int
	}{
		{
			name:           "should return 201 when the operation is applied",
			idempotencyKey: "provider-a:transaction-123",
			body:           validWagerBody,
			result:         processed,
			wantStatus:     http.StatusCreated,
			wantState:      "PROCESSED",
			wantBalance:    &moneyPayload{Amount: "975.00", Currency: "BRL"},
			wantCalls:      1,
		},
		{
			name:           "should return 200 when the operation is a replay",
			idempotencyKey: "provider-a:transaction-123",
			body:           validWagerBody,
			result:         replayed,
			wantStatus:     http.StatusOK,
			wantState:      "PROCESSED",
			wantBalance:    &moneyPayload{Amount: "975.00", Currency: "BRL"},
			wantReplay:     true,
			wantCalls:      1,
		},
		{
			name:            "should return 422 INSUFFICIENT_FUNDS when the operation is rejected",
			idempotencyKey:  "provider-a:transaction-123",
			body:            validWagerBody,
			result:          rejected,
			wantStatus:      http.StatusUnprocessableEntity,
			wantState:       "REJECTED",
			wantFailureCode: "INSUFFICIENT_FUNDS",
			wantCalls:       1,
		},
		{
			name:       "should return 400 INVALID_INPUT when the idempotency key is missing",
			body:       validWagerBody,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:           "should return 400 INVALID_INPUT when the body is malformed",
			idempotencyKey: "provider-a:transaction-123",
			body:           `{"providerId":`,
			wantStatus:     http.StatusBadRequest,
			wantCode:       "INVALID_INPUT",
		},
		{
			name:           "should return 400 INVALID_INPUT when the wallet id is not a UUID",
			idempotencyKey: "provider-a:transaction-123",
			body:           strings.Replace(validWagerBody, "0192f291-27dd-7d3f-8071-5f8685deef37", "wallet-1", 1),
			wantStatus:     http.StatusBadRequest,
			wantCode:       "INVALID_INPUT",
		},
		{
			name:           "should return 400 INVALID_INPUT when the kind is unknown",
			idempotencyKey: "provider-a:transaction-123",
			body:           strings.Replace(validWagerBody, `"BET"`, `"JACKPOT"`, 1),
			wantStatus:     http.StatusBadRequest,
			wantCode:       "INVALID_INPUT",
		},
		{
			name:           "should return 400 TRANSACTION_KIND_NOT_ALLOWED when the kind is OPENING",
			idempotencyKey: "provider-a:transaction-123",
			body:           strings.Replace(validWagerBody, `"BET"`, `"OPENING"`, 1),
			wantStatus:     http.StatusBadRequest,
			wantCode:       "TRANSACTION_KIND_NOT_ALLOWED",
		},
		{
			name:           "should return 400 INVALID_INPUT when the amount is a JSON number",
			idempotencyKey: "provider-a:transaction-123",
			body:           strings.Replace(validWagerBody, `"25.00"`, `25.00`, 1),
			wantStatus:     http.StatusBadRequest,
			wantCode:       "INVALID_INPUT",
		},
		{
			name:           "should return 404 WALLET_NOT_FOUND when the wallet does not exist",
			idempotencyKey: "provider-a:transaction-123",
			body:           validWagerBody,
			processorErr:   domain.NotFoundError(domain.FailureCodeWalletNotFound, "wallet not found"),
			wantStatus:     http.StatusNotFound,
			wantCode:       "WALLET_NOT_FOUND",
			wantCalls:      1,
		},
		{
			name:           "should return 409 IDEMPOTENCY_CONFLICT when the key was used with another body",
			idempotencyKey: "provider-a:transaction-123",
			body:           validWagerBody,
			processorErr:   domain.ConflictError(domain.FailureCodeIdempotencyConflict, "conflict"),
			wantStatus:     http.StatusConflict,
			wantCode:       "IDEMPOTENCY_CONFLICT",
			wantCalls:      1,
		},
		{
			name:           "should return 503 SERVICE_UNAVAILABLE when the database is unavailable",
			idempotencyKey: "provider-a:transaction-123",
			body:           validWagerBody,
			processorErr:   usecase.ErrUnavailable,
			wantStatus:     http.StatusServiceUnavailable,
			wantCode:       "SERVICE_UNAVAILABLE",
			wantCalls:      1,
		},
		{
			name:           "should return 500 INTERNAL_ERROR when an unexpected error happens",
			idempotencyKey: "provider-a:transaction-123",
			body:           validWagerBody,
			processorErr:   errors.New("boom"),
			wantStatus:     http.StatusInternalServerError,
			wantCode:       "INTERNAL_ERROR",
			wantCalls:      1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			processor := &fakeWagerProcessor{result: test.result, err: test.processorErr}
			handler := NewHandler(
				nil,
				[]Route{&WageringHandler{processWager: processor, logger: slog.New(slog.DiscardHandler)}},
				nil,
				fakeTokenVerifier{},
				slog.New(slog.DiscardHandler),
			)
			request := httptest.NewRequest(http.MethodPost, "/wagering/transactions", strings.NewReader(test.body))
			request.Header.Set(IdempotencyKeyHeader, test.idempotencyKey)
			request.Header.Set("Authorization", "Bearer token")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			var body wagerResponseBody
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Equal(t, test.wantCode, body.Error.Code)
			assert.Equal(t, test.wantState, body.Status)
			assert.Equal(t, test.wantBalance, body.Balance)
			assert.Equal(t, test.wantFailureCode, body.FailureCode)
			assert.Equal(t, test.wantReplay, body.IdempotentReplay)
			assert.Equal(t, test.wantCalls, processor.calls)
		})
	}
}

func TestProcessWagerHandlerPassesTheRequestToTheUseCase(t *testing.T) {
	t.Parallel()

	processor := &fakeWagerProcessor{result: usecase.WagerResult{
		TransactionID: domain.NewID(),
		State:         wager.StateProcessed,
		Balance:       money.MustNew(10000, money.MustCurrency("BRL")),
	}}
	handler := NewHandler(
		nil,
		[]Route{&WageringHandler{processWager: processor, logger: slog.New(slog.DiscardHandler)}},
		nil,
		fakeTokenVerifier{},
		slog.New(slog.DiscardHandler),
	)
	body := strings.Replace(validWagerBody, `"kind":"BET","money":{"amount":"25.00"`,
		`"kind":"WIN","money":{"amount":"25.5"`, 1)
	request := httptest.NewRequest(http.MethodPost, "/wagering/transactions", strings.NewReader(body))
	request.Header.Set(IdempotencyKeyHeader, "custom-key")
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set(CorrelationHeader, "req-42")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Equal(t, usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "transaction-123",
		IdempotencyKey:        "custom-key",
		PlayerID:              uuid.MustParse("0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1"),
		WalletID:              uuid.MustParse("0192f291-27dd-7d3f-8071-5f8685deef37"),
		RoundID:               "round-987",
		GameID:                "fortune-chimp",
		Kind:                  wager.KindWin,
		Money:                 money.MustNew(2550, money.MustCurrency("BRL")),
		CorrelationID:         "req-42",
	}, processor.input)
}
