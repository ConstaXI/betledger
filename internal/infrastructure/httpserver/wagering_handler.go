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
	"github.com/davibanfi/betledger/internal/infrastructure/logging"
	"github.com/davibanfi/betledger/internal/usecase"
)

// IdempotencyKeyHeader carries the key that identifies a retry of the same
// operation.
const IdempotencyKeyHeader = "Idempotency-Key"

type wagerProcessor interface {
	Execute(ctx context.Context, input usecase.ProcessWagerInput) (usecase.WagerResult, error)
}

type transactionReader interface {
	ByID(ctx context.Context, providerID string, transactionID domain.ID) (*wager.Transaction, error)
	ByExternalID(ctx context.Context, providerID, externalTransactionID string) (*wager.Transaction, error)
}

// WageringHandler serves the endpoints that receive operations from game
// providers.
type WageringHandler struct {
	processWager    wagerProcessor
	readTransaction transactionReader
	logger          *slog.Logger
}

// NewWageringHandler builds the handler.
func NewWageringHandler(
	processWager *usecase.ProcessWager,
	readTransaction *usecase.ReadTransaction,
	logger *slog.Logger,
) *WageringHandler {
	return &WageringHandler{processWager: processWager, readTransaction: readTransaction, logger: logger}
}

// Register mounts the wagering endpoints.
func (h *WageringHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /wagering/transactions", requireRole(auth.RoleGameProvider, h.handleProcessWager))
	mux.HandleFunc("GET /wagering/transactions/{transactionId}",
		requireRole(auth.RoleGameProvider, h.handleGetTransaction))
	mux.HandleFunc("GET /providers/{providerId}/wagering/transactions/{externalTransactionId}",
		requireRole(auth.RoleGameProvider, h.handleGetTransactionByExternalID))
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
		CorrelationID:                  logging.CorrelationID(r.Context()),
	})
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	h.logger.InfoContext(r.Context(), "operation concluded",
		"providerId", providerID,
		"externalTransactionId", request.ExternalTransactionID,
		"transactionId", result.TransactionID.String(),
		"walletId", walletID.String(),
		"kind", kind,
		"status", result.State,
		"failureCode", result.FailureCode,
		"idempotentReplay", result.IdempotentReplay,
	)

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

type transactionResponse struct {
	TransactionID                  string       `json:"transactionId"`
	ProviderID                     string       `json:"providerId"`
	ExternalTransactionID          string       `json:"externalTransactionId"`
	Kind                           wager.Kind   `json:"kind"`
	Status                         wager.State  `json:"status"`
	WalletID                       string       `json:"walletId"`
	PlayerID                       string       `json:"playerId"`
	RoundID                        string       `json:"roundId"`
	GameID                         string       `json:"gameId"`
	Money                          money.Money  `json:"money"`
	Balance                        *money.Money `json:"balance,omitempty"`
	FailureCode                    string       `json:"failureCode,omitempty"`
	ReferenceExternalTransactionID string       `json:"referenceExternalTransactionId,omitempty"`
	ReferenceAttempts              int          `json:"referenceAttempts"`
}

func (h *WageringHandler) handleGetTransaction(w http.ResponseWriter, r *http.Request) {
	transactionID, err := domain.ParseID(r.PathValue("transactionId"))
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}

	found, err := h.readTransaction.ByID(r.Context(), principalFrom(r.Context()).ProviderID, transactionID)
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	if err := writeJSON(w, http.StatusOK, newTransactionResponse(found)); err != nil {
		writeError(w, r, h.logger, err)
	}
}

func (h *WageringHandler) handleGetTransactionByExternalID(w http.ResponseWriter, r *http.Request) {
	providerID := principalFrom(r.Context()).ProviderID
	if providerID == "" || r.PathValue("providerId") != providerID {
		writeForbidden(w, fmt.Sprintf("the token does not authorize reading operations of provider %q",
			r.PathValue("providerId")))
		return
	}

	found, err := h.readTransaction.ByExternalID(r.Context(), providerID, r.PathValue("externalTransactionId"))
	if err != nil {
		writeError(w, r, h.logger, err)
		return
	}
	if err := writeJSON(w, http.StatusOK, newTransactionResponse(found)); err != nil {
		writeError(w, r, h.logger, err)
	}
}

func newTransactionResponse(transaction *wager.Transaction) transactionResponse {
	response := transactionResponse{
		TransactionID:                  transaction.ID().String(),
		ProviderID:                     transaction.ProviderID(),
		ExternalTransactionID:          transaction.ExternalTransactionID(),
		Kind:                           transaction.Kind(),
		Status:                         transaction.State(),
		WalletID:                       transaction.WalletID().String(),
		PlayerID:                       transaction.PlayerID().String(),
		RoundID:                        transaction.RoundID(),
		GameID:                         transaction.GameID(),
		Money:                          transaction.Money(),
		FailureCode:                    string(transaction.FailureCode()),
		ReferenceExternalTransactionID: transaction.ReferenceExternalTransactionID(),
		ReferenceAttempts:              transaction.ReferenceAttempts(),
	}
	if balance, ok := transaction.ResultBalance(); ok {
		response.Balance = &balance
	}
	return response
}
