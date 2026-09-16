// Package event implements the integration events published after a change is
// committed.
package event

import (
	"time"

	"github.com/davibanfi/betledger/internal/domain"
)

// Type identifies an integration event contract.
type Type string

const (
	TypeWagerTransactionProcessed        Type = "WagerTransactionProcessed"
	TypeWagerTransactionRejected         Type = "WagerTransactionRejected"
	TypeWagerTransactionPendingReference Type = "WagerTransactionPendingReference"
	TypeWalletBalanceChanged             Type = "WalletBalanceChanged"
)

func (t Type) String() string { return string(t) }

// contractVersion is the version of every event produced so far. It is set by
// the constructors and bumped per event type when a payload changes.
const contractVersion = 1

// Event is the envelope published to the outside world.
type Event struct {
	// ID is preserved across republications, so consumers can deduplicate by it.
	ID domain.ID `json:"eventId"`
	// Type names the contract of Data and is set by the constructor.
	Type Type `json:"eventType"`
	// AggregateID is always the wallet, so consumers can order and group events
	// per wallet; the FIFO queue uses it as message group.
	AggregateID domain.ID `json:"aggregateId"`
	// CorrelationID ties together every event produced by the same request.
	CorrelationID string `json:"correlationId"`
	// CausationID points at the message that caused the event, when there is one.
	CausationID string `json:"causationId,omitempty"`
	// OccurredAt is when the change happened, always in UTC.
	OccurredAt time.Time `json:"occurredAt"`
	// Version is the contract version of Type, set by the constructor.
	Version int `json:"version"`
	// Data is the typed payload matching Type.
	Data any `json:"data"`
}

// WithCausation returns a copy of the event pointing at the message that caused
// it. Events are immutable snapshots, so this never mutates the receiver.
func (e Event) WithCausation(causationID string) Event {
	e.CausationID = causationID
	return e
}

func newEvent(eventType Type, aggregateID domain.ID, correlationID string, occurredAt time.Time, data any) Event {
	return Event{
		ID:            domain.NewID(),
		Type:          eventType,
		AggregateID:   aggregateID,
		CorrelationID: correlationID,
		OccurredAt:    occurredAt.UTC(),
		Version:       contractVersion,
		Data:          data,
	}
}

func requireEnvelope(correlationID string, occurredAt time.Time) error {
	if correlationID == "" {
		return domain.ValidationError(domain.FailureCodeInvalidInput, "correlationId is required")
	}
	if occurredAt.IsZero() {
		return domain.ValidationError(domain.FailureCodeInvalidInput, "occurredAt is required")
	}
	return nil
}
