package usecase

import (
	"context"
	"errors"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/event"
	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
)

// ProcessWagerInput is an operation sent by a game provider.
type ProcessWagerInput struct {
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	PlayerID                       domain.ID
	WalletID                       domain.ID
	RoundID                        string
	GameID                         string
	Kind                           wager.Kind
	Money                          money.Money
	ReferenceExternalTransactionID string
	CorrelationID                  string
}

// WagerResult is the outcome of an operation, as returned to the provider.
type WagerResult struct {
	TransactionID domain.ID
	State         wager.State
	// FailureCode explains a REJECTED outcome and is empty otherwise.
	FailureCode domain.FailureCode
	// Balance is the balance observed when the operation was processed, and the
	// zero Money when it was not.
	Balance money.Money
	// IdempotentReplay tells that the result was already persisted and nothing
	// was applied again.
	IdempotentReplay bool
}

// ProcessWager applies an operation sent by a game provider to its wallet.
// Business refusals, such as insufficient funds, are persisted as REJECTED and
// returned as a result, not as an error, so that a replay returns them too.
type ProcessWager struct {
	transactor             Transactor
	walletsRepository      WalletRepository
	transactionsRepository TransactionRepository
	ledgerRepository       LedgerRepository
	outboxRepository       OutboxRepository
	clock                  Clock
}

// NewProcessWager builds the use case.
func NewProcessWager(
	transactor Transactor,
	wallets WalletRepository,
	transactionsRepository TransactionRepository,
	ledger LedgerRepository,
	outbox OutboxRepository,
	clock Clock,
) *ProcessWager {
	return &ProcessWager{
		transactor:             transactor,
		walletsRepository:      wallets,
		transactionsRepository: transactionsRepository,
		ledgerRepository:       ledger,
		outboxRepository:       outbox,
		clock:                  clock,
	}
}

// errReferencePending tells that the referenced operation has not arrived, or
// has not concluded yet, so the operation must wait for it.
var errReferencePending = errors.New("usecase: reference pending")

// Execute processes the operation. An operation whose reference has not
// arrived, or is itself still pending, is recorded as PENDING_REFERENCE and
// moves no money until the reference concludes.
func (uc *ProcessWager) Execute(ctx context.Context, input ProcessWagerInput) (WagerResult, error) {
	if input.CorrelationID == "" {
		return WagerResult{}, domain.ValidationError(domain.FailureCodeInvalidInput, "correlationId is required")
	}

	payloadHash, err := PayloadHash(input)
	if err != nil {
		return WagerResult{}, err
	}
	transaction, err := wager.NewExternal(wager.NewExternalParams{
		ID:                             domain.NewID(),
		Kind:                           input.Kind,
		ProviderID:                     input.ProviderID,
		ExternalTransactionID:          input.ExternalTransactionID,
		IdempotencyKey:                 input.IdempotencyKey,
		PayloadHash:                    payloadHash,
		WalletID:                       input.WalletID,
		PlayerID:                       input.PlayerID,
		RoundID:                        input.RoundID,
		GameID:                         input.GameID,
		Money:                          input.Money,
		ReferenceExternalTransactionID: input.ReferenceExternalTransactionID,
	})
	if err != nil {
		return WagerResult{}, err
	}

	var result WagerResult
	err = uc.transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		w, err := uc.walletsRepository.GetForUpdate(ctx, transaction.WalletID())
		if err != nil {
			return err
		}

		previous, err := uc.findReplay(ctx, transaction)
		if err != nil {
			return err
		}
		if previous != nil {
			result = newWagerResult(previous, true)
			return nil
		}

		loadedVersion := w.Version()
		reference, err := uc.findReference(ctx, transaction)
		var entry *ledger.Entry
		if err == nil {
			entry, err = w.Apply(transaction, reference)
		}
		if err := conclude(transaction, w, err); err != nil {
			return err
		}

		events, err := uc.newEvents(transaction, w, entry, input.CorrelationID)
		if err != nil {
			return err
		}
		if err := uc.record(ctx, w, loadedVersion, transaction, entry, events); err != nil {
			return err
		}
		result = newWagerResult(transaction, false)
		return nil
	})
	if err != nil {
		return WagerResult{}, err
	}

	return result, nil
}

// findReplay returns the operation already recorded under the transaction's
// idempotency key, or nil when the transaction is new. The same key with a
// different payload, or the same provider identifier under another key, is an
// idempotency conflict.
func (uc *ProcessWager) findReplay(ctx context.Context, transaction *wager.Transaction) (*wager.Transaction, error) {
	previous, found, err := uc.transactionsRepository.FindByIdempotencyKey(ctx,
		transaction.ProviderID(), transaction.IdempotencyKey())
	if err != nil {
		return nil, err
	}
	if found && previous.PayloadHash() != transaction.PayloadHash() {
		return nil, domain.ConflictError(domain.FailureCodeIdempotencyConflict,
			"idempotency key %q was already used with a different payload", transaction.IdempotencyKey())
	}
	if found {
		return previous, nil
	}

	_, found, err = uc.transactionsRepository.FindByExternalID(ctx,
		transaction.ProviderID(), transaction.ExternalTransactionID())
	if err != nil {
		return nil, err
	}
	if found {
		return nil, domain.ConflictError(domain.FailureCodeIdempotencyConflict,
			"operation %q was already sent with another idempotency key", transaction.ExternalTransactionID())
	}
	return nil, nil
}

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
func (uc *ProcessWager) newEvents(
	transaction *wager.Transaction,
	w *wallet.Wallet,
	entry *ledger.Entry,
	correlationID string,
) ([]event.Event, error) {
	occurredAt := uc.clock()
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

// record writes the transaction and its events and, when the balance moved, the
// new balance, conditioned on the version loaded, and the ledger entry.
func (uc *ProcessWager) record(
	ctx context.Context,
	w *wallet.Wallet,
	loadedVersion int64,
	transaction *wager.Transaction,
	entry *ledger.Entry,
	events []event.Event,
) error {
	if err := uc.transactionsRepository.Create(ctx, transaction); err != nil {
		return err
	}
	if entry != nil {
		if err := uc.walletsRepository.UpdateBalance(ctx, w, loadedVersion); err != nil {
			return err
		}
		if err := uc.ledgerRepository.Append(ctx, entry); err != nil {
			return err
		}
	}
	return uc.outboxRepository.Append(ctx, events...)
}

// findReference returns the operation the transaction refers to, resolved and
// checked, or nil when it refers to none. It returns errReferencePending when the
// reference has not arrived or has not concluded, and a rejection when the
// reference disagrees with the operation or, for a reversal, was already
// reversed. The wallet lock must be held, so that a reversal and its reference
// are never decided concurrently.
func (uc *ProcessWager) findReference(ctx context.Context, transaction *wager.Transaction) (*wager.Transaction, error) {
	if transaction.ReferenceExternalTransactionID() == "" {
		return nil, nil
	}
	reference, found, err := uc.transactionsRepository.FindByExternalID(ctx,
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
	reversed, err := uc.transactionsRepository.HasProcessedReversal(ctx, reference.ID())
	if err != nil {
		return nil, err
	}
	if reversed {
		return nil, domain.RejectionError(domain.FailureCodeReferenceAlreadyReversed,
			"%s was already reversed", reference.ExternalTransactionID())
	}
	return reference, nil
}

func newWagerResult(transaction *wager.Transaction, replay bool) WagerResult {
	balance, _ := transaction.ResultBalance()
	return WagerResult{
		TransactionID:    transaction.ID(),
		State:            transaction.State(),
		FailureCode:      transaction.FailureCode(),
		Balance:          balance,
		IdempotentReplay: replay,
	}
}
