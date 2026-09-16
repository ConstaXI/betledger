package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/usecase"
)

var fixedNow = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func TestOpenWalletExecute(t *testing.T) {
	t.Parallel()

	brl := domain.MustCurrency("BRL")

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
		wantEventTypes   []domain.EventType
	}{
		{
			name:             "should record the opening when the initial balance is positive",
			input:            usecase.OpenWalletInput{PlayerID: domain.NewID(), InitialBalance: domain.MustMoney(100000, brl), CorrelationID: "req-1"},
			wantCalls:        1,
			wantWallets:      1,
			wantTransactions: 1,
			wantEntries:      1,
			wantEventTypes:   []domain.EventType{domain.EventTypeWagerTransactionProcessed, domain.EventTypeWalletBalanceChanged},
		},
		{
			name:        "should accept without opening records when the initial balance is zero",
			input:       usecase.OpenWalletInput{PlayerID: domain.NewID(), InitialBalance: domain.MustMoney(0, brl), CorrelationID: "req-1"},
			wantCalls:   1,
			wantWallets: 1,
		},
		{
			name:      "should return WALLET_ALREADY_EXISTS when the player already has a wallet in the currency",
			input:     usecase.OpenWalletInput{PlayerID: domain.NewID(), InitialBalance: domain.MustMoney(100000, brl), CorrelationID: "req-1"},
			walletErr: domain.ConflictError(domain.FailureCodeWalletAlreadyExists, "wallet already exists"),
			wantErr:   domain.FailureCodeWalletAlreadyExists,
			wantCalls: 1,
		},
		{
			name:      "should return INVALID_INPUT when the player id is nil",
			input:     usecase.OpenWalletInput{PlayerID: domain.NilID, InitialBalance: domain.MustMoney(100000, brl), CorrelationID: "req-1"},
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
			input:     usecase.OpenWalletInput{PlayerID: domain.NewID(), InitialBalance: domain.MustMoney(100000, brl)},
			wantErr:   domain.FailureCodeInvalidInput,
			wantCalls: 0,
		},
		{
			name:      "should persist nothing when the outbox is unavailable",
			input:     usecase.OpenWalletInput{PlayerID: domain.NewID(), InitialBalance: domain.MustMoney(100000, brl), CorrelationID: "req-1"},
			outboxErr: usecase.ErrUnavailable,
			wantErr:   usecase.ErrUnavailable,
			wantCalls: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			transactor := &fakeTransactor{}
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

	brl := domain.MustCurrency("BRL")
	initial := domain.MustMoney(100000, brl)
	playerID := domain.NewID()
	transactor := &fakeTransactor{}
	uc := usecase.NewOpenWallet(transactor, fakeWallets{}, fakeTransactions{}, fakeLedger{}, fakeOutbox{},
		func() time.Time { return fixedNow })

	wallet, err := uc.Execute(context.Background(), usecase.OpenWalletInput{PlayerID: playerID, InitialBalance: initial, CorrelationID: "req-1"})

	require.NoError(t, err)
	require.Len(t, transactor.committed.wallets, 1)
	require.Len(t, transactor.committed.transactions, 1)
	require.Len(t, transactor.committed.entries, 1)
	require.Len(t, transactor.committed.events, 2)

	assert.Same(t, wallet, transactor.committed.wallets[0])
	assert.Equal(t, playerID, wallet.PlayerID())
	assert.Equal(t, initial, wallet.Balance())
	assert.Equal(t, int64(1), wallet.Version())

	transaction := transactor.committed.transactions[0]
	assert.Equal(t, domain.KindOpening, transaction.Kind())
	assert.Equal(t, domain.StateProcessed, transaction.State())
	assert.Equal(t, wallet.ID(), transaction.WalletID())

	entry := transactor.committed.entries[0]
	assert.Equal(t, transaction.ID(), entry.TransactionID())
	assert.Equal(t, domain.DirectionCredit, entry.Direction())
	assert.Equal(t, domain.MustMoney(0, brl), entry.BalanceBefore())
	assert.Equal(t, initial, entry.BalanceAfter())

	for _, event := range transactor.committed.events {
		assert.Equal(t, wallet.ID(), event.AggregateID)
		assert.Equal(t, "req-1", event.CorrelationID)
		assert.Equal(t, fixedNow, event.OccurredAt)
	}
}
