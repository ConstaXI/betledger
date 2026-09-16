package ledger

import "github.com/davibanfi/betledger/internal/domain/money"

// Direction is the direction of a ledger entry.
type Direction string

const (
	Debit  Direction = "DEBIT"
	Credit Direction = "CREDIT"
)

// IsValid reports whether the direction is known.
func (d Direction) IsValid() bool {
	return d == Debit || d == Credit
}

func (d Direction) String() string { return string(d) }

// Apply returns the balance after moving amount in this direction.
func (d Direction) Apply(balance, amount money.Money) (money.Money, error) {
	if d == Credit {
		return balance.Add(amount)
	}
	return balance.Sub(amount)
}
