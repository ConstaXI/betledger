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
	"github.com/davibanfi/betledger/internal/domain/domaintest"
	"github.com/davibanfi/betledger/internal/domain/ledger"
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

	opened, err := wallet.Open(domain.NewID(), domain.NewID(), money.MustNew(100000, money.MustCurrency("BRL")), domaintest.FixedNow)
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
				&fakeRequestRecorder{},
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

type fakeWalletReader struct {
	wallet *wallet.Wallet
	page   usecase.LedgerPage
	result usecase.ReconciliationResult
	err    error
	cursor *usecase.LedgerCursor
	limit  int
}

func (f *fakeWalletReader) Wallet(context.Context, domain.ID) (*wallet.Wallet, error) {
	return f.wallet, f.err
}

func (f *fakeWalletReader) Ledger(
	_ context.Context,
	_ domain.ID,
	cursor *usecase.LedgerCursor,
	limit int,
) (usecase.LedgerPage, error) {
	f.cursor = cursor
	f.limit = limit
	return f.page, f.err
}

func (f *fakeWalletReader) Reconcile(context.Context, domain.ID) (usecase.ReconciliationResult, error) {
	return f.result, f.err
}

func TestWalletReadHandlers(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	opened, err := wallet.Open(domain.NewID(), domain.NewID(), money.MustNew(100000, brl), domaintest.FixedNow)
	require.NoError(t, err)
	entry, err := opened.OpeningLedgerEntry(domain.NewID(), domaintest.FixedNow)
	require.NoError(t, err)
	cursor := usecase.LedgerCursor{CreatedAt: entry.CreatedAt(), EntryID: entry.ID()}
	page := usecase.LedgerPage{
		Entries:    []*ledger.Entry{entry},
		NextCursor: &cursor,
	}
	reconciled := usecase.ReconciliationResult{
		WalletID:       opened.ID(),
		Stored:         money.MustNew(100000, brl),
		Calculated:     money.MustNew(100000, brl),
		Difference:     money.MustNew(0, brl),
		Consistent:     true,
		CheckedEntries: 1,
	}

	tests := []struct {
		name       string
		method     string
		path       string
		readerErr  error
		wantStatus int
		wantCode   string
		wantCursor *usecase.LedgerCursor
		wantLimit  int
	}{
		{
			name:       "should return 200 when the wallet is read",
			method:     http.MethodGet,
			path:       "/wallets/" + opened.ID().String(),
			wantStatus: http.StatusOK,
		},
		{
			name:       "should return 404 WALLET_NOT_FOUND when the wallet does not exist",
			method:     http.MethodGet,
			path:       "/wallets/" + opened.ID().String(),
			readerErr:  domain.NotFoundError(domain.FailureCodeWalletNotFound, "wallet not found"),
			wantStatus: http.StatusNotFound,
			wantCode:   "WALLET_NOT_FOUND",
		},
		{
			name:       "should return 400 INVALID_INPUT when the wallet id is not a UUID",
			method:     http.MethodGet,
			path:       "/wallets/wallet-1",
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:       "should return 200 when the first page of the ledger is read",
			method:     http.MethodGet,
			path:       "/wallets/" + opened.ID().String() + "/ledger",
			wantStatus: http.StatusOK,
		},
		{
			name:       "should accept when the page carries a cursor and a limit",
			method:     http.MethodGet,
			path:       "/wallets/" + opened.ID().String() + "/ledger?limit=10&cursor=" + encodeLedgerCursor(cursor),
			wantStatus: http.StatusOK,
			wantCursor: &cursor,
			wantLimit:  10,
		},
		{
			name:       "should return 400 INVALID_INPUT when the cursor is not opaque data of ours",
			method:     http.MethodGet,
			path:       "/wallets/" + opened.ID().String() + "/ledger?cursor=not-a-cursor",
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:       "should return 400 INVALID_INPUT when the limit is not a number",
			method:     http.MethodGet,
			path:       "/wallets/" + opened.ID().String() + "/ledger?limit=all",
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_INPUT",
		},
		{
			name:       "should return 200 when the wallet is reconciled",
			method:     http.MethodPost,
			path:       "/wallets/" + opened.ID().String() + "/reconciliation",
			wantStatus: http.StatusOK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			reader := &fakeWalletReader{wallet: opened, page: page, result: reconciled, err: test.readerErr}
			handler := NewHandler(
				nil,
				[]Route{&WalletHandler{readWallet: reader, logger: slog.New(slog.DiscardHandler)}},
				nil,
				fakeTokenVerifier{principal: walletOperator},
				&fakeRequestRecorder{},
				slog.New(slog.DiscardHandler),
			)
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("Authorization", "Bearer token")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			assert.Equal(t, test.wantStatus, recorder.Code)
			assert.Equal(t, test.wantCode, body.Error.Code)
			assert.Equal(t, test.wantCursor, reader.cursor)
			assert.Equal(t, test.wantLimit, reader.limit)
		})
	}
}
