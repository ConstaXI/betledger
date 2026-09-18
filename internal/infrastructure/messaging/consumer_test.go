package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/usecase"
)

func TestWagerConsumerExecute(t *testing.T) {
	t.Parallel()

	playerID := domain.NewID()
	walletID := domain.NewID()
	body := func(mutate func(data map[string]any)) string {
		data := map[string]any{
			"providerId":            "provider-a",
			"externalTransactionId": "transaction-123",
			"idempotencyKey":        "provider-a:transaction-123",
			"playerId":              playerID.String(),
			"walletId":              walletID.String(),
			"roundId":               "round-987",
			"gameId":                "fortune-chimp",
			"kind":                  "BET",
			"money":                 map[string]string{"amount": "25.00", "currency": "BRL"},
		}
		mutate(data)
		encoded, err := json.Marshal(map[string]any{
			"messageId":  "msg-1",
			"type":       "WagerTransactionRequested",
			"occurredAt": "2026-09-08T12:00:00Z",
			"data":       data,
		})
		require.NoError(t, err)
		return string(encoded)
	}
	unchanged := func(map[string]any) {}
	applied := usecase.InboundResult{WagerResult: usecase.WagerResult{
		TransactionID: domain.NewID(),
		State:         wager.StateProcessed,
	}}
	expected := []usecase.InboundMessage{{
		MessageID: "msg-1",
		Input: usecase.ProcessWagerInput{
			ProviderID:            "provider-a",
			ExternalTransactionID: "transaction-123",
			IdempotencyKey:        "provider-a:transaction-123",
			PlayerID:              playerID,
			WalletID:              walletID,
			RoundID:               "round-987",
			GameID:                "fortune-chimp",
			Kind:                  wager.KindBet,
			Money:                 money.MustNew(2500, money.MustCurrency("BRL")),
			CorrelationID:         "msg-1",
		},
	}}

	tests := []struct {
		name            string
		body            string
		receiveErr      error
		processErr      error
		wantErr         error
		wantTaken       int
		wantProcessed   []usecase.InboundMessage
		wantDeleted     int
		wantDeadLetters int
	}{
		{
			name:          "should accept when the message carries an operation",
			body:          body(unchanged),
			wantTaken:     1,
			wantProcessed: expected,
			wantDeleted:   1,
		},
		{
			name:            "should send the message to the dead letter queue when the body is malformed",
			body:            `{"messageId":`,
			wantTaken:       1,
			wantDeleted:     1,
			wantDeadLetters: 1,
		},
		{
			name:            "should send the message to the dead letter queue when the type is unknown",
			body:            `{"messageId":"msg-1","type":"SomethingElse","data":{}}`,
			wantTaken:       1,
			wantDeleted:     1,
			wantDeadLetters: 1,
		},
		{
			name:            "should send the message to the dead letter queue when the wallet id is not a UUID",
			body:            body(func(data map[string]any) { data["walletId"] = "wallet-1" }),
			wantTaken:       1,
			wantDeleted:     1,
			wantDeadLetters: 1,
		},
		{
			name:            "should send the message to the dead letter queue when the kind is unknown",
			body:            body(func(data map[string]any) { data["kind"] = "JACKPOT" }),
			wantTaken:       1,
			wantDeleted:     1,
			wantDeadLetters: 1,
		},
		{
			name:            "should send the message to the dead letter queue when the amount is invalid",
			body:            body(func(data map[string]any) { data["money"] = map[string]string{"amount": "25.123", "currency": "BRL"} }),
			wantTaken:       1,
			wantDeleted:     1,
			wantDeadLetters: 1,
		},
		{
			name:          "should keep the message when handling it fails transiently",
			body:          body(unchanged),
			processErr:    fmt.Errorf("%w: database down", usecase.ErrUnavailable),
			wantErr:       usecase.ErrUnavailable,
			wantTaken:     1,
			wantProcessed: expected,
		},
		{
			name:            "should send the message to the dead letter queue when its identifier was reused",
			body:            body(unchanged),
			processErr:      domain.ConflictError(domain.FailureCodeIdempotencyConflict, "message was already taken in"),
			wantTaken:       1,
			wantProcessed:   expected,
			wantDeleted:     1,
			wantDeadLetters: 1,
		},
		{
			name:       "should return ErrUnavailable when the queue cannot be read",
			receiveErr: errBrokerDown,
			wantErr:    usecase.ErrUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			queue := &fakeQueue{
				receiveErr: test.receiveErr,
				received: []types.Message{{
					MessageId:     aws.String("sqs-1"),
					ReceiptHandle: aws.String("receipt-1"),
					Body:          aws.String(test.body),
					Attributes: map[string]string{
						string(types.MessageSystemAttributeNameMessageGroupId): walletID.String(),
					},
				}},
			}
			processor := &fakeProcessor{result: applied, err: test.processErr}
			metrics := &fakeDeadLetterRecorder{}
			consumer := &WagerConsumer{
				client:            queue,
				processMessage:    processor,
				metrics:           metrics,
				logger:            slog.New(slog.DiscardHandler),
				queueURL:          "http://queues/wager-transactions.fifo",
				deadLetterURL:     "http://queues/wager-transactions-dlq.fifo",
				visibilityTimeout: time.Second,
				batchSize:         10,
			}

			got, err := consumer.Execute(context.Background())

			assert.ErrorIs(t, err, test.wantErr)
			assert.Equal(t, test.wantTaken, got)
			assert.Equal(t, test.wantProcessed, processor.received)
			assert.Len(t, queue.deleted, test.wantDeleted)
			assert.Len(t, queue.sent, test.wantDeadLetters)
			assert.Equal(t, test.wantDeadLetters, metrics.calls)
		})
	}
}
