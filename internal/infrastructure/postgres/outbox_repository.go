package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/davibanfi/betledger/internal/domain/event"
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
func (r *OutboxRepository) Append(ctx context.Context, events ...event.Event) error {
	q, err := queries(ctx)
	if err != nil {
		return err
	}

	for _, e := range events {
		payload, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("encode event %s: %w", e.ID, err)
		}
		err = q.InsertOutboxEvent(ctx, sqlcgen.InsertOutboxEventParams{
			ID:          e.ID,
			AggregateID: e.AggregateID,
			EventType:   e.Type.String(),
			Payload:     payload,
			OccurredAt:  e.OccurredAt,
		})
		if err != nil {
			return translate(err)
		}
	}
	return nil
}
