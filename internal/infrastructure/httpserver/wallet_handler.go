package httpserver

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/infrastructure/auth"
	"github.com/davibanfi/betledger/internal/usecase"
)

type walletOpener interface {
	Execute(ctx context.Context, input usecase.OpenWalletInput) (*wallet.Wallet, error)
}

type walletReader interface {
	Wallet(ctx context.Context, walletID domain.ID) (*wallet.Wallet, error)
	Ledger(ctx context.Context, walletID domain.ID, cursor *usecase.LedgerCursor, limit int) (usecase.LedgerPage, error)
	Reconcile(ctx context.Context, walletID domain.ID) (usecase.ReconciliationResult, error)
}

// WalletHandler serves the wallet endpoints.
type WalletHandler struct {
	openWallet walletOpener
	readWallet walletReader
	logger     *slog.Logger
}

// NewWalletHandler builds the handler.
func NewWalletHandler(openWallet *usecase.OpenWallet, readWallet *usecase.ReadWallet, logger *slog.Logger) *WalletHandler {
	return &WalletHandler{openWallet: openWallet, readWallet: readWallet, logger: logger}
}

// Register mounts the wallet endpoints.
func (h *WalletHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /wallets", requireRole(auth.RoleWalletOperator, h.handleOpenWallet))
	mux.HandleFunc("GET /wallets/{walletId}", requireRole(auth.RoleWalletOperator, h.handleGetWallet))
	mux.HandleFunc("GET /wallets/{walletId}/ledger", requireRole(auth.RoleWalletOperator, h.handleGetLedger))
	mux.HandleFunc("POST /wallets/{walletId}/reconciliation",
		requireRole(auth.RoleWalletOperator, h.handleReconcile))
}

type moneyPayload struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type openWalletRequest struct {
	PlayerID       string       `json:"playerId"`
	InitialBalance moneyPayload `json:"initialBalance"`
}

type walletResponse struct {
	ID       string      `json:"id"`
	PlayerID string      `json:"playerId"`
	Balance  money.Money `json:"balance"`
	Version  int64       `json:"version"`
}

func (h *WalletHandler) handleOpenWallet(w http.ResponseWriter, r *http.Request) {
	var request openWalletRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	playerID, err := domain.ParseID(request.PlayerID)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	initialBalance, err := money.Parse(request.InitialBalance.Amount, request.InitialBalance.Currency)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	opened, err := h.openWallet.Execute(r.Context(), usecase.OpenWalletInput{
		PlayerID:       playerID,
		InitialBalance: initialBalance,
		CorrelationID:  correlationID(r.Context()),
	})
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	w.Header().Set("Location", "/wallets/"+opened.ID().String())
	err = writeJSON(w, http.StatusCreated, newWalletResponse(opened))
	if err != nil {
		writeError(w, r, h.logger, err)
	}
}

type ledgerEntryResponse struct {
	ID            string           `json:"id"`
	TransactionID string           `json:"transactionId"`
	Direction     ledger.Direction `json:"direction"`
	Money         money.Money      `json:"money"`
	BalanceBefore money.Money      `json:"balanceBefore"`
	BalanceAfter  money.Money      `json:"balanceAfter"`
	RecordedAt    time.Time        `json:"recordedAt"`
}

type ledgerPageResponse struct {
	Entries []ledgerEntryResponse `json:"entries"`
	// NextCursor is absent on the last page.
	NextCursor string `json:"nextCursor,omitempty"`
}

type reconciliationResponse struct {
	WalletID          string      `json:"walletId"`
	StoredBalance     money.Money `json:"storedBalance"`
	CalculatedBalance money.Money `json:"calculatedBalance"`
	Difference        money.Money `json:"difference"`
	Consistent        bool        `json:"consistent"`
	CheckedEntries    int         `json:"checkedEntries"`
}

func (h *WalletHandler) handleGetWallet(w http.ResponseWriter, r *http.Request) {
	walletID, err := domain.ParseID(r.PathValue("walletId"))
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	found, err := h.readWallet.Wallet(r.Context(), walletID)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	if err := writeJSON(w, http.StatusOK, newWalletResponse(found)); err != nil {
		writeError(w, r, h.logger, err)
	}
}

func (h *WalletHandler) handleGetLedger(w http.ResponseWriter, r *http.Request) {
	walletID, err := domain.ParseID(r.PathValue("walletId"))
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	cursor, err := decodeLedgerCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	limit, err := queryLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	page, err := h.readWallet.Ledger(r.Context(), walletID, cursor, limit)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	response := ledgerPageResponse{Entries: make([]ledgerEntryResponse, 0, len(page.Entries))}
	for _, entry := range page.Entries {
		response.Entries = append(response.Entries, ledgerEntryResponse{
			ID:            entry.Entry.ID().String(),
			TransactionID: entry.Entry.TransactionID().String(),
			Direction:     entry.Entry.Direction(),
			Money:         entry.Entry.Money(),
			BalanceBefore: entry.Entry.BalanceBefore(),
			BalanceAfter:  entry.Entry.BalanceAfter(),
			RecordedAt:    entry.RecordedAt.UTC(),
		})
	}
	if page.NextCursor != nil {
		response.NextCursor = encodeLedgerCursor(*page.NextCursor)
	}
	if err := writeJSON(w, http.StatusOK, response); err != nil {
		writeError(w, r, h.logger, err)
	}
}

func (h *WalletHandler) handleReconcile(w http.ResponseWriter, r *http.Request) {
	walletID, err := domain.ParseID(r.PathValue("walletId"))
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	result, err := h.readWallet.Reconcile(r.Context(), walletID)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	if !result.Consistent {
		h.logger.ErrorContext(r.Context(), "wallet balance diverges from its ledger",
			"walletId", result.WalletID, "difference", result.Difference.String(),
			"checkedEntries", result.CheckedEntries)
	}

	err = writeJSON(w, http.StatusOK, reconciliationResponse{
		WalletID:          result.WalletID.String(),
		StoredBalance:     result.Stored,
		CalculatedBalance: result.Calculated,
		Difference:        result.Difference,
		Consistent:        result.Consistent,
		CheckedEntries:    result.CheckedEntries,
	})
	if err != nil {
		writeError(w, r, h.logger, err)
	}
}

func newWalletResponse(w *wallet.Wallet) walletResponse {
	return walletResponse{
		ID:       w.ID().String(),
		PlayerID: w.PlayerID().String(),
		Balance:  w.Balance(),
		Version:  w.Version(),
	}
}

// encodeLedgerCursor hides the ordering keys behind an opaque string, so that
// clients pass it back without depending on how pages are ordered.
func encodeLedgerCursor(cursor usecase.LedgerCursor) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(cursor.RecordedAt.UTC().Format(time.RFC3339Nano) + "|" + cursor.EntryID.String()))
}

func decodeLedgerCursor(encoded string) (*usecase.LedgerCursor, error) {
	if encoded == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput, "cursor %q is invalid", encoded)
	}
	recordedAt, entryID, found := strings.Cut(string(decoded), "|")
	if !found {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput, "cursor %q is invalid", encoded)
	}
	at, err := time.Parse(time.RFC3339Nano, recordedAt)
	if err != nil {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput, "cursor %q is invalid", encoded)
	}
	id, err := domain.ParseID(entryID)
	if err != nil {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput, "cursor %q is invalid", encoded)
	}
	return &usecase.LedgerCursor{RecordedAt: at, EntryID: id}, nil
}

func queryLimit(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, domain.ValidationError(domain.FailureCodeInvalidInput, "limit %q is not a number", raw)
	}
	return limit, nil
}
