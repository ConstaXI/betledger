package event_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/davibanfi/betledger/internal/domain/event"
)

func TestEventMarshalJSON(t *testing.T) {
	t.Parallel()

	envelope := event.Event{
		ID:            uuid.MustParse("0192f298-345e-7e38-af88-e43f851a819d"),
		Type:          event.TypeWalletBalanceChanged,
		AggregateID:   uuid.MustParse("0192f291-27dd-7d3f-8071-5f8685deef37"),
		CorrelationID: "correlation-1",
		OccurredAt:    time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
		Version:       1,
		Data:          map[string]string{"walletId": "0192f291-27dd-7d3f-8071-5f8685deef37"},
	}

	tests := []struct {
		name       string
		event      event.Event
		wantResult string
	}{
		{
			name:  "should format when the event has no causation",
			event: envelope,
			wantResult: `{"eventId":"0192f298-345e-7e38-af88-e43f851a819d","eventType":"WalletBalanceChanged",
				"aggregateId":"0192f291-27dd-7d3f-8071-5f8685deef37","correlationId":"correlation-1",
				"occurredAt":"2026-09-08T12:00:00Z","version":1,
				"data":{"walletId":"0192f291-27dd-7d3f-8071-5f8685deef37"}}`,
		},
		{
			name:  "should format when the event carries a causation",
			event: envelope.WithCausation("msg-123"),
			wantResult: `{"eventId":"0192f298-345e-7e38-af88-e43f851a819d","eventType":"WalletBalanceChanged",
				"aggregateId":"0192f291-27dd-7d3f-8071-5f8685deef37","correlationId":"correlation-1",
				"causationId":"msg-123","occurredAt":"2026-09-08T12:00:00Z","version":1,
				"data":{"walletId":"0192f291-27dd-7d3f-8071-5f8685deef37"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(test.event)

			assert.NoError(t, err)
			assert.JSONEq(t, test.wantResult, string(got))
		})
	}
}

func TestEventWithCausation(t *testing.T) {
	t.Parallel()

	envelope := event.Event{
		ID:            uuid.MustParse("0192f298-345e-7e38-af88-e43f851a819d"),
		Type:          event.TypeWagerTransactionProcessed,
		AggregateID:   uuid.MustParse("0192f291-27dd-7d3f-8071-5f8685deef37"),
		CorrelationID: "correlation-1",
		OccurredAt:    time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
		Version:       1,
	}
	caused := envelope
	caused.CausationID = "msg-123"
	recaused := envelope
	recaused.CausationID = "msg-456"

	tests := []struct {
		name        string
		event       event.Event
		causationID string
		wantResult  event.Event
	}{
		{
			name:        "should accept when the event has no causation yet",
			event:       envelope,
			causationID: "msg-123",
			wantResult:  caused,
		},
		{
			name:        "should accept when the event already points at another message",
			event:       caused,
			causationID: "msg-456",
			wantResult:  recaused,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := test.event.WithCausation(test.causationID)

			assert.Equal(t, test.wantResult, got)
		})
	}
}
