package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/davibanfi/betledger/internal/domain"
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

// LeasePending postpones the oldest unpublished event of each aggregate, when
// due, to leaseUntil in a single statement, skipping rows another publisher
// holds. Ordering by the sequence assigned at insert, under the wallet lock,
// follows the commit order of each wallet.
func (r *OutboxRepository) LeasePending(
	ctx context.Context,
	dueAt, leaseUntil time.Time,
	limit int,
) ([]usecase.OutboxRecord, error) {
	q, err := queries(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.LeasePendingOutboxEvents(ctx, sqlcgen.LeasePendingOutboxEventsParams{
		LeaseUntil: leaseUntil,
		DueAt:      dueAt,
		BatchSize:  int32(limit),
	})
	if err != nil {
		return nil, translate(err)
	}
	records := make([]usecase.OutboxRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, usecase.OutboxRecord{
			EventID:     row.ID,
			AggregateID: row.AggregateID,
			EventType:   row.EventType,
			OccurredAt:  row.OccurredAt,
			Payload:     row.Payload,
			Attempts:    int(row.Attempts),
		})
	}
	return records, nil
}

// MarkPublished records the publication. An event already marked, because
// another publisher republished it after a lease ran out, is left as it is.
func (r *OutboxRepository) MarkPublished(ctx context.Context, eventID domain.ID, publishedAt time.Time) error {
	q, err := queries(ctx)
	if err != nil {
		return err
	}
	_, err = q.MarkOutboxEventPublished(ctx, sqlcgen.MarkOutboxEventPublishedParams{
		ID:          eventID,
		PublishedAt: publishedAt,
	})
	return translate(err)
}

// ReschedulePublication records a failed publication, unless the event was
// published meanwhile.
func (r *OutboxRepository) ReschedulePublication(
	ctx context.Context,
	eventID domain.ID,
	attempts int,
	nextAttemptAt time.Time,
) error {
	q, err := queries(ctx)
	if err != nil {
		return err
	}
	_, err = q.RescheduleOutboxEvent(ctx, sqlcgen.RescheduleOutboxEventParams{
		ID:            eventID,
		Attempts:      int32(attempts),
		NextAttemptAt: nextAttemptAt,
	})
	return translate(err)
}
