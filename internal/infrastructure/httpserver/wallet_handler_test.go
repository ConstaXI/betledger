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
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/infrastructure/auth"
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

	validInput := usecase.OpenWalletInput{
		PlayerID:       uuid.MustParse("0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1"),
		InitialBalance: money.MustNew(100000, money.MustCurrency("BRL")),
		CorrelationID:  "req-42",
	}

	tests := []struct {
		name         string
		principal    auth.Principal
		body         string
		openerErr    error
		wantStatus   int
		wantCode     string
		wantAmount   string
		wantVersion  int64
		wantLocation string
		wantCalls    int
		wantInput    usecase.OpenWalletInput
	}{
		{
			name:       "should return 403 FORBIDDEN when the caller is a game provider",
			principal:  providerA,
			body:       validBody,
			wantStatus: http.StatusForbidden,
			wantCode:   "FORBIDDEN",
		},
		{
			name:         "should return 201 when the wallet is opened",
			principal:    walletOperator,
			body:         validBody,
			wantStatus:   http.StatusCreated,
			wantAmount:   "1000.00",
			wantVersion:  1,
			wantLocation: "/wallets/" + opened.ID().String(),
			wantCalls:    1,
			wantInput:    validInput,
		},
		{
			name:       "should return 400 INVALID_INPUT when the body is malformed",
			principal:  walletOperator,
			body:       `{"playerId":`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:       "should return 400 INVALID_INPUT when the body has an unknown field",
			principal:  walletOperator,
			body:       `{"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","initialBalance":{"amount":"1.00","currency":"BRL"},"admin":true}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:       "should return 400 INVALID_INPUT when the body holds more than one object",
			principal:  walletOperator,
			body:       validBody + validBody,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:       "should return 400 INVALID_INPUT when the player id is not a UUID",
			principal:  walletOperator,
			body:       `{"playerId":"player-1","initialBalance":{"amount":"1000.00","currency":"BRL"}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:       "should return 400 INVALID_AMOUNT when the initial balance is negative",
			principal:  walletOperator,
			body:       `{"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","initialBalance":{"amount":"-1.00","currency":"BRL"}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_AMOUNT",
		},
		{
			name:       "should return 400 INVALID_INPUT when the amount is a JSON number",
			principal:  walletOperator,
			body:       `{"playerId":"0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1","initialBalance":{"amount":1000.00,"currency":"BRL"}}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:       "should return 409 WALLET_ALREADY_EXISTS when the player already has the wallet",
			principal:  walletOperator,
			body:       validBody,
			openerErr:  domain.ConflictError(domain.FailureCodeWalletAlreadyExists, "wallet already exists"),
			wantStatus: http.StatusConflict,
			wantCode:   "WALLET_ALREADY_EXISTS",
			wantCalls:  1,
			wantInput:  validInput,
		},
		{
			name:       "should return 503 SERVICE_UNAVAILABLE when the database is unavailable",
			principal:  walletOperator,
			body:       validBody,
			openerErr:  usecase.ErrUnavailable,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "SERVICE_UNAVAILABLE",
			wantCalls:  1,
			wantInput:  validInput,
		},
		{
			name:       "should return 500 INTERNAL_ERROR when an unexpected error happens",
			principal:  walletOperator,
			body:       validBody,
			openerErr:  errors.New("boom"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL_ERROR",
			wantCalls:  1,
			wantInput:  validInput,
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
				fakeTokenVerifier{principal: test.principal},
				slog.New(slog.DiscardHandler),
			)
			request := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer token")
			request.Header.Set(CorrelationHeader, "req-42")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			var body openWalletResponseBody
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Equal(t, test.wantCode, body.Error.Code)
			assert.Equal(t, test.wantAmount, body.Balance.Amount)
			assert.Equal(t, test.wantVersion, body.Version)
			assert.Equal(t, test.wantLocation, recorder.Header().Get("Location"))
			assert.Equal(t, test.wantCalls, opener.calls)
			assert.Equal(t, test.wantInput, opener.input)
			assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
		})
	}
}
