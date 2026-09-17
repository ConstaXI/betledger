package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/domain"
	"github.com/davibanfi/betledger/internal/domain/money"
	"github.com/davibanfi/betledger/internal/domain/wager"
	"github.com/davibanfi/betledger/internal/infrastructure/config"
	"github.com/davibanfi/betledger/internal/usecase"
)

// wagerMessageType is the only message type the wagering queue carries.
const wagerMessageType = "WagerTransactionRequested"

type wagerMessage struct {
	MessageID string `json:"messageId"`
	Type      string `json:"type"`
	Data      struct {
		ProviderID            string `json:"providerId"`
		ExternalTransactionID string `json:"externalTransactionId"`
		IdempotencyKey        string `json:"idempotencyKey"`
		PlayerID              string `json:"playerId"`
		WalletID              string `json:"walletId"`
		RoundID               string `json:"roundId"`
		GameID                string `json:"gameId"`
		Kind                  string `json:"kind"`
		Money                 struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"money"`
		ReferenceExternalTransactionID string `json:"referenceExternalTransactionId"`
	} `json:"data"`
}

// WagerConsumer takes operations from the wagering queue and applies them
// through the same use case the HTTP endpoint uses, so both entries share the
// financial idempotency. A message is deleted only after its handling is
// committed, so an interruption in between brings the message back.
// messageProcessor applies an operation received from the queue.
type messageProcessor interface {
	Execute(ctx context.Context, message usecase.InboundMessage) (usecase.InboundResult, error)
}

type WagerConsumer struct {
	client            queueAPI
	processMessage    messageProcessor
	logger            *slog.Logger
	queueName         string
	deadLetterName    string
	queueURL          string
	deadLetterURL     string
	waitTime          time.Duration
	visibilityTimeout time.Duration
	batchSize         int
}

// NewWagerConsumer builds the consumer, resolving both queues on start so that
// a missing queue prevents the application from starting.
func NewWagerConsumer(
	lc fx.Lifecycle,
	client *sqs.Client,
	cfg config.Config,
	processMessage *usecase.ProcessInboxMessage,
	logger *slog.Logger,
) *WagerConsumer {
	consumer := &WagerConsumer{
		client:            client,
		processMessage:    processMessage,
		logger:            logger,
		queueName:         cfg.WagerQueueName,
		deadLetterName:    cfg.WagerDeadLetterQueueName,
		waitTime:          cfg.ConsumerWaitTime,
		visibilityTimeout: cfg.ConsumerVisibilityTimeout,
		batchSize:         cfg.ConsumerBatchSize,
	}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			for name, target := range map[string]*string{
				consumer.queueName:      &consumer.queueURL,
				consumer.deadLetterName: &consumer.deadLetterURL,
			} {
				output, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(name)})
				if err != nil {
					return fmt.Errorf("failed to resolve the queue %s: %w", name, err)
				}
				*target = aws.ToString(output.QueueUrl)
			}
			return nil
		},
	})
	return consumer
}

// Execute takes a batch of messages and handles each in turn, returning how
// many it took. A transient failure leaves the message in the queue, which delivers it
// again once its visibility runs out; a permanent one sends it to the dead
// letter queue, so a poisoned message never blocks its group.
func (c *WagerConsumer) Execute(ctx context.Context) (int, error) {
	output, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:                    aws.String(c.queueURL),
		MaxNumberOfMessages:         int32(c.batchSize),
		WaitTimeSeconds:             int32(c.waitTime / time.Second),
		VisibilityTimeout:           int32(c.visibilityTimeout / time.Second),
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{types.MessageSystemAttributeNameAll},
	})
	if err != nil {
		return 0, fmt.Errorf("%w: failed to receive from %s: %w", usecase.ErrUnavailable, c.queueName, err)
	}

	errs := make([]error, 0, len(output.Messages))
	for _, message := range output.Messages {
		errs = append(errs, c.handle(ctx, message))
	}
	return len(output.Messages), errors.Join(errs...)
}

// handle applies one message. The handling itself is bounded by the visibility
// timeout, past which the message is delivered again anyway, so a dependency
// that stops answering does not hold the consumer. Reporting the outcome to the
// queue runs on its own deadline, so that a handling that timed out can still
// be dead lettered or deleted.
func (c *WagerConsumer) handle(ctx context.Context, message types.Message) error {
	handling, cancelHandling := context.WithTimeout(ctx, c.visibilityTimeout)
	defer cancelHandling()

	inbound, err := toInboundMessage(message)
	if err == nil {
		var result usecase.InboundResult
		result, err = c.processMessage.Execute(handling, inbound)
		if err == nil {
			c.logger.InfoContext(ctx, "message handled",
				"messageId", inbound.MessageID,
				"transactionId", result.TransactionID,
				"state", result.State,
				"duplicate", result.Duplicate,
			)
			reporting, cancelReporting := context.WithTimeout(ctx, c.visibilityTimeout)
			defer cancelReporting()
			return c.delete(reporting, message)
		}
	}

	if errors.Is(err, usecase.ErrUnavailable) {
		return fmt.Errorf("message %s will be delivered again: %w", aws.ToString(message.MessageId), err)
	}
	c.logger.ErrorContext(ctx, "message sent to the dead letter queue",
		"messageId", aws.ToString(message.MessageId), "error", err)
	reporting, cancelReporting := context.WithTimeout(ctx, c.visibilityTimeout)
	defer cancelReporting()
	return errors.Join(c.deadLetter(reporting, message, err), c.delete(reporting, message))
}

// deadLetter forwards a message that cannot be handled, keeping its body and
// naming the reason, so that it can be inspected and replayed by hand.
func (c *WagerConsumer) deadLetter(ctx context.Context, message types.Message, reason error) error {
	group := message.Attributes[string(types.MessageSystemAttributeNameMessageGroupId)]
	_, err := c.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(c.deadLetterURL),
		MessageBody:            message.Body,
		MessageGroupId:         aws.String(group),
		MessageDeduplicationId: message.MessageId,
		MessageAttributes: map[string]types.MessageAttributeValue{
			"reason": {DataType: aws.String("String"), StringValue: aws.String(reason.Error())},
		},
	})
	return err
}

func (c *WagerConsumer) delete(ctx context.Context, message types.Message) error {
	_, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(c.queueURL),
		ReceiptHandle: message.ReceiptHandle,
	})
	return err
}

// Check reports whether the wagering queue can be reached.
func (c *WagerConsumer) Check(ctx context.Context) error {
	_, err := c.client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(c.queueURL),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameApproximateNumberOfMessages},
	})
	return err
}

// toInboundMessage reads the envelope and its operation. Anything it refuses is
// a permanent failure: the same message will never parse.
func toInboundMessage(message types.Message) (usecase.InboundMessage, error) {
	var body wagerMessage
	if err := json.Unmarshal([]byte(aws.ToString(message.Body)), &body); err != nil {
		return usecase.InboundMessage{}, domain.ValidationError(domain.FailureCodeInvalidInput,
			"malformed message body: %v", err)
	}
	if body.Type != wagerMessageType {
		return usecase.InboundMessage{}, domain.ValidationError(domain.FailureCodeInvalidInput,
			"message type %q is not %s", body.Type, wagerMessageType)
	}
	if body.MessageID == "" {
		return usecase.InboundMessage{}, domain.ValidationError(domain.FailureCodeInvalidInput,
			"messageId is required")
	}

	playerID, err := domain.ParseID(body.Data.PlayerID)
	if err != nil {
		return usecase.InboundMessage{}, err
	}
	walletID, err := domain.ParseID(body.Data.WalletID)
	if err != nil {
		return usecase.InboundMessage{}, err
	}
	kind, err := wager.ParseExternalKind(body.Data.Kind)
	if err != nil {
		return usecase.InboundMessage{}, err
	}
	amount, err := money.Parse(body.Data.Money.Amount, body.Data.Money.Currency)
	if err != nil {
		return usecase.InboundMessage{}, err
	}

	return usecase.InboundMessage{
		MessageID: body.MessageID,
		Input: usecase.ProcessWagerInput{
			ProviderID:                     body.Data.ProviderID,
			ExternalTransactionID:          body.Data.ExternalTransactionID,
			IdempotencyKey:                 body.Data.IdempotencyKey,
			PlayerID:                       playerID,
			WalletID:                       walletID,
			RoundID:                        body.Data.RoundID,
			GameID:                         body.Data.GameID,
			Kind:                           kind,
			Money:                          amount,
			ReferenceExternalTransactionID: body.Data.ReferenceExternalTransactionID,
			CorrelationID:                  body.MessageID,
		},
	}, nil
}
