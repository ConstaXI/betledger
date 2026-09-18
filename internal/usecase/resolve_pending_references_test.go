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

func TestResolvePendingReferencesExecute(t *testing.T) {
	t.Parallel()

	brl := money.MustCurrency("BRL")
	operation := func(kind wager.Kind, externalID, referenceID string, amountMinor int64) func(*usecase.ProcessWagerInput) {
		return func(input *usecase.ProcessWagerInput) {
			input.Kind = kind
			input.ExternalTransactionID = externalID
			input.IdempotencyKey = "provider-a:" + externalID
			input.ReferenceExternalTransactionID = referenceID
			input.Money = money.MustNew(amountMinor, brl)
		}
	}
	refundOfBet := operation(wager.KindRefund, "refund", "bet", 2500)
	bet := operation(wager.KindBet, "bet", "", 2500)
	processed := []event.Type{event.TypeWagerTransactionProcessed, event.TypeWalletBalanceChanged}
	rejected := []event.Type{event.TypeWagerTransactionRejected}
	pending := []event.Type{event.TypeWagerTransactionPendingReference}

	tests := []struct {
		name              string
		initialMinor      int64
		maxAttempts       int
		earlier           []func(input *usecase.ProcessWagerInput)
		runsBefore        int
		advance           time.Duration
		later             []func(input *usecase.ProcessWagerInput)
		outboxErr         error
		watched           string
		wantErr           error
		wantTaken         int
		wantState         wager.State
		wantFailureCode   domain.FailureCode
		wantAttempts      int
		wantNextAttemptAt time.Time
		wantWalletBalance money.Money
		wantEventTypes    []event.Type
		wantMetrics       []string
	}{
		{
			name:              "should accept when the bet arrived after the refund",
			initialMinor:      10000,
			maxAttempts:       3,
			earlier:           []func(*usecase.ProcessWagerInput){refundOfBet, bet},
			watched:           "refund",
			wantTaken:         1,
			wantState:         wager.StateProcessed,
			wantWalletBalance: money.MustNew(10000, brl),
			wantEventTypes:    slices.Concat(pending, processed, processed),
			wantMetrics:       []string{"reference PROCESSED"},
		},
		{
			name:              "should report the refund still waiting when the bet has not arrived",
			initialMinor:      10000,
			maxAttempts:       3,
			earlier:           []func(*usecase.ProcessWagerInput){refundOfBet},
			watched:           "refund",
			wantTaken:         1,
			wantState:         wager.StatePendingReference,
			wantAttempts:      1,
			wantNextAttemptAt: fixedNow.Add(time.Second),
			wantWalletBalance: money.MustNew(10000, brl),
			wantEventTypes:    pending,
			wantMetrics:       []string{"reference PENDING_REFERENCE"},
		},
		{
			name:              "should report nothing taken when the next attempt is not due",
			initialMinor:      10000,
			maxAttempts:       3,
			earlier:           []func(*usecase.ProcessWagerInput){refundOfBet},
			runsBefore:        1,
			watched:           "refund",
			wantState:         wager.StatePendingReference,
			wantAttempts:      1,
			wantNextAttemptAt: fixedNow.Add(time.Second),
			wantWalletBalance: money.MustNew(10000, brl),
			wantEventTypes:    pending,
			wantMetrics:       []string{"reference PENDING_REFERENCE"},
		},
		{
			name:              "should report a doubled delay when another attempt finds nothing",
			initialMinor:      10000,
			maxAttempts:       3,
			earlier:           []func(*usecase.ProcessWagerInput){refundOfBet},
			runsBefore:        1,
			advance:           time.Second,
			watched:           "refund",
			wantTaken:         1,
			wantState:         wager.StatePendingReference,
			wantAttempts:      2,
			wantNextAttemptAt: fixedNow.Add(time.Second + 2*time.Second),
			wantWalletBalance: money.MustNew(10000, brl),
			wantEventTypes:    pending,
			wantMetrics:       []string{"reference PENDING_REFERENCE", "reference PENDING_REFERENCE"},
		},
		{
			name:              "should report the delay capped at the maximum when attempts keep failing",
			initialMinor:      10000,
			maxAttempts:       10,
			earlier:           []func(*usecase.ProcessWagerInput){refundOfBet},
			runsBefore:        3,
			advance:           10 * time.Second,
			watched:           "refund",
			wantTaken:         1,
			wantState:         wager.StatePendingReference,
			wantAttempts:      4,
			wantNextAttemptAt: fixedNow.Add(30*time.Second + 4*time.Second),
			wantWalletBalance: money.MustNew(10000, brl),
			wantEventTypes:    pending,
			wantMetrics: []string{
				"reference PENDING_REFERENCE", "reference PENDING_REFERENCE",
				"reference PENDING_REFERENCE", "reference PENDING_REFERENCE",
			},
		},
		{
			name:              "should return REFERENCE_NOT_FOUND when the attempts run out",
			initialMinor:      10000,
			maxAttempts:       2,
			earlier:           []func(*usecase.ProcessWagerInput){refundOfBet},
			runsBefore:        1,
			advance:           time.Second,
			watched:           "refund",
			wantTaken:         1,
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeReferenceNotFound,
			wantAttempts:      2,
			wantWalletBalance: money.MustNew(10000, brl),
			wantEventTypes:    slices.Concat(pending, rejected),
			wantMetrics:       []string{"reference PENDING_REFERENCE", "reference REJECTED"},
		},
		{
			name:              "should accept when the bet arrives between attempts",
			initialMinor:      10000,
			maxAttempts:       3,
			earlier:           []func(*usecase.ProcessWagerInput){refundOfBet},
			runsBefore:        1,
			advance:           time.Second,
			later:             []func(*usecase.ProcessWagerInput){bet},
			watched:           "refund",
			wantTaken:         1,
			wantState:         wager.StateProcessed,
			wantAttempts:      1,
			wantWalletBalance: money.MustNew(10000, brl),
			wantEventTypes:    slices.Concat(pending, processed, processed),
			wantMetrics:       []string{"reference PENDING_REFERENCE", "reference PROCESSED"},
		},
		{
			name:              "should return REFERENCE_NOT_PROCESSED when the bet arrived and was rejected",
			initialMinor:      1000,
			maxAttempts:       3,
			earlier:           []func(*usecase.ProcessWagerInput){refundOfBet, bet},
			watched:           "refund",
			wantTaken:         1,
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeReferenceNotProcessed,
			wantWalletBalance: money.MustNew(1000, brl),
			wantEventTypes:    slices.Concat(pending, rejected, rejected),
			wantMetrics:       []string{"reference REJECTED"},
		},
		{
			name:         "should return REFERENCE_ALREADY_REVERSED when the bet was refunded meanwhile",
			initialMinor: 10000,
			maxAttempts:  3,
			earlier: []func(*usecase.ProcessWagerInput){
				refundOfBet,
				bet,
				operation(wager.KindRefund, "prompt-refund", "bet", 2500),
			},
			watched:           "refund",
			wantTaken:         1,
			wantState:         wager.StateRejected,
			wantFailureCode:   domain.FailureCodeReferenceAlreadyReversed,
			wantWalletBalance: money.MustNew(10000, brl),
			wantEventTypes:    slices.Concat(pending, processed, processed, rejected),
			wantMetrics:       []string{"reference REJECTED"},
		},
		{
			name:         "should report the rollback still waiting when its refund concludes in the same run",
			initialMinor: 10000,
			maxAttempts:  3,
			earlier: []func(*usecase.ProcessWagerInput){
				operation(wager.KindRollback, "rollback", "refund", 2500),
				refundOfBet,
				bet,
			},
			watched:           "rollback",
			wantTaken:         2,
			wantState:         wager.StatePendingReference,
			wantAttempts:      1,
			wantNextAttemptAt: fixedNow.Add(time.Second),
			wantWalletBalance: money.MustNew(10000, brl),
			wantEventTypes:    slices.Concat(pending, pending, processed, processed),
			wantMetrics:       []string{"reference PENDING_REFERENCE", "reference PROCESSED"},
		},
		{
			name:              "should return ErrUnavailable when the outcome cannot be written, keeping the lease",
			initialMinor:      10000,
			maxAttempts:       3,
			earlier:           []func(*usecase.ProcessWagerInput){refundOfBet, bet},
			outboxErr:         usecase.ErrUnavailable,
			watched:           "refund",
			wantErr:           usecase.ErrUnavailable,
			wantTaken:         1,
			wantState:         wager.StatePendingReference,
			wantNextAttemptAt: fixedNow.Add(30 * time.Second),
			wantWalletBalance: money.MustNew(7500, brl),
			wantEventTypes:    slices.Concat(pending, processed),
		},
		{
			name:              "should report nothing taken when no operation waits",
			initialMinor:      10000,
			maxAttempts:       3,
			earlier:           []func(*usecase.ProcessWagerInput){bet},
			watched:           "bet",
			wantState:         wager.StateProcessed,
			wantWalletBalance: money.MustNew(7500, brl),
			wantEventTypes:    processed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			playerID := domain.NewID()
			w, err := wallet.Open(domain.NewID(), playerID, money.MustNew(test.initialMinor, brl))
			require.NoError(t, err)
			now := fixedNow
			clock := func() time.Time { return now }
			transactor := newFakeTransactor(w)
			processWager := usecase.NewProcessWager(transactor, fakeWallets{}, fakeTransactions{}, fakeLedger{},
				fakeOutbox{}, clock, &fakeMetrics{})
			metrics := &fakeMetrics{}
			uc := usecase.NewResolvePendingReferences(transactor, fakeWallets{}, fakeTransactions{}, fakeLedger{},
				fakeOutbox{err: test.outboxErr}, clock, usecase.ReferenceRetryPolicy{
					MaxAttempts: test.maxAttempts,
					BaseDelay:   time.Second,
					MaxDelay:    4 * time.Second,
					Lease:       30 * time.Second,
					BatchSize:   10,
				}, metrics)
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
			send := func(mutations []func(*usecase.ProcessWagerInput)) {
				for _, mutate := range mutations {
					input := base
					mutate(&input)
					_, err := processWager.Execute(ctx, input)
					require.NoError(t, err)
				}
			}
			send(test.earlier)
			for range test.runsBefore {
				_, err := uc.Execute(ctx)
				require.NoError(t, err)
				now = now.Add(test.advance)
			}
			send(test.later)

			got, err := uc.Execute(ctx)

			byExternalID := map[string]*wager.Transaction{}
			for _, transaction := range transactor.committed.transactions {
				byExternalID[transaction.ExternalTransactionID()] = transaction
			}
			watched := byExternalID[test.watched]
			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantTaken, got)
			assert.Equal(t, test.wantState, watched.State())
			assert.Equal(t, test.wantFailureCode, watched.FailureCode())
			assert.Equal(t, test.wantAttempts, watched.ReferenceAttempts())
			assert.Equal(t, test.wantNextAttemptAt, transactor.committed.nextAttempts[watched.ID()])
			assert.Equal(t, test.wantWalletBalance, transactor.committed.wallets[w.ID()].Balance())
			assert.Equal(t, test.wantEventTypes, transactor.committed.eventTypes)
			assert.Equal(t, test.wantMetrics, metrics.calls)
		})
	}
}
