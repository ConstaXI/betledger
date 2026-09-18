package usecase

import (
	"context"

	"github.com/davibanfi/betledger/internal/domain"
)

// InboundMessage is an operation received from the queue, together with the
// identity the broker gave the message.
type InboundMessage struct {
	MessageID string
	Input     ProcessWagerInput
}

// InboundResult is the outcome of a received message.
type InboundResult struct {
	WagerResult
	// Duplicate tells that the message had already been taken in, so nothing
	// was applied again.
	Duplicate bool
}

// ProcessInboxMessage applies an operation received from the queue, recording
// the message in the inbox within the same commit. A redelivery finds the
// message recorded and applies nothing; the financial idempotency of the
// operation itself still rests on its idempotency key, which is what protects
// the same operation arriving over HTTP and over the queue.
type ProcessInboxMessage struct {
	transactor   Transactor
	inbox        InboxRepository
	processWager *ProcessWager
	metrics      Metrics
}

// NewProcessInboxMessage builds the use case.
func NewProcessInboxMessage(
	transactor Transactor,
	inbox InboxRepository,
	processWager *ProcessWager,
	metrics Metrics,
) *ProcessInboxMessage {
	return &ProcessInboxMessage{
		transactor:   transactor,
		inbox:        inbox,
		processWager: processWager,
		metrics:      metrics,
	}
}

// Execute takes the message in. The same identifier carrying another payload is
// a conflict, which is permanent and must not be retried.
func (uc *ProcessInboxMessage) Execute(ctx context.Context, message InboundMessage) (InboundResult, error) {
	if message.MessageID == "" {
		return InboundResult{}, domain.ValidationError(domain.FailureCodeInvalidInput, "messageId is required")
	}
	payloadHash, err := PayloadHash(message.Input)
	if err != nil {
		return InboundResult{}, err
	}

	var result InboundResult
	err = uc.transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		recorded, found, err := uc.inbox.Find(ctx, message.MessageID)
		if err != nil {
			return err
		}
		if found && recorded.PayloadHash != payloadHash {
			return domain.ConflictError(domain.FailureCodeIdempotencyConflict,
				"message %q was already taken in with a different payload", message.MessageID)
		}
		if found {
			result = InboundResult{
				TransactionID: recorded.TransactionID, IdempotentReplay: true,
				Duplicate: true,
			}
			return nil
		}

		applied, err := uc.processWager.Execute(ctx, message.Input)
		if err != nil {
			return err
		}
		result = InboundResult{WagerResult: applied}
		return uc.inbox.Record(ctx, InboxMessage{
			MessageID:     message.MessageID,
			PayloadHash:   payloadHash,
			TransactionID: applied.TransactionID,
		})
	})
	if err != nil {
		return InboundResult{}, err
	}

	uc.metrics.MessageTaken(ctx, result.Duplicate)
	return result, nil
}
