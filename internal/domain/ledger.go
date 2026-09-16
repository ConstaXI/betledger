package domain

// LedgerDirection is the direction of a ledger entry.
type LedgerDirection string

const (
	DirectionDebit  LedgerDirection = "DEBIT"
	DirectionCredit LedgerDirection = "CREDIT"
)

// IsValid reports whether the direction is known.
func (d LedgerDirection) IsValid() bool {
	return d == DirectionDebit || d == DirectionCredit
}

func (d LedgerDirection) String() string { return string(d) }

// WalletLedgerEntry is an immutable entry of the append-only ledger. Financial
// corrections require new entries, never editing an existing one.
type WalletLedgerEntry struct {
	id            ID
	walletID      ID
	transactionID ID
	direction     LedgerDirection
	money         Money
	balanceBefore Money
	balanceAfter  Money
}

// NewWalletLedgerEntry creates an entry, validating the balance arithmetic. It
// serves both creation and rehydration: construction only validates.
func NewWalletLedgerEntry(
	id, walletID, transactionID ID,
	direction LedgerDirection,
	money, balanceBefore, balanceAfter Money,
) (*WalletLedgerEntry, error) {
	if err := requireID(id, "ledger entry id"); err != nil {
		return nil, err
	}
	if err := requireID(walletID, "walletId"); err != nil {
		return nil, err
	}
	if err := requireID(transactionID, "transactionId"); err != nil {
		return nil, err
	}
	if !direction.IsValid() {
		return nil, ValidationError(FailureCodeInvalidInput, "direction %q is invalid", direction)
	}
	if err := money.Validate(); err != nil {
		return nil, err
	}
	if !money.IsPositive() {
		return nil, ValidationError(FailureCodeInvalidAmount,
			"a ledger entry requires an amount greater than zero, got %s", money)
	}
	if err := requireSameCurrencyAll(money, balanceBefore, balanceAfter); err != nil {
		return nil, err
	}
	if balanceBefore.IsNegative() || balanceAfter.IsNegative() {
		return nil, ValidationError(FailureCodeInvalidAmount,
			"ledger entry balance cannot be negative: before %s, after %s", balanceBefore, balanceAfter)
	}

	expected, err := applyDirection(balanceBefore, money, direction)
	if err != nil {
		return nil, err
	}
	if !expected.Equal(balanceAfter) {
		return nil, ValidationError(FailureCodeInvalidAmount,
			"inconsistent ledger entry: %s %s over %s should result in %s, got %s",
			direction, money, balanceBefore, expected, balanceAfter)
	}

	return &WalletLedgerEntry{
		id:            id,
		walletID:      walletID,
		transactionID: transactionID,
		direction:     direction,
		money:         money,
		balanceBefore: balanceBefore,
		balanceAfter:  balanceAfter,
	}, nil
}

func applyDirection(balanceBefore, money Money, direction LedgerDirection) (Money, error) {
	if direction == DirectionCredit {
		return balanceBefore.Add(money)
	}
	return balanceBefore.Sub(money)
}

func (e *WalletLedgerEntry) ID() ID                     { return e.id }
func (e *WalletLedgerEntry) WalletID() ID               { return e.walletID }
func (e *WalletLedgerEntry) TransactionID() ID          { return e.transactionID }
func (e *WalletLedgerEntry) Direction() LedgerDirection { return e.direction }
func (e *WalletLedgerEntry) Money() Money               { return e.money }
func (e *WalletLedgerEntry) BalanceBefore() Money       { return e.balanceBefore }
func (e *WalletLedgerEntry) BalanceAfter() Money        { return e.balanceAfter }

// SignedMoney returns the amount signed by its direction, used by
// reconciliation to rebuild the balance from the ledger.
func (e *WalletLedgerEntry) SignedMoney() (Money, error) {
	if e.direction == DirectionCredit {
		return e.money, nil
	}
	return e.money.Neg()
}

func requireSameCurrencyAll(values ...Money) error {
	if len(values) == 0 {
		return nil
	}
	for _, value := range values[1:] {
		if _, err := values[0].Cmp(value); err != nil {
			return err
		}
	}
	return nil
}
