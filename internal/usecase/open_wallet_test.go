package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/event"
	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/usecase"
)

var fixedNow = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func TestOpenWalletExecute(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")

	tests := []struct {
		name             string
		input            usecase.OpenWalletInput
		walletErr        error
		outboxErr        error
		wantErr          error
		wantCalls        int
		wantWallets      int
		wantTransactions int
		wantEntries      int
		wantEventTypes   []event.Type
	}{
		{
			name:             "should record the opening when the initial balance is positive",
			input:            usecase.OpenWalletInput{PlayerID: domain.NewID(), InitialBalance: money.MustNew(100000, brl), CorrelationID: "req-1"},
			wantCalls:        1,
			wantWallets:      1,
			wantTransactions: 1,
			wantEntries:      1,
			wantEventTypes:   []event.Type{event.TypeWagerTransactionProcessed, event.TypeWalletBalanceChanged},
		},
		{
			name:        "should accept without opening records when the initial balance is zero",
			input:       usecase.OpenWalletInput{PlayerID: domain.NewID(), InitialBalance: money.MustNew(0, brl), CorrelationID: "req-1"},
			wantCalls:   1,
			wantWallets: 1,
		},
		{
			name:      "should return WALLET_ALREADY_EXISTS when the player already has a wallet in the currency",
			input:     usecase.OpenWalletInput{PlayerID: domain.NewID(), InitialBalance: money.MustNew(100000, brl), CorrelationID: "req-1"},
			walletErr: domain.ConflictError(domain.FailureCodeWalletAlreadyExists, "wallet already exists"),
			wantErr:   domain.FailureCodeWalletAlreadyExists,
			wantCalls: 1,
		},
		{
			name:      "should return INVALID_INPUT when the player id is nil",
			input:     usecase.OpenWalletInput{PlayerID: domain.NilID, InitialBalance: money.MustNew(100000, brl), CorrelationID: "req-1"},
			wantErr:   domain.FailureCodeInvalidInput,
			wantCalls: 0,
		},
		{
			name:      "should return INVALID_INPUT when the initial balance is uninitialized",
			input:     usecase.OpenWalletInput{PlayerID: domain.NewID(), CorrelationID: "req-1"},
			wantErr:   domain.FailureCodeInvalidInput,
			wantCalls: 0,
		},
		{
			name:      "should return INVALID_INPUT when correlationId is missing",
			input:     usecase.OpenWalletInput{PlayerID: domain.NewID(), InitialBalance: money.MustNew(100000, brl)},
			wantErr:   domain.FailureCodeInvalidInput,
			wantCalls: 0,
		},
		{
			name:      "should return ErrUnavailable when the outbox is unavailable",
			input:     usecase.OpenWalletInput{PlayerID: domain.NewID(), InitialBalance: money.MustNew(100000, brl), CorrelationID: "req-1"},
			outboxErr: usecase.ErrUnavailable,
			wantErr:   usecase.ErrUnavailable,
			wantCalls: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			transactor := newFakeTransactor()
			uc := usecase.NewOpenWallet(transactor, fakeWallets{err: test.walletErr}, fakeTransactions{},
				fakeLedger{}, fakeOutbox{err: test.outboxErr}, func() time.Time { return fixedNow })

			got, err := uc.Execute(context.Background(), test.input)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantErr == nil, got != nil)
			assert.Equal(t, test.wantCalls, transactor.calls)
			assert.Len(t, transactor.committed.wallets, test.wantWallets)
			assert.Len(t, transactor.committed.transactions, test.wantTransactions)
			assert.Len(t, transactor.committed.entries, test.wantEntries)
			assert.Equal(t, test.wantEventTypes, transactor.committed.eventTypes)
			for _, w := range transactor.committed.wallets {
				assert.Equal(t, test.input.PlayerID, w.PlayerID())
				assert.Equal(t, test.input.InitialBalance, w.Balance())
				assert.Equal(t, int64(1), w.Version())
			}
			for _, transaction := range transactor.committed.transactions {
				assert.Equal(t, wager.KindOpening, transaction.Kind())
				assert.Equal(t, wager.StateProcessed, transaction.State())
				assert.Contains(t, transactor.committed.wallets, transaction.WalletID())
			}
			for _, entry := range transactor.committed.entries {
				assert.Equal(t, transactor.committed.transactions[0].ID(), entry.TransactionID())
				assert.Equal(t, ledger.Credit, entry.Direction())
				assert.Equal(t, money.MustNew(0, brl), entry.BalanceBefore())
				assert.Equal(t, test.input.InitialBalance, entry.BalanceAfter())
			}
			for _, e := range transactor.committed.events {
				assert.Contains(t, transactor.committed.wallets, e.AggregateID)
				assert.Equal(t, test.input.CorrelationID, e.CorrelationID)
				assert.Equal(t, fixedNow, e.OccurredAt)
			}
		})
	}
}
