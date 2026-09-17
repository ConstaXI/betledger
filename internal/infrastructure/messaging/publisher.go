// Package messaging connects the application to Amazon SQS.
package messaging

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"go.uber.org/fx"

	"github.com/davibanfi/betledger/internal/infrastructure/config"
	"github.com/davibanfi/betledger/internal/usecase"
)

// NewClient builds the SQS client. Credentials come from the standard AWS
// chain; the endpoint is overridden when AWS_ENDPOINT_URL points at LocalStack.
func NewClient(cfg config.Config) (*sqs.Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(cfg.AWSRegion))
	if err != nil {
		return nil, fmt.Errorf("failed to load the AWS configuration: %w", err)
	}
	return sqs.NewFromConfig(awsCfg, func(options *sqs.Options) {
		if cfg.AWSEndpointURL != "" {
			options.BaseEndpoint = aws.String(cfg.AWSEndpointURL)
		}
	}), nil
}

// queueAPI is the part of the SQS client the publisher and the consumer use.
type queueAPI interface {
	GetQueueUrl(context.Context, *sqs.GetQueueUrlInput, ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error)
	GetQueueAttributes(
		context.Context,
		*sqs.GetQueueAttributesInput,
		...func(*sqs.Options),
	) (*sqs.GetQueueAttributesOutput, error)
	SendMessage(context.Context, *sqs.SendMessageInput, ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
	ReceiveMessage(
		context.Context,
		*sqs.ReceiveMessageInput,
		...func(*sqs.Options),
	) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

// EventPublisher publishes outbox events to the events FIFO queue. The message
// body is the event snapshot as recorded; the group is the aggregate, so events
// of a wallet are consumed in order, and the deduplication id is the event id,
// so a republication within the deduplication window is dropped by the queue.
type EventPublisher struct {
	client    queueAPI
	queueName string
	queueURL  string
}

// NewEventPublisher builds the publisher. The queue URL is resolved on start,
// so that a missing queue or an unreachable broker prevents the application
// from starting.
func NewEventPublisher(lc fx.Lifecycle, client *sqs.Client, cfg config.Config) *EventPublisher {
	publisher := &EventPublisher{client: client, queueName: cfg.EventsQueueName}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			output, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(cfg.EventsQueueName)})
			if err != nil {
				return fmt.Errorf("failed to resolve the events queue %s: %w", cfg.EventsQueueName, err)
			}
			publisher.queueURL = aws.ToString(output.QueueUrl)
			return nil
		},
	})
	return publisher
}

// Check reports whether the events queue can be reached.
func (p *EventPublisher) Check(ctx context.Context) error {
	_, err := p.client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(p.queueURL),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameApproximateNumberOfMessages},
	})
	return err
}

// Publish sends the event to the queue.
func (p *EventPublisher) Publish(ctx context.Context, record usecase.OutboxRecord) error {
	_, err := p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(p.queueURL),
		MessageBody:            aws.String(string(record.Payload)),
		MessageGroupId:         aws.String(record.AggregateID.String()),
		MessageDeduplicationId: aws.String(record.EventID.String()),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"eventType": {DataType: aws.String("String"), StringValue: aws.String(record.EventType)},
			"eventId":   {DataType: aws.String("String"), StringValue: aws.String(record.EventID.String())},
		},
	})
	if err != nil {
		return fmt.Errorf("%w: failed to publish event %s: %w", usecase.ErrUnavailable, record.EventID, err)
	}
	return nil
}
