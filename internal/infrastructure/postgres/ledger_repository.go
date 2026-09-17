package postgres

import (
	"context"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/money"
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
func (r *LedgerRepository) Append(ctx context.Context, entry *ledger.Entry) error {
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

// Page returns the entries of the wallet after the cursor, in the order they
// were recorded, with the identifier breaking ties within the same instant.
func (r *LedgerRepository) Page(
	ctx context.Context,
	walletID domain.ID,
	cursor *usecase.LedgerCursor,
	limit int,
) ([]usecase.LedgerEntry, error) {
	q, err := queries(ctx)
	if err != nil {
		return nil, err
	}

	params := sqlcgen.SelectLedgerPageParams{WalletID: walletID, PageSize: int32(limit)}
	if cursor != nil {
		params.CursorRecordedAt = &cursor.RecordedAt
		params.CursorEntryID = &cursor.EntryID
	}
	rows, err := q.SelectLedgerPage(ctx, params)
	if err != nil {
		return nil, translate(err)
	}

	entries := make([]usecase.LedgerEntry, 0, len(rows))
	for _, row := range rows {
		currency, err := money.NewCurrency(row.Currency)
		if err != nil {
			return nil, err
		}
		amount, err := money.New(row.AmountMinor, currency)
		if err != nil {
			return nil, err
		}
		before, err := money.New(row.BalanceBeforeMinor, currency)
		if err != nil {
			return nil, err
		}
		after, err := money.New(row.BalanceAfterMinor, currency)
		if err != nil {
			return nil, err
		}
		entry, err := ledger.NewEntry(row.ID, row.WalletID, row.TransactionID,
			ledger.Direction(row.Direction), amount, before, after)
		if err != nil {
			return nil, err
		}
		entries = append(entries, usecase.LedgerEntry{Entry: entry, RecordedAt: row.CreatedAt})
	}
	return entries, nil
}
