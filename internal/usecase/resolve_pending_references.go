package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/wager"
)

// ReferenceRetryPolicy bounds how operations waiting for a reference are tried
// again.
type ReferenceRetryPolicy struct {
	// MaxAttempts is how many attempts may find no concluded reference before
	// the operation is rejected with REFERENCE_NOT_FOUND.
	MaxAttempts int
	// BaseDelay is the wait after the first unsuccessful attempt; it doubles
	// after each further one.
	BaseDelay time.Duration
	// MaxDelay caps the wait between two attempts.
	MaxDelay time.Duration
	// Lease is how long a worker holds the operations it took before another
	// worker may take them, which recovers work left by a worker that died.
	Lease time.Duration
	// BatchSize is how many operations an execution takes at most.
	BatchSize int
}

// ResolvePendingReferences retries the operations waiting for a reference.
// Each attempt runs the same checks as an operation that finds its reference on
// arrival, under the lock of its wallet.
type ResolvePendingReferences struct {
	settlement
	transactor Transactor
	policy     ReferenceRetryPolicy
	metrics    Metrics
}

// NewResolvePendingReferences builds the use case.
func NewResolvePendingReferences(
	transactor Transactor,
	wallets WalletRepository,
	transactions TransactionRepository,
	ledger LedgerRepository,
	outbox OutboxRepository,
	clock Clock,
	policy ReferenceRetryPolicy,
	metrics Metrics,
) *ResolvePendingReferences {
	return &ResolvePendingReferences{
		walletsRepository:      wallets,
		transactionsRepository: transactions,
		ledgerRepository:       ledger,
		outboxRepository:       outbox,
		clock:                  clock,
		transactor:             transactor,
		policy:                 policy,
		metrics:                metrics,
	}
}

// Execute takes the operations whose next attempt is due and tries each again,
// each in its own database transaction, returning how many it took. The
// reference found concludes the operation as PROCESSED or REJECTED; a reference
// still missing schedules another attempt with exponential backoff, until the
// attempts run out and the operation is rejected with REFERENCE_NOT_FOUND. A
// failure on one operation does not stop the others: its lease runs out and it
// is taken again later.
func (uc *ResolvePendingReferences) Execute(ctx context.Context) (int, error) {
	now := uc.clock()
	var leased []PendingReference
	err := uc.transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		leased, err = uc.transactionsRepository.LeasePendingReferences(ctx, now, now.Add(uc.policy.Lease),
			uc.policy.BatchSize)
		return err
	})
	if err != nil {
		return 0, err
	}

	errs := make([]error, 0, len(leased))
	for _, pending := range leased {
		state, err := uc.retry(ctx, pending)
		errs = append(errs, err)
		uc.observe(ctx, state, err)
	}
	return len(leased), errors.Join(errs...)
}

// observe reports the attempt. A failed attempt reports nothing but the
// conflict it may carry, because its transaction rolled back and the operation
// stayed as it was.
func (uc *ResolvePendingReferences) observe(ctx context.Context, state wager.State, err error) {
	switch {
	case errors.Is(err, ErrConcurrentUpdate):
		uc.metrics.ConcurrencyConflict(ctx)
	case err == nil && state.IsValid():
		uc.metrics.ReferenceAttempted(ctx, state)
	}
}

// retry tries the operation again and reports the state it left it in. An
// operation no longer waiting, because another worker concluded it meanwhile,
// is left untouched and reports the state that worker left.
func (uc *ResolvePendingReferences) retry(
	ctx context.Context,
	pending PendingReference,
) (wager.State, error) {
	at := uc.clock()
	var state wager.State
	err := uc.transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		w, err := uc.walletsRepository.GetForUpdate(ctx, pending.WalletID)
		if err != nil {
			return err
		}
		transaction, found, err := uc.transactionsRepository.FindByID(ctx, pending.TransactionID)
		if err != nil || !found {
			return err
		}
		state = transaction.State()
		if state != wager.StatePendingReference {
			return nil
		}

		loadedVersion := w.Version()
		reference, err := uc.findReference(ctx, transaction)
		var entry *ledger.Entry
		if err == nil {
			entry, err = w.Apply(transaction, reference, at)
		}
		if errors.Is(err, errReferencePending) {
			err = transaction.RecordMissingReference(uc.policy.MaxAttempts, at)
		} else {
			err = conclude(transaction, w, err, at)
		}
		if err != nil {
			return err
		}

		state = transaction.State()
		if state == wager.StatePendingReference {
			nextAttemptAt := at.Add(uc.policy.delay(transaction.ReferenceAttempts()))
			return uc.transactionsRepository.UpdatePendingReference(ctx, transaction, nextAttemptAt)
		}
		events, err := uc.newEvents(transaction, w, entry, "pending-reference-"+transaction.ID().String(), at)
		if err != nil {
			return err
		}
		return uc.record(ctx, w, loadedVersion, transaction, entry, events,
			func(ctx context.Context, transaction *wager.Transaction) error {
				return uc.transactionsRepository.UpdatePendingReference(ctx, transaction, time.Time{})
			})
	})
	return state, err
}

// delay is the wait before the attempt that follows the given number of
// unsuccessful ones.
func (p ReferenceRetryPolicy) delay(attempts int) time.Duration {
	return backoff(p.BaseDelay, p.MaxDelay, attempts)
}

// backoff is base after the first unsuccessful attempt, doubled after each
// further one and capped at maximum.
func backoff(base, maximum time.Duration, attempts int) time.Duration {
	delay := base
	for range attempts - 1 {
		if delay >= maximum/2 {
			return maximum
		}
		delay *= 2
	}
	return min(delay, maximum)
}
