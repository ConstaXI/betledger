// Package wallet implements the wallet, root of the financial aggregate.
package wallet

import (
	"errors"
	"time"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
)

// Wallet is the root of the financial aggregate. Every balance change goes
// through the aggregate and yields the matching ledger entry, which must be
// committed in the same SQL transaction as the balance.
type Wallet struct {
	// id is the internal UUIDv7 identifier of the wallet.
	id domain.ID
	// playerID identifies the owner; together with the balance currency it is
	// unique across wallets.
	playerID domain.ID
	// balance never goes below zero, and its currency is the wallet currency.
	balance money.Money
	// version starts at 1 and increments only on a balance change, backing the
	// conditional update that prevents lost updates.
	version int64
	// createdAt is when the wallet was opened.
	createdAt time.Time
	// updatedAt is when the balance last moved, so it advances with the version
	// and equals createdAt in a wallet that never moved after the opening.
	updatedAt time.Time
}

const initialVersion int64 = 1

// Open creates a wallet already holding the initial balance, opened at the given
// instant. The version stays at 1: the opening does not count as a
// post-creation balance change.
func Open(id, playerID domain.ID, initialBalance money.Money, openedAt time.Time) (*Wallet, error) {
	return Rehydrate(id, playerID, initialBalance, initialVersion, openedAt, openedAt)
}

// Rehydrate rebuilds a persisted wallet without reapplying movements. It does not
// compare the two instants: they come from different processes, whose clocks may
// disagree, and a wallet must stay readable regardless.
func Rehydrate(
	id, playerID domain.ID,
	balance money.Money,
	version int64,
	createdAt, updatedAt time.Time,
) (*Wallet, error) {
	if err := domain.RequireID(id, "walletId"); err != nil {
		return nil, err
	}
	if err := domain.RequireID(playerID, "playerId"); err != nil {
		return nil, err
	}
	if err := balance.Validate(); err != nil {
		return nil, err
	}
	if balance.IsNegative() {
		return nil, domain.ValidationError(domain.FailureCodeInvalidAmount,
			"wallet balance cannot be negative, got %s", balance)
	}
	if version < initialVersion {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput,
			"wallet version must be >= %d, got %d", initialVersion, version)
	}
	if err := domain.RequireTime(createdAt, "createdAt"); err != nil {
		return nil, err
	}
	if err := domain.RequireTime(updatedAt, "updatedAt"); err != nil {
		return nil, err
	}

	return &Wallet{
		id:        id,
		playerID:  playerID,
		balance:   balance,
		version:   version,
		createdAt: createdAt,
		updatedAt: updatedAt,
	}, nil
}

// Credit adds the amount to the balance and returns the matching credit entry,
// recorded at the given instant.
func (w *Wallet) Credit(amount money.Money, transactionID domain.ID, at time.Time) (*ledger.Entry, error) {
	return w.move(amount, transactionID, ledger.Credit, at)
}

// Debit subtracts the amount, keeping the balance at or above zero, and returns
// the matching debit entry. A shortfall yields an error satisfying
// errors.Is(err, domain.ErrInsufficientFunds), so the application can choose
// between FailureCodeInsufficientFunds and FailureCodeInsufficientFundsForReversal.
func (w *Wallet) Debit(amount money.Money, transactionID domain.ID, at time.Time) (*ledger.Entry, error) {
	return w.move(amount, transactionID, ledger.Debit, at)
}

// Apply moves the wallet as the operation demands: a BET debits, a WIN or a
// REFUND credits, a LOSS moves nothing and returns a nil entry, and a ROLLBACK
// moves against its reference, crediting a BET and debiting a WIN or a REFUND.
// A reversal needs the reference it resolved to; other kinds ignore it.
// Refusals — a player who does not own the wallet, another currency or
// insufficient funds — are errors of class domain.ErrRejected, meant to be
// recorded rather than rolled back; a reversal short of funds is refused with
// FailureCodeInsufficientFundsForReversal, apart from a BET.
func (w *Wallet) Apply(operation, reference *wager.Transaction, at time.Time) (*ledger.Entry, error) {
	if operation == nil {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput, "transaction is required")
	}
	if operation.WalletID() != w.id {
		return nil, domain.ValidationError(domain.FailureCodeInvalidInput,
			"transaction %s belongs to wallet %s, not %s", operation.ID(), operation.WalletID(), w.id)
	}
	if operation.Kind().IsReversal() {
		referenceID, resolved := operation.ReferenceTransactionID()
		if !resolved || reference == nil || reference.ID() != referenceID {
			return nil, domain.ValidationError(domain.FailureCodeInvalidInput,
				"%s %s cannot be applied without its resolved reference", operation.Kind(), operation.ID())
		}
	}
	if operation.PlayerID() != w.playerID {
		return nil, domain.RejectionError(domain.FailureCodeWalletPlayerMismatch,
			"player %s does not own wallet %s", operation.PlayerID(), w.id)
	}
	if operation.Money().Currency() != w.Currency() {
		return nil, domain.RejectionError(domain.FailureCodeCurrencyMismatch,
			"wallet %s holds %s, not %s", w.id, w.Currency(), operation.Money().Currency())
	}

	switch {
	case operation.Kind() == wager.KindLoss:
		return nil, nil
	case operation.Kind() == wager.KindBet:
		return w.Debit(operation.Money(), operation.ID(), at)
	case operation.Kind() == wager.KindRollback && reference.Kind() != wager.KindBet:
		entry, err := w.Debit(operation.Money(), operation.ID(), at)
		if errors.Is(err, domain.ErrInsufficientFunds) {
			return nil, domain.RejectionErrorWithCause(domain.ErrInsufficientFunds,
				domain.FailureCodeInsufficientFundsForReversal,
				"insufficient funds to roll back %s: balance %s, debit %s",
				reference.ExternalTransactionID(), w.balance, operation.Money())
		}
		return entry, err
	default:
		return w.Credit(operation.Money(), operation.ID(), at)
	}
}

func (w *Wallet) move(
	amount money.Money,
	transactionID domain.ID,
	direction ledger.Direction,
	at time.Time,
) (*ledger.Entry, error) {
	if err := domain.RequireID(transactionID, "transactionId"); err != nil {
		return nil, err
	}
	if err := amount.Validate(); err != nil {
		return nil, err
	}
	if !amount.IsPositive() {
		return nil, domain.ValidationError(domain.FailureCodeInvalidAmount,
			"a movement requires an amount greater than zero, got %s", amount)
	}
	if amount.Currency() != w.Currency() {
		return nil, domain.ValidationError(domain.FailureCodeCurrencyMismatch,
			"currency %s does not match the wallet currency %s", amount.Currency(), w.Currency())
	}

	balanceBefore := w.balance
	balanceAfter, err := direction.Apply(balanceBefore, amount)
	if err != nil {
		return nil, err
	}
	if balanceAfter.IsNegative() {
		return nil, domain.RejectionErrorWithCause(domain.ErrInsufficientFunds, domain.FailureCodeInsufficientFunds,
			"insufficient funds: balance %s, debit %s", balanceBefore, amount)
	}

	entry, err := ledger.NewEntry(domain.NewID(), w.id, transactionID, direction,
		amount, balanceBefore, balanceAfter, at)
	if err != nil {
		return nil, err
	}

	w.balance = balanceAfter
	w.version++
	w.updatedAt = at
	return entry, nil
}

// OpeningLedgerEntry produces the opening credit entry, from zero to the
// initial balance, without touching balance or version, which Open has already
// set. It fails when the opening had no positive initial balance.
func (w *Wallet) OpeningLedgerEntry(transactionID domain.ID, at time.Time) (*ledger.Entry, error) {
	if err := domain.RequireID(transactionID, "transactionId"); err != nil {
		return nil, err
	}
	if !w.balance.IsPositive() {
		return nil, domain.ValidationError(domain.FailureCodeInvalidAmount,
			"an opening without a positive balance produces no ledger entry")
	}
	zero, err := money.Zero(w.Currency())
	if err != nil {
		return nil, err
	}
	return ledger.NewEntry(domain.NewID(), w.id, transactionID, ledger.Credit, w.balance, zero, w.balance, at)
}

func (w *Wallet) ID() domain.ID            { return w.id }
func (w *Wallet) PlayerID() domain.ID      { return w.playerID }
func (w *Wallet) Balance() money.Money     { return w.balance }
func (w *Wallet) Version() int64           { return w.version }
func (w *Wallet) CreatedAt() time.Time     { return w.createdAt }
func (w *Wallet) UpdatedAt() time.Time     { return w.updatedAt }
func (w *Wallet) Currency() money.Currency { return w.balance.Currency() }
