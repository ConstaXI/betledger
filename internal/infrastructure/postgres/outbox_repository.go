package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres/sqlcgen"
	"github.com/davibanfi/betledger/internal/usecase"
)

var _ usecase.OutboxRepository = (*OutboxRepository)(nil)

// OutboxRepository records events in the outbox table. The payload is stored as
// the exact JSON snapshot, and the table rejects changes to it.
type OutboxRepository struct{}

// NewOutboxRepository builds the repository.
func NewOutboxRepository() *OutboxRepository {
	return &OutboxRepository{}
}

// Append inserts the events within the transaction carried by ctx.
func (r *OutboxRepository) Append(ctx context.Context, events ...domain.Event) error {
	q, err := queries(ctx)
	if err != nil {
		return err
	}

	for _, event := range events {
		payload, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("encode event %s: %w", event.EventID, err)
		}
		err = q.InsertOutboxEvent(ctx, sqlcgen.InsertOutboxEventParams{
			ID:          event.EventID,
			AggregateID: event.AggregateID,
			EventType:   event.EventType.String(),
			Payload:     payload,
			OccurredAt:  event.OccurredAt,
		})
		if err != nil {
			return translate(err)
		}
	}
	return nil
}
