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
	"github.com/davibanfi/betledger/internal/usecase"
)

func TestPublishOutboxExecute(t *testing.T) {
	t.Parallel()

	processed := event.TypeWagerTransactionProcessed.String()
	balanceChanged := event.TypeWalletBalanceChanged.String()

	tests := []struct {
		name              string
		openings          int
		failures          int
		runsBefore        int
		advance           time.Duration
		wantErr           error
		wantTaken         int
		wantPublished     []string
		wantAttempts      int
		wantNextAttemptAt time.Time
	}{
		{
			name:              "should accept when the oldest event of a wallet is due",
			openings:          1,
			wantTaken:         1,
			wantPublished:     []string{processed},
			wantNextAttemptAt: fixedNow.Add(30 * time.Second),
		},
		{
			name:              "should accept when the next event of the wallet follows on another run",
			openings:          1,
			runsBefore:        1,
			wantTaken:         1,
			wantPublished:     []string{processed, balanceChanged},
			wantNextAttemptAt: fixedNow.Add(30 * time.Second),
		},
		{
			name:              "should accept when events of different wallets go out in the same run",
			openings:          2,
			wantTaken:         2,
			wantPublished:     []string{processed, processed},
			wantNextAttemptAt: fixedNow.Add(30 * time.Second),
		},
		{
			name:              "should return ErrUnavailable when the publication fails, scheduling a retry",
			openings:          1,
			failures:          1,
			wantErr:           usecase.ErrUnavailable,
			wantTaken:         1,
			wantAttempts:      1,
			wantNextAttemptAt: fixedNow.Add(time.Second),
		},
		{
			name:              "should report nothing taken when the failed event is not due, holding the wallet back",
			openings:          1,
			failures:          1,
			runsBefore:        1,
			wantAttempts:      1,
			wantNextAttemptAt: fixedNow.Add(time.Second),
		},
		{
			name:              "should return ErrUnavailable with a doubled delay when the retry fails too",
			openings:          1,
			failures:          2,
			runsBefore:        1,
			advance:           time.Second,
			wantErr:           usecase.ErrUnavailable,
			wantTaken:         1,
			wantAttempts:      2,
			wantNextAttemptAt: fixedNow.Add(time.Second + 2*time.Second),
		},
		{
			name:              "should accept when the retry succeeds, publishing the event once",
			openings:          1,
			failures:          1,
			runsBefore:        1,
			advance:           time.Second,
			wantTaken:         1,
			wantPublished:     []string{processed},
			wantAttempts:      1,
			wantNextAttemptAt: fixedNow.Add(time.Second + 30*time.Second),
		},
		{
			name:              "should report nothing taken when every event was published",
			openings:          1,
			runsBefore:        2,
			wantPublished:     []string{processed, balanceChanged},
			wantNextAttemptAt: fixedNow.Add(30 * time.Second),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			now := fixedNow
			clock := func() time.Time { return now }
			transactor := newFakeTransactor()
			openWallet := usecase.NewOpenWallet(transactor, fakeWallets{}, fakeTransactions{}, fakeLedger{},
				fakeOutbox{}, clock)
			for range test.openings {
				_, err := openWallet.Execute(ctx, usecase.OpenWalletInput{
					PlayerID:       domain.NewID(),
					InitialBalance: money.MustNew(10000, money.MustCurrency("BRL")),
					CorrelationID:  "req-1",
				})
				require.NoError(t, err)
			}
			publisher := &fakePublisher{failures: test.failures}
			uc := usecase.NewPublishOutbox(transactor, fakeOutbox{}, publisher, clock, usecase.PublicationPolicy{
				BaseDelay: time.Second,
				MaxDelay:  time.Minute,
				Lease:     30 * time.Second,
				BatchSize: 10,
			})
			for range test.runsBefore {
				_, _ = uc.Execute(ctx)
				now = now.Add(test.advance)
			}

			got, err := uc.Execute(ctx)

			var published []string
			for _, record := range publisher.published {
				published = append(published, record.EventType)
				assert.Contains(t, transactor.committed.published, record.EventID)
			}
			firstID := transactor.committed.events[0].ID
			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantTaken, got)
			assert.Equal(t, test.wantPublished, published)
			assert.Len(t, transactor.committed.published, len(test.wantPublished))
			assert.Equal(t, test.wantAttempts, transactor.committed.publications[firstID])
			assert.Equal(t, test.wantNextAttemptAt, transactor.committed.nextAttempts[firstID])
		})
	}
}
