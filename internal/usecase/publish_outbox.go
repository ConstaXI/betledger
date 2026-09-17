package usecase

import (
	"context"
	"errors"
	"time"
)

// PublicationPolicy bounds how recorded events are published and retried.
type PublicationPolicy struct {
	// BaseDelay is the wait after the first failed publication of an event; it
	// doubles after each further failure.
	BaseDelay time.Duration
	// MaxDelay caps the wait between two publications of the same event.
	MaxDelay time.Duration
	// Lease is how long a publisher holds the events it took before another
	// publisher may take them, which recovers work left by a publisher that
	// died.
	Lease time.Duration
	// BatchSize is how many events an execution takes at most.
	BatchSize int
}

// PublishOutbox publishes the events recorded in the outbox after the commit
// that produced them. Delivery is at least once: an event whose publication
// succeeded but was not marked, because the publisher died in between, is
// published again with the same event id once its lease runs out.
type PublishOutbox struct {
	transactor Transactor
	outbox     OutboxRepository
	publisher  EventPublisher
	clock      Clock
	policy     PublicationPolicy
}

// NewPublishOutbox builds the use case.
func NewPublishOutbox(
	transactor Transactor,
	outbox OutboxRepository,
	publisher EventPublisher,
	clock Clock,
	policy PublicationPolicy,
) *PublishOutbox {
	return &PublishOutbox{
		transactor: transactor,
		outbox:     outbox,
		publisher:  publisher,
		clock:      clock,
		policy:     policy,
	}
}

// Execute takes the due events and publishes each, returning how many it took.
// A failed publication is retried with exponential backoff and keeps the later
// events of its wallet waiting, so a wallet's events never go out of order.
// Events are never discarded: publication is retried until it succeeds.
func (uc *PublishOutbox) Execute(ctx context.Context) (int, error) {
	now := uc.clock()
	var leased []OutboxRecord
	err := uc.transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		leased, err = uc.outbox.LeasePending(ctx, now, now.Add(uc.policy.Lease), uc.policy.BatchSize)
		return err
	})
	if err != nil {
		return 0, err
	}

	errs := make([]error, 0, len(leased))
	for _, record := range leased {
		errs = append(errs, uc.publish(ctx, record))
	}
	return len(leased), errors.Join(errs...)
}

func (uc *PublishOutbox) publish(ctx context.Context, record OutboxRecord) error {
	published := uc.publisher.Publish(ctx, record)
	return errors.Join(published, uc.transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		if published != nil {
			attempts := record.Attempts + 1
			return uc.outbox.ReschedulePublication(ctx, record.EventID, attempts,
				uc.clock().Add(backoff(uc.policy.BaseDelay, uc.policy.MaxDelay, attempts)))
		}
		return uc.outbox.MarkPublished(ctx, record.EventID, uc.clock())
	}))
}
