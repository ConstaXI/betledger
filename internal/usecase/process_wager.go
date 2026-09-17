package usecase

import (
	"context"
	"errors"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/event"
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

// Execute processes the operation. BET, WIN and LOSS are supported so far; a
// WIN tied to a previous operation waits for reference resolution.
func (uc *ProcessWager) Execute(ctx context.Context, input ProcessWagerInput) (WagerResult, error) {
	if input.CorrelationID == "" {
		return WagerResult{}, domain.ValidationError(domain.FailureCodeInvalidInput, "correlationId is required")
	}
	if input.Kind != wager.KindBet && input.Kind != wager.KindWin && input.Kind != wager.KindLoss {
		return WagerResult{}, domain.ValidationError(domain.FailureCodeKindNotAllowed,
			"kind %s is not supported yet", input.Kind)
	}
	if input.Kind == wager.KindWin && input.ReferenceExternalTransactionID != "" {
		return WagerResult{}, domain.ValidationError(domain.FailureCodeKindNotAllowed,
			"a WIN referencing another operation is not supported yet")
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

		previous, found, err := uc.transactionsRepository.FindByIdempotencyKey(ctx, input.ProviderID, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if found && previous.PayloadHash() != payloadHash {
			return domain.ConflictError(domain.FailureCodeIdempotencyConflict,
				"idempotency key %q was already used with a different payload", input.IdempotencyKey)
		}
		if found {
			result = newWagerResult(previous, true)
			return nil
		}

		_, found, err = uc.transactionsRepository.FindByExternalID(ctx, input.ProviderID, input.ExternalTransactionID)
		if err != nil {
			return err
		}
		if found {
			return domain.ConflictError(domain.FailureCodeIdempotencyConflict,
				"operation %q was already sent with another idempotency key", input.ExternalTransactionID)
		}

		loadedVersion := w.Version()
		occurredAt := uc.clock()

		entry, err := w.Apply(transaction)
		rejected := errors.Is(err, domain.ErrRejected)
		if err != nil && !rejected {
			return err
		}

		var events []event.Event
		if rejected {
			failureCode, _ := domain.CodeOf(err)
			if err := transaction.MarkRejected(failureCode); err != nil {
				return err
			}
			rejection, err := event.NewWagerTransactionRejected(transaction, input.CorrelationID, occurredAt)
			if err != nil {
				return err
			}
			events = append(events, rejection)
		} else {
			if err := transaction.MarkProcessed(w.Balance()); err != nil {
				return err
			}
			processed, err := event.NewWagerTransactionProcessed(transaction, input.CorrelationID, occurredAt)
			if err != nil {
				return err
			}
			events = append(events, processed)
		}

		if err := uc.transactionsRepository.Create(ctx, transaction); err != nil {
			return err
		}
		if entry != nil {
			balanceChanged, err := event.NewWalletBalanceChanged(w, entry, input.CorrelationID, occurredAt)
			if err != nil {
				return err
			}
			if err := uc.walletsRepository.UpdateBalance(ctx, w, loadedVersion); err != nil {
				return err
			}
			if err := uc.ledgerRepository.Append(ctx, entry); err != nil {
				return err
			}
			events = append(events, balanceChanged)
		}

		result = newWagerResult(transaction, false)
		return uc.outboxRepository.Append(ctx, events...)
	})
	if err != nil {
		return WagerResult{}, err
	}

	return result, nil
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
