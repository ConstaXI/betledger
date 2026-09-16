package domain

import "time"

// EventType identifies an integration event contract.
type EventType string

const (
	EventTypeWagerTransactionProcessed        EventType = "WagerTransactionProcessed"
	EventTypeWagerTransactionRejected         EventType = "WagerTransactionRejected"
	EventTypeWagerTransactionPendingReference EventType = "WagerTransactionPendingReference"
	EventTypeWalletBalanceChanged             EventType = "WalletBalanceChanged"
)

func (t EventType) String() string { return string(t) }

// eventVersion is the contract version of every event produced so far. It is set
// by the constructors and bumped per event type when a payload changes.
const eventVersion = 1

// Event is the envelope published to the outside world.
type Event struct {
	// EventID is preserved across republications, so consumers can deduplicate
	// by it.
	EventID ID `json:"eventId"`
	// EventType names the contract of Data and is set by the constructor.
	EventType EventType `json:"eventType"`
	// AggregateID is always the wallet, so consumers can order and group events
	// per wallet; the FIFO queue uses it as message group.
	AggregateID ID `json:"aggregateId"`
	// CorrelationID ties together every event produced by the same request.
	CorrelationID string `json:"correlationId"`
	// CausationID points at the message that caused the event, when there is one.
	CausationID string `json:"causationId,omitempty"`
	// OccurredAt is when the change happened, always in UTC.
	OccurredAt time.Time `json:"occurredAt"`
	// Version is the contract version of EventType, set by the constructor.
	Version int `json:"version"`
	// Data is the typed payload matching EventType.
	Data any `json:"data"`
}

// WagerTransactionProcessedData carries a successfully concluded operation. The
// provider metadata is absent for the internal wallet opening.
type WagerTransactionProcessedData struct {
	TransactionID         string          `json:"transactionId"`
	Kind                  TransactionKind `json:"kind"`
	WalletID              string          `json:"walletId"`
	PlayerID              string          `json:"playerId"`
	Money                 Money           `json:"money"`
	Balance               Money           `json:"balance"`
	ProviderID            string          `json:"providerId,omitempty"`
	ExternalTransactionID string          `json:"externalTransactionId,omitempty"`
	RoundID               string          `json:"roundId,omitempty"`
	GameID                string          `json:"gameId,omitempty"`
}

// WagerTransactionRejectedData carries a definitive business refusal.
type WagerTransactionRejectedData struct {
	TransactionID         string          `json:"transactionId"`
	Kind                  TransactionKind `json:"kind"`
	WalletID              string          `json:"walletId"`
	PlayerID              string          `json:"playerId"`
	Money                 Money           `json:"money"`
	FailureCode           FailureCode     `json:"failureCode"`
	ProviderID            string          `json:"providerId,omitempty"`
	ExternalTransactionID string          `json:"externalTransactionId,omitempty"`
	RoundID               string          `json:"roundId,omitempty"`
	GameID                string          `json:"gameId,omitempty"`
}

// WagerTransactionPendingReferenceData carries the wait for a reference that has
// not arrived yet.
type WagerTransactionPendingReferenceData struct {
	TransactionID                  string          `json:"transactionId"`
	Kind                           TransactionKind `json:"kind"`
	WalletID                       string          `json:"walletId"`
	PlayerID                       string          `json:"playerId"`
	Money                          Money           `json:"money"`
	ReferenceExternalTransactionID string          `json:"referenceExternalTransactionId"`
	ProviderID                     string          `json:"providerId,omitempty"`
	ExternalTransactionID          string          `json:"externalTransactionId,omitempty"`
	RoundID                        string          `json:"roundId,omitempty"`
	GameID                         string          `json:"gameId,omitempty"`
}

// WalletBalanceChangedData carries an effective balance change.
type WalletBalanceChangedData struct {
	WalletID      string          `json:"walletId"`
	TransactionID string          `json:"transactionId"`
	Direction     LedgerDirection `json:"direction"`
	Money         Money           `json:"money"`
	BalanceBefore Money           `json:"balanceBefore"`
	BalanceAfter  Money           `json:"balanceAfter"`
	WalletVersion int64           `json:"walletVersion"`
}

// NewWagerTransactionProcessedEvent builds the event for a concluded operation.
// The transaction must be in the PROCESSED state and carry the observed balance.
func NewWagerTransactionProcessedEvent(transaction *WagerTransaction, correlationID string, occurredAt time.Time) (Event, error) {
	if err := requireEventInput(transaction, correlationID, occurredAt); err != nil {
		return Event{}, err
	}
	if transaction.State() != StateProcessed {
		return Event{}, ValidationError(FailureCodeInvalidStateTransition,
			"%s requires state %s, got %s", EventTypeWagerTransactionProcessed, StateProcessed, transaction.State())
	}
	balance, ok := transaction.ResultBalance()
	if !ok {
		return Event{}, ValidationError(FailureCodeInvalidInput,
			"%s requires the observed balance", EventTypeWagerTransactionProcessed)
	}

	return newEvent(EventTypeWagerTransactionProcessed, transaction.WalletID(), correlationID, occurredAt,
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

// NewWagerTransactionRejectedEvent builds the event for a definitive refusal.
func NewWagerTransactionRejectedEvent(transaction *WagerTransaction, correlationID string, occurredAt time.Time) (Event, error) {
	if err := requireEventInput(transaction, correlationID, occurredAt); err != nil {
		return Event{}, err
	}
	if transaction.State() != StateRejected {
		return Event{}, ValidationError(FailureCodeInvalidStateTransition,
			"%s requires state %s, got %s", EventTypeWagerTransactionRejected, StateRejected, transaction.State())
	}
	if transaction.FailureCode() == "" {
		return Event{}, ValidationError(FailureCodeInvalidInput,
			"%s requires a failureCode", EventTypeWagerTransactionRejected)
	}

	return newEvent(EventTypeWagerTransactionRejected, transaction.WalletID(), correlationID, occurredAt,
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

// NewWagerTransactionPendingReferenceEvent builds the event recording that the
// operation is waiting for a reference.
func NewWagerTransactionPendingReferenceEvent(transaction *WagerTransaction, correlationID string, occurredAt time.Time) (Event, error) {
	if err := requireEventInput(transaction, correlationID, occurredAt); err != nil {
		return Event{}, err
	}
	if transaction.State() != StatePendingReference {
		return Event{}, ValidationError(FailureCodeInvalidStateTransition,
			"%s requires state %s, got %s", EventTypeWagerTransactionPendingReference, StatePendingReference, transaction.State())
	}

	return newEvent(EventTypeWagerTransactionPendingReference, transaction.WalletID(), correlationID, occurredAt,
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

// NewWalletBalanceChangedEvent builds the event for an effective balance change,
// taking the amounts from the ledger entry that recorded it.
func NewWalletBalanceChangedEvent(wallet *Wallet, entry *WalletLedgerEntry, correlationID string, occurredAt time.Time) (Event, error) {
	if wallet == nil {
		return Event{}, ValidationError(FailureCodeInvalidInput, "wallet is required")
	}
	if entry == nil {
		return Event{}, ValidationError(FailureCodeInvalidInput, "ledger entry is required")
	}
	if correlationID == "" {
		return Event{}, ValidationError(FailureCodeInvalidInput, "correlationId is required")
	}
	if occurredAt.IsZero() {
		return Event{}, ValidationError(FailureCodeInvalidInput, "occurredAt is required")
	}
	if entry.WalletID() != wallet.ID() {
		return Event{}, ValidationError(FailureCodeInvalidInput,
			"ledger entry belongs to wallet %s, not %s", entry.WalletID(), wallet.ID())
	}

	return newEvent(EventTypeWalletBalanceChanged, wallet.ID(), correlationID, occurredAt,
		WalletBalanceChangedData{
			WalletID:      wallet.ID().String(),
			TransactionID: entry.TransactionID().String(),
			Direction:     entry.Direction(),
			Money:         entry.Money(),
			BalanceBefore: entry.BalanceBefore(),
			BalanceAfter:  entry.BalanceAfter(),
			WalletVersion: wallet.Version(),
		}), nil
}

func newEvent(eventType EventType, aggregateID ID, correlationID string, occurredAt time.Time, data any) Event {
	return Event{
		EventID:       NewID(),
		EventType:     eventType,
		AggregateID:   aggregateID,
		CorrelationID: correlationID,
		OccurredAt:    occurredAt.UTC(),
		Version:       eventVersion,
		Data:          data,
	}
}

func requireEventInput(transaction *WagerTransaction, correlationID string, occurredAt time.Time) error {
	if transaction == nil {
		return ValidationError(FailureCodeInvalidInput, "transaction is required")
	}
	if correlationID == "" {
		return ValidationError(FailureCodeInvalidInput, "correlationId is required")
	}
	if occurredAt.IsZero() {
		return ValidationError(FailureCodeInvalidInput, "occurredAt is required")
	}
	return nil
}

// WithCausation returns a copy of the event pointing at the message that caused
// it. Events are immutable snapshots, so this never mutates the receiver.
func (e Event) WithCausation(causationID string) Event {
	e.CausationID = causationID
	return e
}
