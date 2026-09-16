package event

import (
	"time"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wallet"
)

// WalletBalanceChangedData carries an effective balance change.
type WalletBalanceChangedData struct {
	WalletID      string           `json:"walletId"`
	TransactionID string           `json:"transactionId"`
	Direction     ledger.Direction `json:"direction"`
	Money         money.Money      `json:"money"`
	BalanceBefore money.Money      `json:"balanceBefore"`
	BalanceAfter  money.Money      `json:"balanceAfter"`
	WalletVersion int64            `json:"walletVersion"`
}

// NewWalletBalanceChanged builds the event for an effective balance change,
// taking the amounts from the ledger entry that recorded it.
func NewWalletBalanceChanged(w *wallet.Wallet, entry *ledger.Entry, correlationID string, occurredAt time.Time) (Event, error) {
	if w == nil {
		return Event{}, domain.ValidationError(domain.FailureCodeInvalidInput, "wallet is required")
	}
	if entry == nil {
		return Event{}, domain.ValidationError(domain.FailureCodeInvalidInput, "ledger entry is required")
	}
	if err := requireEnvelope(correlationID, occurredAt); err != nil {
		return Event{}, err
	}
	if entry.WalletID() != w.ID() {
		return Event{}, domain.ValidationError(domain.FailureCodeInvalidInput,
			"ledger entry belongs to wallet %s, not %s", entry.WalletID(), w.ID())
	}

	return newEvent(TypeWalletBalanceChanged, w.ID(), correlationID, occurredAt,
		WalletBalanceChangedData{
			WalletID:      w.ID().String(),
			TransactionID: entry.TransactionID().String(),
			Direction:     entry.Direction(),
			Money:         entry.Money(),
			BalanceBefore: entry.BalanceBefore(),
			BalanceAfter:  entry.BalanceAfter(),
			WalletVersion: w.Version(),
		}), nil
}
