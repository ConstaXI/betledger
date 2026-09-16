package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/event"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/domain/wallet"
	"github.com/davibanfi/betledger/internal/usecase"
)

func TestProcessWagerExecute(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	playerID := domain.NewID()

	tests := []struct {
		name              string
		mutate            func(input *usecase.ProcessWagerInput)
		wantErr           error
		wantState         wager.State
		wantFailureCode   domain.FailureCode
		wantBalance       money.Money
		wantWalletBalance money.Money
		wantWalletVersion int64
		wantTransactions  int
		wantEntries       int
		wantEventTypes    []event.Type
	}{
		{
			name:              "should accept when the balance covers the bet",
			mutate:            func(*usecase.ProcessWagerInput) {},
			wantState:         wager.StateProcessed,
			wantBalance:       money.MustNew(7500, brl),
			wantWalletBalance: money.MustNew(7500, brl),
			wantWalletVersion: 2,
			wantTransactions:  1,
			wantEntries:       1,
			wantEventTypes:    []event.Type{event.TypeWagerTransactionProcessed, event.TypeWalletBalanceChanged},
		},
		{
			name:              "should accept when the bet takes the whole balance",
			mutate:            func(input *usecase.ProcessWagerInput) { input.Money = money.MustNew(10000, brl) },
			wantState:         wager.StateProcessed,
			wantBalance:       money.MustNew(0, brl),
			wantWalletBalance: money.MustNew(0, brl),
			wantWalletVersion: 2,
			wantTransactions:  1,
			wantEntries:       1,
			wantEventTypes:    []event.Type{event.TypeWagerTransactionProcessed, event.TypeWalletBalanceChanged},
		},
		{
			name:              "should return INSUFFICIENT_FUNDS when the bet exceeds the balance",
			mutate:            func(input *usecase.ProcessWagerInput) { input.Money = money.MustNew(10001, brl) },
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeInsufficientFunds,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
			wantTransactions:  1,
			wantEventTypes:    []event.Type{event.TypeWagerTransactionRejected},
		},
		{
			name:              "should return WALLET_PLAYER_MISMATCH when the player does not own the wallet",
			mutate:            func(input *usecase.ProcessWagerInput) { input.PlayerID = domain.NewID() },
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeWalletPlayerMismatch,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
			wantTransactions:  1,
			wantEventTypes:    []event.Type{event.TypeWagerTransactionRejected},
		},
		{
			name: "should return CURRENCY_MISMATCH when the bet is in another currency",
			mutate: func(input *usecase.ProcessWagerInput) {
				input.Money = money.MustNew(2500, money.MustCurrency("USD"))
			},
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeCurrencyMismatch,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
			wantTransactions:  1,
			wantEventTypes:    []event.Type{event.TypeWagerTransactionRejected},
		},
		{
			name:              "should return WALLET_NOT_FOUND when the wallet does not exist",
			mutate:            func(input *usecase.ProcessWagerInput) { input.WalletID = domain.NewID() },
			wantErr:           domain.FailureCodeWalletNotFound,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
		},
		{
			name:              "should return TRANSACTION_KIND_NOT_ALLOWED when the kind is not supported yet",
			mutate:            func(input *usecase.ProcessWagerInput) { input.Kind = wager.KindWin },
			wantErr:           domain.FailureCodeKindNotAllowed,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
		},
		{
			name:              "should return INVALID_INPUT when the idempotency key is missing",
			mutate:            func(input *usecase.ProcessWagerInput) { input.IdempotencyKey = "" },
			wantErr:           domain.FailureCodeInvalidInput,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
		},
		{
			name:              "should return INVALID_INPUT when correlationId is missing",
			mutate:            func(input *usecase.ProcessWagerInput) { input.CorrelationID = "" },
			wantErr:           domain.FailureCodeInvalidInput,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
		},
		{
			name:              "should return INVALID_AMOUNT when the bet is zero",
			mutate:            func(input *usecase.ProcessWagerInput) { input.Money = money.MustNew(0, brl) },
			wantErr:           domain.FailureCodeInvalidAmount,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := mustWallet(t, playerID, money.MustNew(10000, brl))
			transactor := newFakeTransactor(w)
			uc := newProcessWager(transactor)
			input := validBetInput(w.ID(), playerID)
			test.mutate(&input)

			got, err := uc.Execute(context.Background(), input)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantState, got.State)
			assert.Equal(t, test.wantFailureCode, got.FailureCode)
			assert.Equal(t, test.wantBalance, got.Balance)
			assert.False(t, got.IdempotentReplay)
			assert.Equal(t, test.wantWalletBalance, transactor.committed.wallets[w.ID()].Balance())
			assert.Equal(t, test.wantWalletVersion, transactor.committed.wallets[w.ID()].Version())
			assert.Len(t, transactor.committed.transactions, test.wantTransactions)
			assert.Len(t, transactor.committed.entries, test.wantEntries)
			assert.Equal(t, test.wantEventTypes, transactor.committed.eventTypes)
		})
	}
}

func TestProcessWagerReplaysTheOriginalResult(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	playerID := domain.NewID()
	w := mustWallet(t, playerID, money.MustNew(10000, brl))
	transactor := newFakeTransactor(w)
	uc := newProcessWager(transactor)

	first, err := uc.Execute(context.Background(), validBetInput(w.ID(), playerID))
	require.NoError(t, err)

	later := validBetInput(w.ID(), playerID)
	later.ExternalTransactionID = "transaction-124"
	later.IdempotencyKey = "provider-a:transaction-124"
	_, err = uc.Execute(context.Background(), later)
	require.NoError(t, err)

	replay := validBetInput(w.ID(), playerID)
	replay.CorrelationID = "req-retry"
	got, err := uc.Execute(context.Background(), replay)

	require.NoError(t, err)
	assert.True(t, got.IdempotentReplay)
	assert.Equal(t, first.TransactionID, got.TransactionID)
	assert.Equal(t, money.MustNew(7500, brl), got.Balance, "a replay returns the balance observed originally")
	assert.Equal(t, money.MustNew(5000, brl), transactor.committed.wallets[w.ID()].Balance())
	assert.Len(t, transactor.committed.entries, 2)
}

func TestProcessWagerReplaysARejection(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	playerID := domain.NewID()
	w := mustWallet(t, playerID, money.MustNew(1000, brl))
	transactor := newFakeTransactor(w)
	uc := newProcessWager(transactor)
	input := validBetInput(w.ID(), playerID)

	first, err := uc.Execute(context.Background(), input)
	require.NoError(t, err)
	got, err := uc.Execute(context.Background(), input)

	require.NoError(t, err)
	assert.Equal(t, wager.StateRejected, first.State)
	assert.Equal(t, first.TransactionID, got.TransactionID)
	assert.Equal(t, domain.FailureCodeInsufficientFunds, got.FailureCode)
	assert.True(t, got.IdempotentReplay)
	assert.Len(t, transactor.committed.transactions, 1)
	assert.Equal(t, []event.Type{event.TypeWagerTransactionRejected}, transactor.committed.eventTypes)
}

func TestProcessWagerRejectsReusedIdentity(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")

	tests := []struct {
		name   string
		mutate func(input *usecase.ProcessWagerInput)
	}{
		{
			name:   "should return IDEMPOTENCY_CONFLICT when the key is reused with a different payload",
			mutate: func(input *usecase.ProcessWagerInput) { input.Money = money.MustNew(3000, brl) },
		},
		{
			name: "should return IDEMPOTENCY_CONFLICT when the operation is resent with another key",
			mutate: func(input *usecase.ProcessWagerInput) {
				input.IdempotencyKey = "provider-a:transaction-123:retry"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			playerID := domain.NewID()
			w := mustWallet(t, playerID, money.MustNew(10000, brl))
			transactor := newFakeTransactor(w)
			uc := newProcessWager(transactor)
			_, err := uc.Execute(context.Background(), validBetInput(w.ID(), playerID))
			require.NoError(t, err)

			second := validBetInput(w.ID(), playerID)
			test.mutate(&second)
			got, err := uc.Execute(context.Background(), second)

			assert.ErrorIs(t, err, domain.FailureCodeIdempotencyConflict)
			assert.Equal(t, usecase.WagerResult{}, got)
			assert.Equal(t, money.MustNew(7500, brl), transactor.committed.wallets[w.ID()].Balance())
			assert.Len(t, transactor.committed.transactions, 1)
		})
	}
}

func TestProcessWagerDoesNotApplyTheDebitWhenTheCommitFails(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	playerID := domain.NewID()
	w := mustWallet(t, playerID, money.MustNew(10000, brl))
	transactor := newFakeTransactor(w)
	uc := usecase.NewProcessWager(transactor, fakeWallets{}, fakeTransactions{}, fakeLedger{},
		fakeOutbox{err: usecase.ErrUnavailable}, func() time.Time { return fixedNow })

	got, err := uc.Execute(context.Background(), validBetInput(w.ID(), playerID))

	assert.ErrorIs(t, err, usecase.ErrUnavailable)
	assert.Equal(t, usecase.WagerResult{}, got)
	assert.Equal(t, money.MustNew(10000, brl), transactor.committed.wallets[w.ID()].Balance())
	assert.Equal(t, int64(1), transactor.committed.wallets[w.ID()].Version())
	assert.Empty(t, transactor.committed.transactions)
}

func newProcessWager(transactor *fakeTransactor) *usecase.ProcessWager {
	return usecase.NewProcessWager(transactor, fakeWallets{}, fakeTransactions{}, fakeLedger{}, fakeOutbox{},
		func() time.Time { return fixedNow })
}

func mustWallet(t *testing.T, playerID domain.ID, balance money.Money) *wallet.Wallet {
	t.Helper()

	w, err := wallet.Open(domain.NewID(), playerID, balance)
	require.NoError(t, err)
	return w
}

func validBetInput(walletID, playerID domain.ID) usecase.ProcessWagerInput {
	return usecase.ProcessWagerInput{
		ProviderID:            "provider-a",
		ExternalTransactionID: "transaction-123",
		IdempotencyKey:        "provider-a:transaction-123",
		PlayerID:              playerID,
		WalletID:              walletID,
		RoundID:               "round-987",
		GameID:                "fortune-chimp",
		Kind:                  wager.KindBet,
		Money:                 money.MustNew(2500, money.MustCurrency("BRL")),
		CorrelationID:         "req-1",
	}
}
