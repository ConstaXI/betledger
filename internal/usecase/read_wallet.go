package usecase

import (
	"context"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wallet"
)

// defaultLedgerPageSize and maxLedgerPageSize bound how many ledger entries a
// page carries, so a caller can neither ask for everything nor for nothing.
const (
	defaultLedgerPageSize = 50
	maxLedgerPageSize     = 200
)

// LedgerPage is a page of ledger entries and where the next one starts.
type LedgerPage struct {
	Entries []LedgerEntry
	// NextCursor points at the last entry returned, and is nil when the page is
	// the last one.
	NextCursor *LedgerCursor
}

// ReconciliationResult compares the stored balance of a wallet with the one
// rebuilt from its ledger. Reconciling never changes a balance: a divergence is
// reported, not corrected.
type ReconciliationResult struct {
	WalletID   domain.ID
	Stored     money.Money
	Calculated money.Money
	// Difference is the stored balance minus the rebuilt one, so a positive
	// value means the wallet holds more than the ledger explains.
	Difference     money.Money
	Consistent     bool
	CheckedEntries int
}

// ReadWallet answers the queries about a wallet: its current state, its ledger
// and its reconciliation.
type ReadWallet struct {
	transactor Transactor
	wallets    WalletRepository
	ledger     LedgerRepository
	metrics    Metrics
}

// NewReadWallet builds the use case.
func NewReadWallet(
	transactor Transactor,
	wallets WalletRepository,
	ledger LedgerRepository,
	metrics Metrics,
) *ReadWallet {
	return &ReadWallet{transactor: transactor, wallets: wallets, ledger: ledger, metrics: metrics}
}

// Wallet returns the wallet.
func (uc *ReadWallet) Wallet(ctx context.Context, walletID domain.ID) (*wallet.Wallet, error) {
	var found *wallet.Wallet
	err := uc.transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		found, err = uc.wallets.Find(ctx, walletID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// Ledger returns a page of the wallet ledger, oldest first. It reads one entry
// beyond the page to tell whether another page follows.
func (uc *ReadWallet) Ledger(
	ctx context.Context,
	walletID domain.ID,
	cursor *LedgerCursor,
	limit int,
) (LedgerPage, error) {
	if limit < 0 || limit > maxLedgerPageSize {
		return LedgerPage{}, domain.ValidationError(domain.FailureCodeInvalidInput,
			"limit must be between 1 and %d, got %d", maxLedgerPageSize, limit)
	}
	if limit == 0 {
		limit = defaultLedgerPageSize
	}

	var page LedgerPage
	err := uc.transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		if _, err := uc.wallets.Find(ctx, walletID); err != nil {
			return err
		}
		entries, err := uc.ledger.Page(ctx, walletID, cursor, limit+1)
		if err != nil {
			return err
		}
		if len(entries) > limit {
			last := entries[limit-1]
			page.NextCursor = &LedgerCursor{RecordedAt: last.RecordedAt, EntryID: last.Entry.ID()}
			entries = entries[:limit]
		}
		page.Entries = entries
		return nil
	})
	if err != nil {
		return LedgerPage{}, err
	}
	return page, nil
}

// Reconcile rebuilds the balance of the wallet from its ledger and compares it
// with the stored one.
func (uc *ReadWallet) Reconcile(ctx context.Context, walletID domain.ID) (ReconciliationResult, error) {
	var result ReconciliationResult
	err := uc.transactor.WithinTransaction(ctx, func(ctx context.Context) error {
		reconciliation, err := uc.wallets.Reconcile(ctx, walletID)
		if err != nil {
			return err
		}
		difference, err := reconciliation.Stored.Sub(reconciliation.Calculated)
		if err != nil {
			return err
		}
		result = ReconciliationResult{
			WalletID:       walletID,
			Stored:         reconciliation.Stored,
			Calculated:     reconciliation.Calculated,
			Difference:     difference,
			Consistent:     difference.IsZero(),
			CheckedEntries: reconciliation.CheckedEntries,
		}
		return nil
	})
	if err != nil {
		return ReconciliationResult{}, err
	}

	uc.metrics.ReconciliationChecked(ctx, result.Consistent)
	return result, nil
}
