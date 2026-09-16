package domain

// Wallet is the root of the financial aggregate. Every balance change goes
// through the aggregate and yields the matching ledger entry, which must be
// committed in the same SQL transaction as the balance.
type Wallet struct {
	// id is the internal UUIDv7 identifier of the wallet.
	id ID
	// playerID identifies the owner; together with the balance currency it is
	// unique across wallets.
	playerID ID
	// balance never goes below zero, and its currency is the wallet currency.
	balance Money
	// version starts at 1 and increments only on a balance change, backing the
	// conditional update that prevents lost updates.
	version int64
}

const initialWalletVersion int64 = 1

// OpenWallet creates a wallet already holding the initial balance. The version
// stays at 1: the opening does not count as a post-creation balance change.
func OpenWallet(id, playerID ID, initialBalance Money) (*Wallet, error) {
	return RehydrateWallet(id, playerID, initialBalance, initialWalletVersion)
}

// RehydrateWallet rebuilds a persisted wallet without reapplying movements.
func RehydrateWallet(id, playerID ID, balance Money, version int64) (*Wallet, error) {
	if err := requireID(id, "walletId"); err != nil {
		return nil, err
	}
	if err := requireID(playerID, "playerId"); err != nil {
		return nil, err
	}
	if err := balance.Validate(); err != nil {
		return nil, err
	}
	if balance.IsNegative() {
		return nil, ValidationError(FailureCodeInvalidAmount,
			"wallet balance cannot be negative, got %s", balance)
	}
	if version < initialWalletVersion {
		return nil, ValidationError(FailureCodeInvalidInput,
			"wallet version must be >= %d, got %d", initialWalletVersion, version)
	}

	return &Wallet{
		id:       id,
		playerID: playerID,
		balance:  balance,
		version:  version,
	}, nil
}

// Credit adds the amount to the balance and returns the matching credit entry.
func (w *Wallet) Credit(money Money, transactionID ID) (*WalletLedgerEntry, error) {
	return w.move(money, transactionID, DirectionCredit)
}

// Debit subtracts the amount, keeping the balance at or above zero, and returns
// the matching debit entry. A shortfall yields an error satisfying
// errors.Is(err, ErrInsufficientFunds), so the application can choose between
// FailureCodeInsufficientFunds and FailureCodeInsufficientFundsForReversal.
func (w *Wallet) Debit(money Money, transactionID ID) (*WalletLedgerEntry, error) {
	return w.move(money, transactionID, DirectionDebit)
}

func (w *Wallet) move(money Money, transactionID ID, direction LedgerDirection) (*WalletLedgerEntry, error) {
	if err := requireID(transactionID, "transactionId"); err != nil {
		return nil, err
	}
	if err := money.Validate(); err != nil {
		return nil, err
	}
	if !money.IsPositive() {
		return nil, ValidationError(FailureCodeInvalidAmount,
			"a movement requires an amount greater than zero, got %s", money)
	}
	if money.Currency() != w.Currency() {
		return nil, ValidationError(FailureCodeCurrencyMismatch,
			"currency %s does not match the wallet currency %s", money.Currency(), w.Currency())
	}

	balanceBefore := w.balance
	balanceAfter, err := applyDirection(balanceBefore, money, direction)
	if err != nil {
		return nil, err
	}
	if balanceAfter.IsNegative() {
		return nil, RejectionErrorWithCause(ErrInsufficientFunds, FailureCodeInsufficientFunds,
			"insufficient funds: balance %s, debit %s", balanceBefore, money)
	}

	entry, err := NewWalletLedgerEntry(NewID(), w.id, transactionID, direction, money, balanceBefore, balanceAfter)
	if err != nil {
		return nil, err
	}

	w.balance = balanceAfter
	w.version++
	return entry, nil
}

// OpeningLedgerEntry produces the opening credit entry, from zero to the
// initial balance, without touching balance or version, which OpenWallet has
// already set. It fails when the opening had no positive initial balance.
func (w *Wallet) OpeningLedgerEntry(transactionID ID) (*WalletLedgerEntry, error) {
	if err := requireID(transactionID, "transactionId"); err != nil {
		return nil, err
	}
	if !w.balance.IsPositive() {
		return nil, ValidationError(FailureCodeInvalidAmount,
			"an opening without a positive balance produces no ledger entry")
	}
	zero, err := ZeroMoney(w.Currency())
	if err != nil {
		return nil, err
	}
	return NewWalletLedgerEntry(NewID(), w.id, transactionID, DirectionCredit, w.balance, zero, w.balance)
}

func (w *Wallet) ID() ID             { return w.id }
func (w *Wallet) PlayerID() ID       { return w.playerID }
func (w *Wallet) Balance() Money     { return w.balance }
func (w *Wallet) Version() int64     { return w.version }
func (w *Wallet) Currency() Currency { return w.balance.Currency() }
