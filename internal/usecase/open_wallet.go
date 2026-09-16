package usecase

import (
	"context"

	"github.com/davibanfi/betledger/internal/domain"
)

// OpenWalletInput is the request to open a wallet.
type OpenWalletInput struct {
	PlayerID       domain.ID
	InitialBalance domain.Money
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
func (uc *OpenWallet) Execute(ctx context.Context, input OpenWalletInput) (*domain.Wallet, error) {
	if input.CorrelationID == "" {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput, "correlationId is required")
	}

	wallet, err := domain.OpenWallet(domain.NewID(), input.PlayerID, input.InitialBalance)
	if err != nil {
		return nil, err
	}

	err = uc.transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.wallets.Create(ctx, wallet); err != nil {
			return err
		}
		if !wallet.Balance().IsPositive() {
			return nil
		}

		opening, err := domain.NewOpeningTransaction(domain.NewID(), wallet.ID(), wallet.PlayerID(), wallet.Balance())
		if err != nil {
			return err
		}
		if err := uc.transactions.Create(ctx, opening); err != nil {
			return err
		}

		credit, err := wallet.OpeningLedgerEntry(opening.ID())
		if err != nil {
			return err
		}
		if err := uc.ledger.Append(ctx, credit); err != nil {
			return err
		}

		occurredAt := uc.clock()
		processed, err := domain.NewWagerTransactionProcessedEvent(opening, input.CorrelationID, occurredAt)
		if err != nil {
			return err
		}
		balanceChanged, err := domain.NewWalletBalanceChangedEvent(wallet, credit, input.CorrelationID, occurredAt)
		if err != nil {
			return err
		}
		return uc.outbox.Append(ctx, processed, balanceChanged)
	})
	if err != nil {
		return nil, err
	}

	return wallet, nil
}
