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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/usecase"
)

type fakeWalletOpener struct {
	wallet *wallet.Wallet
	err    error
	calls  int
	input  usecase.OpenWalletInput
}

func (f *fakeWalletOpener) Execute(_ context.Context, input usecase.OpenWalletInput) (*wallet.Wallet, error) {
	f.calls++
	f.input = input
	return f.wallet, f.err
}

type openWalletResponseBody struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
	Balance struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	} `json:"balance"`
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

func TestOpenWalletHandler(t *testing.T) {
	t.Parallel()

	const validBody = `{"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","initialBalance":{"amount":"1000.00","currency":"BRL"}}`

	opened, err := wallet.Open(domain.NewID(), domain.NewID(), money.MustNew(100000, money.MustCurrency("BRL")))
	require.NoError(t, err)

	tests := []struct {
		name        string
		body        string
		openerErr   error
		wantStatus  int
		wantCode    string
		wantAmount  string
		wantVersion int64
		wantCalls   int
	}{
		{
			name:        "should return 201 when the wallet is opened",
			body:        validBody,
			wantStatus:  http.StatusCreated,
			wantAmount:  "1000.00",
			wantVersion: 1,
			wantCalls:   1,
		},
		{
			name:       "should return 400 INVALID_INPUT when the body is malformed",
			body:       `{"playerId":`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:       "should return 400 INVALID_INPUT when the body has an unknown field",
			body:       `{"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","initialBalance":{"amount":"1.00","currency":"BRL"},"admin":true}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:       "should return 400 INVALID_INPUT when the body holds more than one object",
			body:       validBody + validBody,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:       "should return 400 INVALID_INPUT when the player id is not a UUID",
			body:       `{"playerId":"player-1","initialBalance":{"amount":"1000.00","currency":"BRL"}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:       "should return 400 INVALID_AMOUNT when the initial balance is negative",
			body:       `{"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","initialBalance":{"amount":"-1.00","currency":"BRL"}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_AMOUNT",
		},
		{
			name:       "should return 400 INVALID_INPUT when the amount is a JSON number",
			body:       `{"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","initialBalance":{"amount":1000.00,"currency":"BRL"}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:       "should return 409 WALLET_ALREADY_EXISTS when the player already has the wallet",
			body:       validBody,
			openerErr:  domain.ConflictError(domain.FailureCodeWalletAlreadyExists, "wallet already exists"),
			wantStatus: http.StatusConflict,
			wantCode:   "WALLET_ALREADY_EXISTS",
			wantCalls:  1,
		},
		{
			name:       "should return 503 SERVICE_UNAVAILABLE when the database is unavailable",
			body:       validBody,
			openerErr:  usecase.ErrUnavailable,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "SERVICE_UNAVAILABLE",
			wantCalls:  1,
		},
		{
			name:       "should return 500 INTERNAL_ERROR when an unexpected error happens",
			body:       validBody,
			openerErr:  errors.New("boom"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL_ERROR",
			wantCalls:  1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			opener := &fakeWalletOpener{wallet: opened, err: test.openerErr}
			handler := NewHandler(
				nil,
				[]Route{&WalletHandler{openWallet: opener, logger: slog.New(slog.DiscardHandler)}},
				nil,
				fakeTokenVerifier{},
				slog.New(slog.DiscardHandler),
			)
			request := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer token")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			var body openWalletResponseBody
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Equal(t, test.wantCode, body.Error.Code)
			assert.Equal(t, test.wantAmount, body.Balance.Amount)
			assert.Equal(t, test.wantVersion, body.Version)
			assert.Equal(t, test.wantCalls, opener.calls)
			assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
		})
	}
}

func TestOpenWalletHandlerPassesCorrelationIDToTheUseCase(t *testing.T) {
	t.Parallel()

	opened, err := wallet.Open(domain.NewID(), domain.NewID(), money.MustNew(0, money.MustCurrency("BRL")))
	require.NoError(t, err)
	opener := &fakeWalletOpener{wallet: opened}
	handler := NewHandler(
		nil,
		[]Route{&WalletHandler{openWallet: opener, logger: slog.New(slog.DiscardHandler)}},
		nil,
		fakeTokenVerifier{},
		slog.New(slog.DiscardHandler),
	)
	request := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(
		`{"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","initialBalance":{"amount":"0.00","currency":"BRL"}}`))
	request.Header.Set(CorrelationHeader, "req-42")
	request.Header.Set("Authorization", "Bearer token")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Equal(t, "req-42", opener.input.CorrelationID)
	assert.Equal(t, "/wallets/"+opened.ID().String(), recorder.Header().Get("Location"))
}
