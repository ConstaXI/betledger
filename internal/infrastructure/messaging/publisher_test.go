package messaging

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/usecase"
)

func TestEventPublisherPublish(t *testing.T) {
	t.Parallel()

	eventID := domain.NewID()
	aggregateID := domain.NewID()
	record := usecase.OutboxRecord{
		EventID:     eventID,
		AggregateID: aggregateID,
		EventType:   "WalletBalanceChanged",
		Payload:     []byte(`{"eventId":"` + eventID.String() + `"}`),
	}

	tests := []struct {
		name         string
		sendErr      error
		wantErr      error
		wantSent     int
		wantBody     string
		wantGroup    string
		wantDedup    string
		wantAttrType string
	}{
		{
			name:         "should accept when the queue takes the event",
			wantSent:     1,
			wantBody:     string(record.Payload),
			wantGroup:    aggregateID.String(),
			wantDedup:    eventID.String(),
			wantAttrType: "WalletBalanceChanged",
		},
		{
			name:    "should return ErrUnavailable when the queue refuses the event",
			sendErr: errBrokerDown,
			wantErr: usecase.ErrUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			queue := &fakeQueue{sendErr: test.sendErr}
			publisher := &EventPublisher{client: queue, queueURL: "http://queues/wallet-events.fifo"}

			err := publisher.Publish(context.Background(), record)

			var body, group, dedup, attrType string
			for _, sent := range queue.sent {
				body = aws.ToString(sent.MessageBody)
				group = aws.ToString(sent.MessageGroupId)
				dedup = aws.ToString(sent.MessageDeduplicationId)
				attrType = aws.ToString(sent.MessageAttributes["eventType"].StringValue)
			}
			assert.ErrorIs(t, err, test.wantErr)
			assert.Len(t, queue.sent, test.wantSent)
			assert.Equal(t, test.wantBody, body, "the body is the stored snapshot, byte for byte")
			assert.Equal(t, test.wantGroup, group, "the wallet is the message group")
			assert.Equal(t, test.wantDedup, dedup, "the event id deduplicates republications")
			assert.Equal(t, test.wantAttrType, attrType)
		})
	}
}
