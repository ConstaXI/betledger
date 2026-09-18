package usecase

import (
	"context"
	"errors"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
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
	settlement
	transactor Transactor
	metrics    Metrics
}

// NewProcessWager builds the use case.
func NewProcessWager(
	transactor Transactor,
	wallets WalletRepository,
	transactions TransactionRepository,
	ledger LedgerRepository,
	outbox OutboxRepository,
	clock Clock,
	metrics Metrics,
) *ProcessWager {
	return &ProcessWager{
		settlement: settlement{
			walletsRepository:      wallets,
			transactionsRepository: transactions,
			ledgerRepository:       ledger,
			outboxRepository:       outbox,
			clock:                  clock,
		},
		transactor: transactor,
		metrics:    metrics,
	}
}

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

	startedAt := uc.clock()
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
		if err := uc.record(ctx, w, loadedVersion, transaction, entry, events, uc.transactionsRepository.Create); err != nil {
			return err
		}
		result = newWagerResult(transaction, false)
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrConcurrentUpdate) {
			uc.metrics.ConcurrencyConflict(ctx)
		}
		return WagerResult{}, err
	}

	uc.metrics.WagerConcluded(ctx, WagerOutcome{
		Kind:        transaction.Kind(),
		State:       result.State,
		FailureCode: result.FailureCode,
		Replay:      result.IdempotentReplay,
		Duration:    uc.clock().Sub(startedAt),
	})
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
