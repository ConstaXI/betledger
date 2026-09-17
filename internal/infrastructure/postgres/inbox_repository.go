package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres/sqlcgen"
	"github.com/davibanfi/betledger/internal/usecase"
)

const inboxMessagesPrimaryKey = "inbox_messages_pkey"

var _ usecase.InboxRepository = (*InboxRepository)(nil)

// InboxRepository records the messages the consumer took in.
type InboxRepository struct{}

// NewInboxRepository builds the repository.
func NewInboxRepository() *InboxRepository {
	return &InboxRepository{}
}

// Find returns the message already recorded under the identifier.
func (r *InboxRepository) Find(ctx context.Context, messageID string) (usecase.InboxMessage, bool, error) {
	q, err := queries(ctx)
	if err != nil {
		return usecase.InboxMessage{}, false, err
	}
	row, err := q.SelectInboxMessage(ctx, messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return usecase.InboxMessage{}, false, nil
	}
	if err != nil {
		return usecase.InboxMessage{}, false, translate(err)
	}
	return usecase.InboxMessage{
		MessageID:     row.MessageID,
		PayloadHash:   row.PayloadHash,
		TransactionID: row.TransactionID,
	}, true, nil
}

// Record stores the message within the transaction carried by ctx.
func (r *InboxRepository) Record(ctx context.Context, message usecase.InboxMessage) error {
	q, err := queries(ctx)
	if err != nil {
		return err
	}
	err = q.InsertInboxMessage(ctx, sqlcgen.InsertInboxMessageParams{
		MessageID:     message.MessageID,
		PayloadHash:   message.PayloadHash,
		TransactionID: message.TransactionID,
	})
	if isUniqueViolation(err, inboxMessagesPrimaryKey) {
		return domain.ConflictError(domain.FailureCodeIdempotencyConflict,
			"message %q was already taken in", message.MessageID)
	}
	return translate(err)
}
