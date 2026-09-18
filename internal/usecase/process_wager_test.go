package usecase_test

import (
	"context"
	"slices"
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
	unchanged := func(*usecase.ProcessWagerInput) {}
	operation := func(kind wager.Kind, externalID, referenceID string, amountMinor int64) func(*usecase.ProcessWagerInput) {
		return func(input *usecase.ProcessWagerInput) {
			input.Kind = kind
			input.ExternalTransactionID = externalID
			input.IdempotencyKey = "provider-a:" + externalID
			input.ReferenceExternalTransactionID = referenceID
			input.Money = money.MustNew(amountMinor, brl)
		}
	}
	processed := []event.Type{event.TypeWagerTransactionProcessed, event.TypeWalletBalanceChanged}
	rejected := []event.Type{event.TypeWagerTransactionRejected}
	pending := []event.Type{event.TypeWagerTransactionPendingReference}

	tests := []struct {
		name              string
		initialMinor      int64
		earlier           []func(input *usecase.ProcessWagerInput)
		mutate            func(input *usecase.ProcessWagerInput)
		outboxErr         error
		wantErr           error
		wantState         wager.State
		wantFailureCode   domain.FailureCode
		wantBalance       money.Money
		wantReplay        bool
		wantWalletBalance money.Money
		wantWalletVersion int64
		wantTransactions  int
		wantEntries       int
		wantEventTypes    []event.Type
		wantMetrics       []string
	}{
		{
			name:              "should accept when the balance covers the bet",
			initialMinor:      10000,
			mutate:            unchanged,
			wantState:         wager.StateProcessed,
			wantBalance:       money.MustNew(7500, brl),
			wantWalletBalance: money.MustNew(7500, brl),
			wantWalletVersion: 2,
			wantTransactions:  1,
			wantEntries:       1,
			wantEventTypes:    processed,
			wantMetrics:       []string{"wager BET PROCESSED false"},
		},
		{
			name:              "should accept when the bet takes the whole balance",
			initialMinor:      10000,
			mutate:            func(input *usecase.ProcessWagerInput) { input.Money = money.MustNew(10000, brl) },
			wantState:         wager.StateProcessed,
			wantBalance:       money.MustNew(0, brl),
			wantWalletBalance: money.MustNew(0, brl),
			wantWalletVersion: 2,
			wantTransactions:  1,
			wantEntries:       1,
			wantEventTypes:    processed,
			wantMetrics:       []string{"wager BET PROCESSED false"},
		},
		{
			name:              "should return INSUFFICIENT_FUNDS when the bet exceeds the balance",
			initialMinor:      10000,
			mutate:            func(input *usecase.ProcessWagerInput) { input.Money = money.MustNew(10001, brl) },
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeInsufficientFunds,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
			wantTransactions:  1,
			wantEventTypes:    rejected,
			wantMetrics:       []string{"wager BET REJECTED false"},
		},
		{
			name:              "should return WALLET_PLAYER_MISMATCH when the player does not own the wallet",
			initialMinor:      10000,
			mutate:            func(input *usecase.ProcessWagerInput) { input.PlayerID = domain.NewID() },
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeWalletPlayerMismatch,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
			wantTransactions:  1,
			wantEventTypes:    rejected,
			wantMetrics:       []string{"wager BET REJECTED false"},
		},
		{
			name:         "should return CURRENCY_MISMATCH when the bet is in another currency",
			initialMinor: 10000,
			mutate: func(input *usecase.ProcessWagerInput) {
				input.Money = money.MustNew(2500, money.MustCurrency("USD"))
			},
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeCurrencyMismatch,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
			wantTransactions:  1,
			wantEventTypes:    rejected,
			wantMetrics:       []string{"wager BET REJECTED false"},
		},
		{
			name:              "should return WALLET_NOT_FOUND when the wallet does not exist",
			initialMinor:      10000,
			mutate:            func(input *usecase.ProcessWagerInput) { input.WalletID = domain.NewID() },
			wantErr:           domain.FailureCodeWalletNotFound,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
		},
		{
			name:              "should accept when a win credits the wallet",
			initialMinor:      10000,
			mutate:            func(input *usecase.ProcessWagerInput) { input.Kind = wager.KindWin },
			wantState:         wager.StateProcessed,
			wantBalance:       money.MustNew(12500, brl),
			wantWalletBalance: money.MustNew(12500, brl),
			wantWalletVersion: 2,
			wantTransactions:  1,
			wantEntries:       1,
			wantEventTypes:    processed,
			wantMetrics:       []string{"wager WIN PROCESSED false"},
		},
		{
			name:              "should accept when a loss records the round without moving the balance",
			initialMinor:      10000,
			mutate:            operation(wager.KindLoss, "loss", "", 0),
			wantState:         wager.StateProcessed,
			wantBalance:       money.MustNew(10000, brl),
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
			wantTransactions:  1,
			wantEventTypes:    []event.Type{event.TypeWagerTransactionProcessed},
			wantMetrics:       []string{"wager LOSS PROCESSED false"},
		},
		{
			name:         "should return CURRENCY_MISMATCH when a loss is in another currency",
			initialMinor: 10000,
			mutate: func(input *usecase.ProcessWagerInput) {
				input.Kind = wager.KindLoss
				input.Money = money.MustNew(0, money.MustCurrency("USD"))
			},
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeCurrencyMismatch,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
			wantTransactions:  1,
			wantEventTypes:    rejected,
			wantMetrics:       []string{"wager LOSS REJECTED false"},
		},
		{
			name:              "should return INVALID_AMOUNT when a loss carries an amount",
			initialMinor:      10000,
			mutate:            func(input *usecase.ProcessWagerInput) { input.Kind = wager.KindLoss },
			wantErr:           domain.FailureCodeInvalidAmount,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
		},
		{
			name:              "should return INVALID_AMOUNT when the bet is zero",
			initialMinor:      10000,
			mutate:            func(input *usecase.ProcessWagerInput) { input.Money = money.MustNew(0, brl) },
			wantErr:           domain.FailureCodeInvalidAmount,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
		},
		{
			name:              "should return INVALID_INPUT when the idempotency key is missing",
			initialMinor:      10000,
			mutate:            func(input *usecase.ProcessWagerInput) { input.IdempotencyKey = "" },
			wantErr:           domain.FailureCodeInvalidInput,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
		},
		{
			name:              "should return INVALID_INPUT when correlationId is missing",
			initialMinor:      10000,
			mutate:            func(input *usecase.ProcessWagerInput) { input.CorrelationID = "" },
			wantErr:           domain.FailureCodeInvalidInput,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
		},
		{
			name:              "should return ErrUnavailable when the outbox cannot be written",
			initialMinor:      10000,
			mutate:            unchanged,
			outboxErr:         usecase.ErrUnavailable,
			wantErr:           usecase.ErrUnavailable,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
		},
		{
			name:              "should report a replay with the original balance when the bet is resent after other movements",
			initialMinor:      10000,
			earlier:           []func(*usecase.ProcessWagerInput){unchanged, operation(wager.KindBet, "transaction-124", "", 2500)},
			mutate:            func(input *usecase.ProcessWagerInput) { input.CorrelationID = "req-retry" },
			wantState:         wager.StateProcessed,
			wantBalance:       money.MustNew(7500, brl),
			wantReplay:        true,
			wantWalletBalance: money.MustNew(5000, brl),
			wantWalletVersion: 3,
			wantTransactions:  2,
			wantEntries:       2,
			wantEventTypes:    slices.Concat(processed, processed),
			wantMetrics: []string{
				"wager BET PROCESSED false",
				"wager BET PROCESSED false",
				"wager BET PROCESSED true",
			},
		},
		{
			name:              "should report a replay of INSUFFICIENT_FUNDS when the rejected bet is resent",
			initialMinor:      1000,
			earlier:           []func(*usecase.ProcessWagerInput){unchanged},
			mutate:            unchanged,
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeInsufficientFunds,
			wantReplay:        true,
			wantWalletBalance: money.MustNew(1000, brl),
			wantWalletVersion: 1,
			wantTransactions:  1,
			wantEventTypes:    rejected,
			wantMetrics:       []string{"wager BET REJECTED false", "wager BET REJECTED true"},
		},
		{
			name:              "should return IDEMPOTENCY_CONFLICT when the key is reused with a different payload",
			initialMinor:      10000,
			earlier:           []func(*usecase.ProcessWagerInput){unchanged},
			mutate:            func(input *usecase.ProcessWagerInput) { input.Money = money.MustNew(3000, brl) },
			wantErr:           domain.FailureCodeIdempotencyConflict,
			wantWalletBalance: money.MustNew(7500, brl),
			wantWalletVersion: 2,
			wantTransactions:  1,
			wantEntries:       1,
			wantEventTypes:    processed,
			wantMetrics:       []string{"wager BET PROCESSED false"},
		},
		{
			name:              "should return IDEMPOTENCY_CONFLICT when the operation is resent with another key",
			initialMinor:      10000,
			earlier:           []func(*usecase.ProcessWagerInput){unchanged},
			mutate:            func(input *usecase.ProcessWagerInput) { input.IdempotencyKey = "provider-a:retry" },
			wantErr:           domain.FailureCodeIdempotencyConflict,
			wantWalletBalance: money.MustNew(7500, brl),
			wantWalletVersion: 2,
			wantTransactions:  1,
			wantEntries:       1,
			wantEventTypes:    processed,
			wantMetrics:       []string{"wager BET PROCESSED false"},
		},
		{
			name:              "should accept when a refund returns a processed bet",
			initialMinor:      10000,
			earlier:           []func(*usecase.ProcessWagerInput){operation(wager.KindBet, "bet", "", 2500)},
			mutate:            operation(wager.KindRefund, "refund", "bet", 2500),
			wantState:         wager.StateProcessed,
			wantBalance:       money.MustNew(10000, brl),
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 3,
			wantTransactions:  2,
			wantEntries:       2,
			wantEventTypes:    slices.Concat(processed, processed),
			wantMetrics:       []string{"wager BET PROCESSED false", "wager REFUND PROCESSED false"},
		},
		{
			name:              "should accept when a win refers to a processed bet",
			initialMinor:      10000,
			earlier:           []func(*usecase.ProcessWagerInput){operation(wager.KindBet, "bet", "", 2500)},
			mutate:            operation(wager.KindWin, "win", "bet", 4000),
			wantState:         wager.StateProcessed,
			wantBalance:       money.MustNew(11500, brl),
			wantWalletBalance: money.MustNew(11500, brl),
			wantWalletVersion: 3,
			wantTransactions:  2,
			wantEntries:       2,
			wantEventTypes:    slices.Concat(processed, processed),
			wantMetrics:       []string{"wager BET PROCESSED false", "wager WIN PROCESSED false"},
		},
		{
			name:              "should accept when a rollback undoes a processed win",
			initialMinor:      10000,
			earlier:           []func(*usecase.ProcessWagerInput){operation(wager.KindWin, "win", "", 2500)},
			mutate:            operation(wager.KindRollback, "rollback", "win", 2500),
			wantState:         wager.StateProcessed,
			wantBalance:       money.MustNew(10000, brl),
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 3,
			wantTransactions:  2,
			wantEntries:       2,
			wantEventTypes:    slices.Concat(processed, processed),
			wantMetrics:       []string{"wager WIN PROCESSED false", "wager ROLLBACK PROCESSED false"},
		},
		{
			name:         "should return REFERENCE_ALREADY_REVERSED when a bet is refunded twice",
			initialMinor: 10000,
			earlier: []func(*usecase.ProcessWagerInput){
				operation(wager.KindBet, "bet", "", 2500),
				operation(wager.KindRefund, "refund-1", "bet", 2500),
			},
			mutate:            operation(wager.KindRefund, "refund-2", "bet", 2500),
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeReferenceAlreadyReversed,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 3,
			wantTransactions:  3,
			wantEntries:       2,
			wantEventTypes:    slices.Concat(processed, processed, rejected),
			wantMetrics: []string{
				"wager BET PROCESSED false",
				"wager REFUND PROCESSED false",
				"wager REFUND REJECTED false",
			},
		},
		{
			name:         "should return REFERENCE_ALREADY_REVERSED when a refunded bet is rolled back",
			initialMinor: 10000,
			earlier: []func(*usecase.ProcessWagerInput){
				operation(wager.KindBet, "bet", "", 2500),
				operation(wager.KindRefund, "refund", "bet", 2500),
			},
			mutate:            operation(wager.KindRollback, "rollback", "bet", 2500),
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeReferenceAlreadyReversed,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 3,
			wantTransactions:  3,
			wantEntries:       2,
			wantEventTypes:    slices.Concat(processed, processed, rejected),
			wantMetrics: []string{
				"wager BET PROCESSED false",
				"wager REFUND PROCESSED false",
				"wager ROLLBACK REJECTED false",
			},
		},
		{
			name:              "should return REFERENCE_NOT_PROCESSED when the bet was rejected",
			initialMinor:      1000,
			earlier:           []func(*usecase.ProcessWagerInput){operation(wager.KindBet, "bet", "", 2500)},
			mutate:            operation(wager.KindRefund, "refund", "bet", 2500),
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeReferenceNotProcessed,
			wantWalletBalance: money.MustNew(1000, brl),
			wantWalletVersion: 1,
			wantTransactions:  2,
			wantEventTypes:    slices.Concat(rejected, rejected),
			wantMetrics:       []string{"wager BET REJECTED false", "wager REFUND REJECTED false"},
		},
		{
			name:              "should return REFERENCE_AMOUNT_MISMATCH when a refund is partial",
			initialMinor:      10000,
			earlier:           []func(*usecase.ProcessWagerInput){operation(wager.KindBet, "bet", "", 2500)},
			mutate:            operation(wager.KindRefund, "refund", "bet", 1000),
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeReferenceAmountMismatch,
			wantWalletBalance: money.MustNew(7500, brl),
			wantWalletVersion: 2,
			wantTransactions:  2,
			wantEntries:       1,
			wantEventTypes:    slices.Concat(processed, rejected),
			wantMetrics:       []string{"wager BET PROCESSED false", "wager REFUND REJECTED false"},
		},
		{
			name:         "should return INSUFFICIENT_FUNDS_FOR_REVERSAL when rolling back a spent win",
			initialMinor: 0,
			earlier: []func(*usecase.ProcessWagerInput){
				operation(wager.KindWin, "win", "", 2500),
				operation(wager.KindBet, "bet", "", 2000),
			},
			mutate:            operation(wager.KindRollback, "rollback", "win", 2500),
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeInsufficientFundsForReversal,
			wantWalletBalance: money.MustNew(500, brl),
			wantWalletVersion: 3,
			wantTransactions:  3,
			wantEntries:       2,
			wantEventTypes:    slices.Concat(processed, processed, rejected),
			wantMetrics: []string{
				"wager WIN PROCESSED false",
				"wager BET PROCESSED false",
				"wager ROLLBACK REJECTED false",
			},
		},
		{
			name:              "should report PENDING_REFERENCE when the referenced operation has not arrived",
			initialMinor:      10000,
			mutate:            operation(wager.KindRefund, "refund", "bet", 2500),
			wantState:         wager.StatePendingReference,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
			wantTransactions:  1,
			wantEventTypes:    pending,
			wantMetrics:       []string{"wager REFUND PENDING_REFERENCE false"},
		},
		{
			name:              "should report PENDING_REFERENCE when the reference is itself pending",
			initialMinor:      10000,
			earlier:           []func(*usecase.ProcessWagerInput){operation(wager.KindRefund, "refund", "bet", 2500)},
			mutate:            operation(wager.KindRollback, "rollback", "refund", 2500),
			wantState:         wager.StatePendingReference,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
			wantTransactions:  2,
			wantEventTypes:    slices.Concat(pending, pending),
			wantMetrics: []string{
				"wager REFUND PENDING_REFERENCE false",
				"wager ROLLBACK PENDING_REFERENCE false",
			},
		},
		{
			name:              "should report a replay of PENDING_REFERENCE when the waiting refund is resent",
			initialMinor:      10000,
			earlier:           []func(*usecase.ProcessWagerInput){operation(wager.KindRefund, "refund", "bet", 2500)},
			mutate:            operation(wager.KindRefund, "refund", "bet", 2500),
			wantState:         wager.StatePendingReference,
			wantReplay:        true,
			wantWalletBalance: money.MustNew(10000, brl),
			wantWalletVersion: 1,
			wantTransactions:  1,
			wantEventTypes:    pending,
			wantMetrics:       []string{"wager REFUND PENDING_REFERENCE false", "wager REFUND PENDING_REFERENCE true"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			playerID := domain.NewID()
			w, err := wallet.Open(domain.NewID(), playerID, money.MustNew(test.initialMinor, brl), fixedNow)
			require.NoError(t, err)
			transactor := newFakeTransactor(w)
			metrics := &fakeMetrics{}
			uc := usecase.NewProcessWager(transactor, fakeWallets{}, fakeTransactions{}, fakeLedger{},
				fakeOutbox{err: test.outboxErr}, func() time.Time { return fixedNow }, metrics)
			base := usecase.ProcessWagerInput{
				ProviderID:            "provider-a",
				ExternalTransactionID: "transaction-123",
				IdempotencyKey:        "provider-a:transaction-123",
				PlayerID:              playerID,
				WalletID:              w.ID(),
				RoundID:               "round-987",
				GameID:                "fortune-chimp",
				Kind:                  wager.KindBet,
				Money:                 money.MustNew(2500, brl),
				CorrelationID:         "req-1",
			}
			var earlierIDs []domain.ID
			for _, mutate := range test.earlier {
				input := base
				mutate(&input)
				earlier, err := uc.Execute(context.Background(), input)
				require.NoError(t, err)
				earlierIDs = append(earlierIDs, earlier.TransactionID)
			}
			input := base
			test.mutate(&input)

			got, err := uc.Execute(context.Background(), input)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantState, got.State)
			assert.Equal(t, test.wantFailureCode, got.FailureCode)
			assert.Equal(t, test.wantBalance, got.Balance)
			assert.Equal(t, test.wantReplay, got.IdempotentReplay)
			assert.Equal(t, test.wantReplay, slices.Contains(earlierIDs, got.TransactionID))
			assert.Equal(t, test.wantWalletBalance, transactor.committed.wallets[w.ID()].Balance())
			assert.Equal(t, test.wantWalletVersion, transactor.committed.wallets[w.ID()].Version())
			assert.Len(t, transactor.committed.transactions, test.wantTransactions)
			assert.Len(t, transactor.committed.entries, test.wantEntries)
			assert.Equal(t, test.wantEventTypes, transactor.committed.eventTypes)
			assert.Equal(t, test.wantMetrics, metrics.calls)
			for _, e := range transactor.committed.events {
				assert.Equal(t, fixedNow, e.OccurredAt)
			}
		})
	}
}
