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

func TestProcessInboxMessageExecute(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	unchanged := func(*usecase.InboundMessage) {}
	processed := []event.Type{event.TypeWagerTransactionProcessed, event.TypeWalletBalanceChanged}

	tests := []struct {
		name              string
		earlier           []func(message *usecase.InboundMessage)
		mutate            func(message *usecase.InboundMessage)
		wantErr           error
		wantState         wager.State
		wantFailureCode   domain.FailureCode
		wantDuplicate     bool
		wantWalletBalance money.Money
		wantTransactions  int
		wantMessages      int
		wantEventTypes    []event.Type
		wantMetrics       []string
	}{
		{
			name:              "should accept when the message arrives for the first time",
			mutate:            unchanged,
			wantState:         wager.StateProcessed,
			wantWalletBalance: money.MustNew(7500, brl),
			wantTransactions:  1,
			wantMessages:      1,
			wantEventTypes:    processed,
			wantMetrics:       []string{"wager BET PROCESSED false", "message false"},
		},
		{
			name:              "should report a duplicate when the same message is delivered again",
			earlier:           []func(*usecase.InboundMessage){unchanged},
			mutate:            unchanged,
			wantDuplicate:     true,
			wantWalletBalance: money.MustNew(7500, brl),
			wantTransactions:  1,
			wantMessages:      1,
			wantEventTypes:    processed,
			wantMetrics:       []string{"wager BET PROCESSED false", "message false", "message true"},
		},
		{
			name:    "should return IDEMPOTENCY_CONFLICT when the message id carries another payload",
			earlier: []func(*usecase.InboundMessage){unchanged},
			mutate: func(message *usecase.InboundMessage) {
				message.Input.Money = money.MustNew(3000, brl)
				message.Input.IdempotencyKey = "provider-a:another"
				message.Input.ExternalTransactionID = "another"
			},
			wantErr:           domain.FailureCodeIdempotencyConflict,
			wantWalletBalance: money.MustNew(7500, brl),
			wantTransactions:  1,
			wantMessages:      1,
			wantEventTypes:    processed,
			wantMetrics:       []string{"wager BET PROCESSED false", "message false"},
		},
		{
			name:    "should accept when another message carries the operation already applied",
			earlier: []func(*usecase.InboundMessage){unchanged},
			mutate: func(message *usecase.InboundMessage) {
				message.MessageID = "msg-2"
			},
			wantState:         wager.StateProcessed,
			wantDuplicate:     false,
			wantWalletBalance: money.MustNew(7500, brl),
			wantTransactions:  1,
			wantMessages:      2,
			wantEventTypes:    processed,
			wantMetrics: []string{
				"wager BET PROCESSED false", "message false", "wager BET PROCESSED true", "message false",
			},
		},
		{
			name:              "should return INSUFFICIENT_FUNDS when the operation is refused, taking the message in",
			mutate:            func(message *usecase.InboundMessage) { message.Input.Money = money.MustNew(20000, brl) },
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeInsufficientFunds,
			wantWalletBalance: money.MustNew(10000, brl),
			wantTransactions:  1,
			wantMessages:      1,
			wantEventTypes:    []event.Type{event.TypeWagerTransactionRejected},
			wantMetrics:       []string{"wager BET REJECTED false", "message false"},
		},
		{
			name:              "should return INVALID_INPUT when the message has no identifier",
			mutate:            func(message *usecase.InboundMessage) { message.MessageID = "" },
			wantErr:           domain.FailureCodeInvalidInput,
			wantWalletBalance: money.MustNew(10000, brl),
		},
		{
			name:              "should return INVALID_AMOUNT when the operation is invalid, taking nothing in",
			mutate:            func(message *usecase.InboundMessage) { message.Input.Money = money.MustNew(0, brl) },
			wantErr:           domain.FailureCodeInvalidAmount,
			wantWalletBalance: money.MustNew(10000, brl),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			playerID := domain.NewID()
			w, err := wallet.Open(domain.NewID(), playerID, money.MustNew(10000, brl))
			require.NoError(t, err)
			transactor := newFakeTransactor(w)
			metrics := &fakeMetrics{}
			processWager := usecase.NewProcessWager(transactor, fakeWallets{}, fakeTransactions{}, fakeLedger{},
				fakeOutbox{}, func() time.Time { return fixedNow }, metrics)
			uc := usecase.NewProcessInboxMessage(transactor, fakeInbox{}, processWager, metrics)
			base := usecase.InboundMessage{
				MessageID: "msg-1",
				Input: usecase.ProcessWagerInput{
					ProviderID:            "provider-a",
					ExternalTransactionID: "transaction-123",
					IdempotencyKey:        "provider-a:transaction-123",
					PlayerID:              playerID,
					WalletID:              w.ID(),
					RoundID:               "round-987",
					GameID:                "fortune-chimp",
					Kind:                  wager.KindBet,
					Money:                 money.MustNew(2500, brl),
					CorrelationID:         "msg-1",
				},
			}
			for _, mutate := range test.earlier {
				message := base
				mutate(&message)
				_, err := uc.Execute(ctx, message)
				require.NoError(t, err)
			}
			message := base
			test.mutate(&message)

			got, err := uc.Execute(ctx, message)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantState, got.State)
			assert.Equal(t, test.wantFailureCode, got.FailureCode)
			assert.Equal(t, test.wantDuplicate, got.Duplicate)
			assert.Equal(t, test.wantWalletBalance, transactor.committed.wallets[w.ID()].Balance())
			assert.Len(t, transactor.committed.transactions, test.wantTransactions)
			assert.Len(t, transactor.committed.inbox, test.wantMessages)
			assert.Equal(t, test.wantEventTypes, transactor.committed.eventTypes)
			assert.Equal(t, test.wantMetrics, metrics.calls)
		})
	}
}
