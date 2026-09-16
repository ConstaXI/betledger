package httpserver

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/usecase"
)

type walletOpener interface {
	Execute(ctx context.Context, input usecase.OpenWalletInput) (*domain.Wallet, error)
}

// WalletHandler serves the wallet endpoints.
type WalletHandler struct {
	openWallet walletOpener
	logger     *slog.Logger
}

// NewWalletHandler builds the handler.
func NewWalletHandler(openWallet *usecase.OpenWallet, logger *slog.Logger) *WalletHandler {
	return &WalletHandler{openWallet: openWallet, logger: logger}
}

// Register mounts the wallet endpoints.
func (h *WalletHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /wallets", h.handleOpenWallet)
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
	ID       string       `json:"id"`
	PlayerID string       `json:"playerId"`
	Balance  domain.Money `json:"balance"`
	Version  int64        `json:"version"`
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
	initialBalance, err := domain.ParseMoney(request.InitialBalance.Amount, request.InitialBalance.Currency)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	wallet, err := h.openWallet.Execute(r.Context(), usecase.OpenWalletInput{
		PlayerID:       playerID,
		InitialBalance: initialBalance,
		CorrelationID:  correlationID(r.Context()),
	})
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	w.Header().Set("Location", "/wallets/"+wallet.ID().String())
	err = writeJSON(w, http.StatusCreated, walletResponse{
		ID:       wallet.ID().String(),
		PlayerID: wallet.PlayerID().String(),
		Balance:  wallet.Balance(),
		Version:  wallet.Version(),
	})
	if err != nil {
		writeError(w, r, h.logger, err)
	}
}
