package usecase

import (
	"context"
	"errors"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/event"
	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
)

// settlement gathers the steps that conclude an operation, shared by the use
// cases that receive operations and that retry the ones waiting for a reference.
type settlement struct {
	walletsRepository      WalletRepository
	transactionsRepository TransactionRepository
	ledgerRepository       LedgerRepository
	outboxRepository       OutboxRepository
	clock                  Clock
}

// errReferencePending tells that the referenced operation has not arrived, or
// has not concluded yet, so the operation must wait for it.
var errReferencePending = errors.New("usecase: reference pending")

// conclude moves the transaction to the state its outcome calls for: waiting
// for the reference, rejected by a business rule, or processed. Any other
// outcome is an error that aborts the database transaction.
func conclude(transaction *wager.Transaction, w *wallet.Wallet, outcome error) error {
	switch {
	case errors.Is(outcome, errReferencePending):
		return transaction.MarkPendingReference()
	case errors.Is(outcome, domain.ErrRejected):
		failureCode, _ := domain.CodeOf(outcome)
		return transaction.MarkRejected(failureCode)
	case outcome != nil:
		return outcome
	default:
		return transaction.MarkProcessed(w.Balance())
	}
}

// newEvents builds the events of a concluded transaction, all stamped with the
// same instant: one for its state, and WalletBalanceChanged when the ledger
// entry shows the balance moved.
func (s settlement) newEvents(
	transaction *wager.Transaction,
	w *wallet.Wallet,
	entry *ledger.Entry,
	correlationID string,
) ([]event.Event, error) {
	occurredAt := s.clock()
	var outcome event.Event
	var err error
	switch transaction.State() {
	case wager.StatePendingReference:
		outcome, err = event.NewWagerTransactionPendingReference(transaction, correlationID, occurredAt)
	case wager.StateRejected:
		outcome, err = event.NewWagerTransactionRejected(transaction, correlationID, occurredAt)
	default:
		outcome, err = event.NewWagerTransactionProcessed(transaction, correlationID, occurredAt)
	}
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return []event.Event{outcome}, nil
	}

	balanceChanged, err := event.NewWalletBalanceChanged(w, entry, correlationID, occurredAt)
	if err != nil {
		return nil, err
	}
	return []event.Event{outcome, balanceChanged}, nil
}

// record saves the transaction with save and writes its events and, when the
// balance moved, the new balance, conditioned on the version loaded, and the
// ledger entry.
func (s settlement) record(
	ctx context.Context,
	w *wallet.Wallet,
	loadedVersion int64,
	transaction *wager.Transaction,
	entry *ledger.Entry,
	events []event.Event,
	save func(ctx context.Context, transaction *wager.Transaction) error,
) error {
	if err := save(ctx, transaction); err != nil {
		return err
	}
	if entry != nil {
		if err := s.walletsRepository.UpdateBalance(ctx, w, loadedVersion); err != nil {
			return err
		}
		if err := s.ledgerRepository.Append(ctx, entry); err != nil {
			return err
		}
	}
	return s.outboxRepository.Append(ctx, events...)
}

// findReference returns the operation the transaction refers to, resolved and
// checked, or nil when it refers to none. It returns errReferencePending when the
// reference has not arrived or has not concluded, and a rejection when the
// reference disagrees with the operation or, for a reversal, was already
// reversed. The wallet lock must be held, so that a reversal and its reference
// are never decided concurrently.
func (s settlement) findReference(ctx context.Context, transaction *wager.Transaction) (*wager.Transaction, error) {
	if transaction.ReferenceExternalTransactionID() == "" {
		return nil, nil
	}
	reference, found, err := s.transactionsRepository.FindByExternalID(ctx,
		transaction.ProviderID(), transaction.ReferenceExternalTransactionID())
	if err != nil {
		return nil, err
	}
	if !found || !reference.State().IsTerminal() {
		return nil, errReferencePending
	}
	if err := transaction.ResolveReference(reference); err != nil {
		return nil, err
	}
	if !transaction.Kind().IsReversal() {
		return reference, nil
	}
	reversed, err := s.transactionsRepository.HasProcessedReversal(ctx, reference.ID())
	if err != nil {
		return nil, err
	}
	if reversed {
		return nil, domain.RejectionError(domain.FailureCodeReferenceAlreadyReversed,
			"%s was already reversed", reference.ExternalTransactionID())
	}
	return reference, nil
}
