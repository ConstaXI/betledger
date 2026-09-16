package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
			name:      "should persist nothing when the outbox is unavailable",
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

			_, err := uc.Execute(context.Background(), test.input)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantCalls, transactor.calls)
			assert.Len(t, transactor.committed.wallets, test.wantWallets)
			assert.Len(t, transactor.committed.transactions, test.wantTransactions)
			assert.Len(t, transactor.committed.entries, test.wantEntries)
			assert.Equal(t, test.wantEventTypes, transactor.committed.eventTypes)
		})
	}
}

func TestOpenWalletRecordsConsistentOpening(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	initial := money.MustNew(100000, brl)
	playerID := domain.NewID()
	transactor := newFakeTransactor()
	uc := usecase.NewOpenWallet(transactor, fakeWallets{}, fakeTransactions{}, fakeLedger{}, fakeOutbox{},
		func() time.Time { return fixedNow })

	w, err := uc.Execute(context.Background(), usecase.OpenWalletInput{PlayerID: playerID, InitialBalance: initial, CorrelationID: "req-1"})

	require.NoError(t, err)
	require.Len(t, transactor.committed.wallets, 1)
	require.Len(t, transactor.committed.transactions, 1)
	require.Len(t, transactor.committed.entries, 1)
	require.Len(t, transactor.committed.events, 2)

	assert.Equal(t, w.Balance(), transactor.committed.wallets[w.ID()].Balance())
	assert.Equal(t, playerID, w.PlayerID())
	assert.Equal(t, initial, w.Balance())
	assert.Equal(t, int64(1), w.Version())

	transaction := transactor.committed.transactions[0]
	assert.Equal(t, wager.KindOpening, transaction.Kind())
	assert.Equal(t, wager.StateProcessed, transaction.State())
	assert.Equal(t, w.ID(), transaction.WalletID())

	entry := transactor.committed.entries[0]
	assert.Equal(t, transaction.ID(), entry.TransactionID())
	assert.Equal(t, ledger.Credit, entry.Direction())
	assert.Equal(t, money.MustNew(0, brl), entry.BalanceBefore())
	assert.Equal(t, initial, entry.BalanceAfter())

	for _, e := range transactor.committed.events {
		assert.Equal(t, w.ID(), e.AggregateID)
		assert.Equal(t, "req-1", e.CorrelationID)
		assert.Equal(t, fixedNow, e.OccurredAt)
	}
}
