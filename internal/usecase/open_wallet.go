package usecase

import (
	"context"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/event"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
)

// OpenWalletInput is the request to open a wallet.
type OpenWalletInput struct {
	PlayerID       domain.ID
	InitialBalance money.Money
	CorrelationID  string
}

// OpenWallet opens a wallet and, when the initial balance is positive, records
// the OPENING transaction, its credit entry and the resulting events in the same
// commit as the wallet.
type OpenWallet struct {
	transactor   Transactor
	wallets      WalletRepository
	transactions TransactionRepository
	ledger       LedgerRepository
	outbox       OutboxRepository
	clock        Clock
}

// NewOpenWallet builds the use case.
func NewOpenWallet(
	transactor Transactor,
	wallets WalletRepository,
	transactions TransactionRepository,
	ledger LedgerRepository,
	outbox OutboxRepository,
	clock Clock,
) *OpenWallet {
	return &OpenWallet{
		transactor:   transactor,
		wallets:      wallets,
		transactions: transactions,
		ledger:       ledger,
		outbox:       outbox,
		clock:        clock,
	}
}

// Execute opens the wallet.
func (uc *OpenWallet) Execute(ctx context.Context, input OpenWalletInput) (*wallet.Wallet, error) {
	if input.CorrelationID == "" {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput, "correlationId is required")
	}

	w, err := wallet.Open(domain.NewID(), input.PlayerID, input.InitialBalance)
	if err != nil {
		return nil, err
	}

	err = uc.transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.wallets.Create(ctx, w); err != nil {
			return err
		}
		if !w.Balance().IsPositive() {
			return nil
		}

		opening, err := wager.NewOpening(domain.NewID(), w.ID(), w.PlayerID(), w.Balance())
		if err != nil {
			return err
		}
		if err := uc.transactions.Create(ctx, opening); err != nil {
			return err
		}

		credit, err := w.OpeningLedgerEntry(opening.ID())
		if err != nil {
			return err
		}
		if err := uc.ledger.Append(ctx, credit); err != nil {
			return err
		}

		occurredAt := uc.clock()
		processed, err := event.NewWagerTransactionProcessed(opening, input.CorrelationID, occurredAt)
		if err != nil {
			return err
		}
		balanceChanged, err := event.NewWalletBalanceChanged(w, credit, input.CorrelationID, occurredAt)
		if err != nil {
			return err
		}
		return uc.outbox.Append(ctx, processed, balanceChanged)
	})
	if err != nil {
		return nil, err
	}

	return w, nil
}
