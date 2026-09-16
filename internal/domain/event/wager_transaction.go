package event

import (
	"time"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
)

// WagerTransactionProcessedData carries a successfully concluded operation. The
// provider metadata is absent for the internal wallet opening.
type WagerTransactionProcessedData struct {
	TransactionID         string      `json:"transactionId"`
	Kind                  wager.Kind  `json:"kind"`
	WalletID              string      `json:"walletId"`
	PlayerID              string      `json:"playerId"`
	Money                 money.Money `json:"money"`
	Balance               money.Money `json:"balance"`
	ProviderID            string      `json:"providerId,omitempty"`
	ExternalTransactionID string      `json:"externalTransactionId,omitempty"`
	RoundID               string      `json:"roundId,omitempty"`
	GameID                string      `json:"gameId,omitempty"`
}

// WagerTransactionRejectedData carries a definitive business refusal.
type WagerTransactionRejectedData struct {
	TransactionID         string             `json:"transactionId"`
	Kind                  wager.Kind         `json:"kind"`
	WalletID              string             `json:"walletId"`
	PlayerID              string             `json:"playerId"`
	Money                 money.Money        `json:"money"`
	FailureCode           domain.FailureCode `json:"failureCode"`
	ProviderID            string             `json:"providerId,omitempty"`
	ExternalTransactionID string             `json:"externalTransactionId,omitempty"`
	RoundID               string             `json:"roundId,omitempty"`
	GameID                string             `json:"gameId,omitempty"`
}

// WagerTransactionPendingReferenceData carries the wait for a reference that has
// not arrived yet.
type WagerTransactionPendingReferenceData struct {
	TransactionID                  string      `json:"transactionId"`
	Kind                           wager.Kind  `json:"kind"`
	WalletID                       string      `json:"walletId"`
	PlayerID                       string      `json:"playerId"`
	Money                          money.Money `json:"money"`
	ReferenceExternalTransactionID string      `json:"referenceExternalTransactionId"`
	ProviderID                     string      `json:"providerId,omitempty"`
	ExternalTransactionID          string      `json:"externalTransactionId,omitempty"`
	RoundID                        string      `json:"roundId,omitempty"`
	GameID                         string      `json:"gameId,omitempty"`
}

// NewWagerTransactionProcessed builds the event for a concluded operation. The
// transaction must be in the PROCESSED state and carry the observed balance.
func NewWagerTransactionProcessed(transaction *wager.Transaction, correlationID string, occurredAt time.Time) (Event, error) {
	if err := requireTransaction(transaction, correlationID, occurredAt); err != nil {
		return Event{}, err
	}
	if transaction.State() != wager.StateProcessed {
		return Event{}, domain.ValidationError(domain.FailureCodeInvalidStateTransition,
			"%s requires state %s, got %s", TypeWagerTransactionProcessed, wager.StateProcessed, transaction.State())
	}
	balance, ok := transaction.ResultBalance()
	if !ok {
		return Event{}, domain.ValidationError(domain.FailureCodeInvalidInput,
			"%s requires the observed balance", TypeWagerTransactionProcessed)
	}

	return newEvent(TypeWagerTransactionProcessed, transaction.WalletID(), correlationID, occurredAt,
		WagerTransactionProcessedData{
			TransactionID:         transaction.ID().String(),
			Kind:                  transaction.Kind(),
			WalletID:              transaction.WalletID().String(),
			PlayerID:              transaction.PlayerID().String(),
			Money:                 transaction.Money(),
			Balance:               balance,
			ProviderID:            transaction.ProviderID(),
			ExternalTransactionID: transaction.ExternalTransactionID(),
			RoundID:               transaction.RoundID(),
			GameID:                transaction.GameID(),
		}), nil
}

// NewWagerTransactionRejected builds the event for a definitive refusal.
func NewWagerTransactionRejected(transaction *wager.Transaction, correlationID string, occurredAt time.Time) (Event, error) {
	if err := requireTransaction(transaction, correlationID, occurredAt); err != nil {
		return Event{}, err
	}
	if transaction.State() != wager.StateRejected {
		return Event{}, domain.ValidationError(domain.FailureCodeInvalidStateTransition,
			"%s requires state %s, got %s", TypeWagerTransactionRejected, wager.StateRejected, transaction.State())
	}
	if transaction.FailureCode() == "" {
		return Event{}, domain.ValidationError(domain.FailureCodeInvalidInput,
			"%s requires a failureCode", TypeWagerTransactionRejected)
	}

	return newEvent(TypeWagerTransactionRejected, transaction.WalletID(), correlationID, occurredAt,
		WagerTransactionRejectedData{
			TransactionID:         transaction.ID().String(),
			Kind:                  transaction.Kind(),
			WalletID:              transaction.WalletID().String(),
			PlayerID:              transaction.PlayerID().String(),
			Money:                 transaction.Money(),
			FailureCode:           transaction.FailureCode(),
			ProviderID:            transaction.ProviderID(),
			ExternalTransactionID: transaction.ExternalTransactionID(),
			RoundID:               transaction.RoundID(),
			GameID:                transaction.GameID(),
		}), nil
}

// NewWagerTransactionPendingReference builds the event recording that the
// operation is waiting for a reference.
func NewWagerTransactionPendingReference(transaction *wager.Transaction, correlationID string, occurredAt time.Time) (Event, error) {
	if err := requireTransaction(transaction, correlationID, occurredAt); err != nil {
		return Event{}, err
	}
	if transaction.State() != wager.StatePendingReference {
		return Event{}, domain.ValidationError(domain.FailureCodeInvalidStateTransition,
			"%s requires state %s, got %s",
			TypeWagerTransactionPendingReference, wager.StatePendingReference, transaction.State())
	}

	return newEvent(TypeWagerTransactionPendingReference, transaction.WalletID(), correlationID, occurredAt,
		WagerTransactionPendingReferenceData{
			TransactionID:                  transaction.ID().String(),
			Kind:                           transaction.Kind(),
			WalletID:                       transaction.WalletID().String(),
			PlayerID:                       transaction.PlayerID().String(),
			Money:                          transaction.Money(),
			ReferenceExternalTransactionID: transaction.ReferenceExternalTransactionID(),
			ProviderID:                     transaction.ProviderID(),
			ExternalTransactionID:          transaction.ExternalTransactionID(),
			RoundID:                        transaction.RoundID(),
			GameID:                         transaction.GameID(),
		}), nil
}

func requireTransaction(transaction *wager.Transaction, correlationID string, occurredAt time.Time) error {
	if transaction == nil {
		return domain.ValidationError(domain.FailureCodeInvalidInput, "transaction is required")
	}
	return requireEnvelope(correlationID, occurredAt)
}
