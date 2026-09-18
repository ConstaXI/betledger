package event_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/domaintest"
	"github.com/davibanfi/betledger/internal/domain/event"
	"github.com/davibanfi/betledger/internal/domain/ledger"
	"github.com/davibanfi/betledger/internal/domain/wallet"
)

func TestNewWalletBalanceChanged(t *testing.T) {
	t.Parallel()

	occurredAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	debited := domaintest.MustOpenWallet(t, domaintest.MustParseMoney(t, "100.00", "BRL"))
	debit, err := debited.Debit(domaintest.MustParseMoney(t, "25.00", "BRL"), domain.NewID(), domaintest.FixedNow)
	require.NoError(t, err)
	credited := domaintest.MustOpenWallet(t, domaintest.MustParseMoney(t, "100.00", "BRL"))
	credit, err := credited.Credit(domaintest.MustParseMoney(t, "40.00", "BRL"), domain.NewID(), domaintest.FixedNow)
	require.NoError(t, err)

	tests := []struct {
		name          string
		wallet        *wallet.Wallet
		entry         *ledger.Entry
		correlationID string
		occurredAt    time.Time
		wantResult    event.Event
		wantErr       error
	}{
		{
			name:          "should accept when a debit changed the balance",
			wallet:        debited,
			entry:         debit,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantResult: event.Event{
				Type:          event.TypeWalletBalanceChanged,
				AggregateID:   debited.ID(),
				CorrelationID: "correlation-1",
				OccurredAt:    occurredAt,
				Version:       1,
				Data: event.WalletBalanceChangedData{
					WalletID:      debited.ID().String(),
					TransactionID: debit.TransactionID().String(),
					Direction:     ledger.Debit,
					Money:         domaintest.MustParseMoney(t, "25.00", "BRL"),
					BalanceBefore: domaintest.MustParseMoney(t, "100.00", "BRL"),
					BalanceAfter:  domaintest.MustParseMoney(t, "75.00", "BRL"),
					WalletVersion: 2,
				},
			},
		},
		{
			name:          "should accept when a credit changed the balance",
			wallet:        credited,
			entry:         credit,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantResult: event.Event{
				Type:          event.TypeWalletBalanceChanged,
				AggregateID:   credited.ID(),
				CorrelationID: "correlation-1",
				OccurredAt:    occurredAt,
				Version:       1,
				Data: event.WalletBalanceChangedData{
					WalletID:      credited.ID().String(),
					TransactionID: credit.TransactionID().String(),
					Direction:     ledger.Credit,
					Money:         domaintest.MustParseMoney(t, "40.00", "BRL"),
					BalanceBefore: domaintest.MustParseMoney(t, "100.00", "BRL"),
					BalanceAfter:  domaintest.MustParseMoney(t, "140.00", "BRL"),
					WalletVersion: 2,
				},
			},
		},
		{
			name:          "should return INVALID_INPUT when the entry belongs to another wallet",
			wallet:        debited,
			entry:         credit,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantErr:       domain.FailureCodeInvalidInput,
		},
		{
			name:          "should return INVALID_INPUT when the wallet is missing",
			entry:         debit,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantErr:       domain.FailureCodeInvalidInput,
		},
		{
			name:          "should return INVALID_INPUT when the entry is missing",
			wallet:        debited,
			correlationID: "correlation-1",
			occurredAt:    occurredAt,
			wantErr:       domain.FailureCodeInvalidInput,
		},
		{
			name:       "should return INVALID_INPUT when correlationId is missing",
			wallet:     debited,
			entry:      debit,
			occurredAt: occurredAt,
			wantErr:    domain.FailureCodeInvalidInput,
		},
		{
			name:          "should return INVALID_INPUT when occurredAt is missing",
			wallet:        debited,
			entry:         debit,
			correlationID: "correlation-1",
			wantErr:       domain.FailureCodeInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := event.NewWalletBalanceChanged(test.wallet, test.entry, test.correlationID, test.occurredAt)

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantErr == nil, !domain.IsNilID(got.ID))
			got.ID = domain.NilID
			assert.Equal(t, test.wantResult, got)
		})
	}
}

func TestWalletBalanceChangedDataMarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		data       event.WalletBalanceChangedData
		wantResult string
	}{
		{
			name: "should format when the balance was debited",
			data: event.WalletBalanceChangedData{
				WalletID:      "w-1",
				TransactionID: "t-1",
				Direction:     ledger.Debit,
				Money:         domaintest.MustParseMoney(t, "25.00", "BRL"),
				BalanceBefore: domaintest.MustParseMoney(t, "100.00", "BRL"),
				BalanceAfter:  domaintest.MustParseMoney(t, "75.00", "BRL"),
				WalletVersion: 2,
			},
			wantResult: `{"walletId":"w-1","transactionId":"t-1","direction":"DEBIT",
				"money":{"amount":"25.00","currency":"BRL"},
				"balanceBefore":{"amount":"100.00","currency":"BRL"},
				"balanceAfter":{"amount":"75.00","currency":"BRL"},
				"walletVersion":2}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(test.data)

			assert.NoError(t, err)
			assert.JSONEq(t, test.wantResult, string(got))
		})
	}
}
