// Package ledger implements the append-only wallet ledger.
package ledger

import (
	"time"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
)

// Entry is an immutable entry of the append-only ledger. Financial corrections
// require new entries, never editing an existing one.
type Entry struct {
	// id is the internal identifier of the entry.
	id domain.ID
	// walletID is the wallet whose balance the entry moved.
	walletID domain.ID
	// transactionID is the operation that produced the entry; together with
	// walletID it is unique, so an operation never moves a wallet twice.
	transactionID domain.ID
	// direction tells whether money was added to or removed from the balance.
	direction Direction
	// amount is always positive; the sign comes from direction.
	amount money.Money
	// balanceBefore is the wallet balance right before the entry.
	balanceBefore money.Money
	// balanceAfter equals balanceBefore plus or minus amount, per direction.
	balanceAfter money.Money
	// createdAt is when the entry was recorded; it never changes, because the
	// ledger is append-only, and it orders the pages of the ledger.
	createdAt time.Time
}

// NewEntry creates an entry, validating the balance arithmetic. It serves both
// creation and rehydration: construction only validates.
func NewEntry(
	id, walletID, transactionID domain.ID,
	direction Direction,
	amount, balanceBefore, balanceAfter money.Money,
	createdAt time.Time,
) (*Entry, error) {
	if err := domain.RequireID(id, "ledger entry id"); err != nil {
		return nil, err
	}
	if err := domain.RequireID(walletID, "walletId"); err != nil {
		return nil, err
	}
	if err := domain.RequireID(transactionID, "transactionId"); err != nil {
		return nil, err
	}
	if err := domain.RequireTime(createdAt, "createdAt"); err != nil {
		return nil, err
	}
	if !direction.IsValid() {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput, "direction %q is invalid", direction)
	}
	if err := amount.Validate(); err != nil {
		return nil, err
	}
	if !amount.IsPositive() {
		return nil, domain.ValidationError(domain.FailureCodeInvalidAmount,
			"a ledger entry requires an amount greater than zero, got %s", amount)
	}
	if err := requireSameCurrency(amount, balanceBefore, balanceAfter); err != nil {
		return nil, err
	}
	if balanceBefore.IsNegative() || balanceAfter.IsNegative() {
		return nil, domain.ValidationError(domain.FailureCodeInvalidAmount,
			"ledger entry balance cannot be negative: before %s, after %s", balanceBefore, balanceAfter)
	}

	expected, err := direction.Apply(balanceBefore, amount)
	if err != nil {
		return nil, err
	}
	if !expected.Equal(balanceAfter) {
		return nil, domain.ValidationError(domain.FailureCodeInvalidAmount,
			"inconsistent ledger entry: %s %s over %s should result in %s, got %s",
			direction, amount, balanceBefore, expected, balanceAfter)
	}

	return &Entry{
		id:            id,
		walletID:      walletID,
		transactionID: transactionID,
		direction:     direction,
		amount:        amount,
		balanceBefore: balanceBefore,
		balanceAfter:  balanceAfter,
		createdAt:     createdAt,
	}, nil
}

func (e *Entry) ID() domain.ID              { return e.id }
func (e *Entry) WalletID() domain.ID        { return e.walletID }
func (e *Entry) TransactionID() domain.ID   { return e.transactionID }
func (e *Entry) Direction() Direction       { return e.direction }
func (e *Entry) Money() money.Money         { return e.amount }
func (e *Entry) BalanceBefore() money.Money { return e.balanceBefore }
func (e *Entry) BalanceAfter() money.Money  { return e.balanceAfter }
func (e *Entry) CreatedAt() time.Time       { return e.createdAt }

// SignedMoney returns the amount signed by its direction, used by
// reconciliation to rebuild the balance from the ledger.
func (e *Entry) SignedMoney() (money.Money, error) {
	if e.direction == Credit {
		return e.amount, nil
	}
	return e.amount.Neg()
}

func requireSameCurrency(amount, balanceBefore, balanceAfter money.Money) error {
	if _, err := amount.Cmp(balanceBefore); err != nil {
		return err
	}
	_, err := amount.Cmp(balanceAfter)
	return err
}
