package postgres

import (
	"context"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/infrastructure/postgres/sqlcgen"
	"github.com/davibanfi/betledger/internal/usecase"
)

var _ usecase.LedgerRepository = (*LedgerRepository)(nil)

// LedgerRepository stores wallet ledger entries in PostgreSQL. The table itself
// rejects updates and deletes, so entries are append-only regardless of callers.
type LedgerRepository struct{}

// NewLedgerRepository builds the repository.
func NewLedgerRepository() *LedgerRepository {
	return &LedgerRepository{}
}

// Append inserts the entry within the transaction carried by ctx.
func (r *LedgerRepository) Append(ctx context.Context, entry *domain.WalletLedgerEntry) error {
	q, err := queries(ctx)
	if err != nil {
		return err
	}

	return translate(q.InsertLedgerEntry(ctx, sqlcgen.InsertLedgerEntryParams{
		ID:                 entry.ID(),
		WalletID:           entry.WalletID(),
		TransactionID:      entry.TransactionID(),
		Direction:          entry.Direction().String(),
		Currency:           entry.Money().Currency().String(),
		AmountMinor:        entry.Money().MinorUnits(),
		BalanceBeforeMinor: entry.BalanceBefore().MinorUnits(),
		BalanceAfterMinor:  entry.BalanceAfter().MinorUnits(),
	}))
}
