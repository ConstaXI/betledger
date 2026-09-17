package httpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/infrastructure/auth"
	"github.com/davibanfi/betledger/internal/usecase"
)

// IdempotencyKeyHeader carries the key that identifies a retry of the same
// operation.
const IdempotencyKeyHeader = "Idempotency-Key"

type wagerProcessor interface {
	Execute(ctx context.Context, input usecase.ProcessWagerInput) (usecase.WagerResult, error)
}

// WageringHandler serves the endpoints that receive operations from game
// providers.
type WageringHandler struct {
	processWager wagerProcessor
	logger       *slog.Logger
}

// NewWageringHandler builds the handler.
func NewWageringHandler(processWager *usecase.ProcessWager, logger *slog.Logger) *WageringHandler {
	return &WageringHandler{processWager: processWager, logger: logger}
}

// Register mounts the wagering endpoints.
func (h *WageringHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /wagering/transactions", requireRole(auth.RoleGameProvider, h.handleProcessWager))
}

type processWagerRequest struct {
	ProviderID                     string       `json:"providerId"`
	ExternalTransactionID          string       `json:"externalTransactionId"`
	PlayerID                       string       `json:"playerId"`
	WalletID                       string       `json:"walletId"`
	RoundID                        string       `json:"roundId"`
	GameID                         string       `json:"gameId"`
	Kind                           string       `json:"kind"`
	Money                          moneyPayload `json:"money"`
	ReferenceExternalTransactionID string       `json:"referenceExternalTransactionId"`
}

type wagerResultResponse struct {
	TransactionID    string       `json:"transactionId"`
	Status           wager.State  `json:"status"`
	Balance          *money.Money `json:"balance,omitempty"`
	FailureCode      string       `json:"failureCode,omitempty"`
	IdempotentReplay bool         `json:"idempotentReplay"`
}

func (h *WageringHandler) handleProcessWager(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get(IdempotencyKeyHeader)
	if idempotencyKey == "" {
		writeError(w, r, h.logger, domain.ValidationError(domain.FailureCodeInvalidInput,
			"the %s header is required", IdempotencyKeyHeader))
		return
	}

	var request processWagerRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	providerID := principalFrom(r.Context()).ProviderID
	if providerID == "" || request.ProviderID != providerID {
		writeForbidden(w, fmt.Sprintf("the token does not authorize operations for provider %q", request.ProviderID))
		return
	}

	playerID, err := domain.ParseID(request.PlayerID)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	walletID, err := domain.ParseID(request.WalletID)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	kind, err := wager.ParseExternalKind(request.Kind)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	amount, err := money.Parse(request.Money.Amount, request.Money.Currency)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	result, err := h.processWager.Execute(r.Context(), usecase.ProcessWagerInput{
		ProviderID:                     providerID,
		ExternalTransactionID:          request.ExternalTransactionID,
		IdempotencyKey:                 idempotencyKey,
		PlayerID:                       playerID,
		WalletID:                       walletID,
		RoundID:                        request.RoundID,
		GameID:                         request.GameID,
		Kind:                           kind,
		Money:                          amount,
		ReferenceExternalTransactionID: request.ReferenceExternalTransactionID,
		CorrelationID:                  correlationID(r.Context()),
	})
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	if err := writeJSON(w, wagerResultStatus(result), newWagerResultResponse(result)); err != nil {
		writeError(w, r, h.logger, err)
	}
}

// wagerResultStatus tells the outcomes apart by status alone: 422 for REJECTED,
// 202 for an operation still waiting for its reference, 201 when the operation
// was just applied and 200 for a replay of an applied one.
func wagerResultStatus(result usecase.WagerResult) int {
	switch {
	case result.State == wager.StateRejected:
		return http.StatusUnprocessableEntity
	case result.State == wager.StatePendingReference:
		return http.StatusAccepted
	case result.IdempotentReplay:
		return http.StatusOK
	default:
		return http.StatusCreated
	}
}

func newWagerResultResponse(result usecase.WagerResult) wagerResultResponse {
	response := wagerResultResponse{
		TransactionID:    result.TransactionID.String(),
		Status:           result.State,
		FailureCode:      string(result.FailureCode),
		IdempotentReplay: result.IdempotentReplay,
	}
	if result.State == wager.StateProcessed {
		response.Balance = &result.Balance
	}
	return response
}
