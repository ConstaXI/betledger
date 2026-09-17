//go:build integration

package testenv

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/davibanfi/betledger/internal/infrastructure/config"
	"github.com/davibanfi/betledger/internal/infrastructure/messaging"
	"github.com/davibanfi/betledger/internal/usecase"
)

// EventsQueueName is the events queue the provisioning script creates, the one
// applications started by the tests publish to.
const EventsQueueName = "wallet-events.fifo"

// LocalStack is a disposable LocalStack container provisioned by the same
// script Docker Compose runs.
type LocalStack struct {
	// EndpointURL reaches the AWS APIs emulated by the container.
	EndpointURL string
	// Client calls SQS on the container.
	Client    *sqs.Client
	container *testcontainers.DockerContainer
}

// StartLocalStack starts the container and waits until the provisioning script
// has run. The AWS credentials and region must already be in the environment.
// The caller must call Terminate when done.
func StartLocalStack(ctx context.Context) (*LocalStack, error) {
	_, source, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(source), "..", "..", "deploy", "localstack", "create-queues.sh")

	container, err := testcontainers.Run(ctx, "localstack/localstack:4.14",
		testcontainers.WithEnv(map[string]string{"SERVICES": "sqs"}),
		testcontainers.WithExposedPorts("4566/tcp"),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			HostFilePath:      script,
			ContainerFilePath: "/etc/localstack/init/ready.d/create-queues.sh",
			FileMode:          0o755,
		}),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/_localstack/init/ready").
				WithPort("4566/tcp").
				WithResponseMatcher(func(body io.Reader) bool {
					content, err := io.ReadAll(body)
					return err == nil && strings.Contains(string(content), `"completed": true`)
				}).
				WithStartupTimeout(3*time.Minute),
		),
	)
	broker := &LocalStack{container: container}
	if err != nil {
		return broker, err
	}

	broker.EndpointURL, err = container.PortEndpoint(ctx, "4566/tcp", "http")
	if err != nil {
		return broker, err
	}
	broker.Client, err = messaging.NewClient(config.Config{AWSRegion: "us-east-1", AWSEndpointURL: broker.EndpointURL})
	return broker, err
}

// Terminate removes the container.
func (l *LocalStack) Terminate() {
	if l == nil || l.container == nil {
		return
	}
	_ = testcontainers.TerminateContainer(l.container)
}

// CreateEventsQueue creates a FIFO queue of the test's own, configured like the
// events queue, and returns its name and URL.
func (l *LocalStack) CreateEventsQueue(t *testing.T) (name, url string) {
	t.Helper()

	name = "events-" + uuid.NewString() + ".fifo"
	output, err := l.Client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String(name),
		Attributes: map[string]string{
			string(types.QueueAttributeNameFifoQueue):                 "true",
			string(types.QueueAttributeNameContentBasedDeduplication): "false",
		},
	})
	require.NoError(t, err)
	return name, aws.ToString(output.QueueUrl)
}

// ReceivedEvent is an event read from a queue.
type ReceivedEvent struct {
	EventID         string
	EventType       string
	AggregateID     string
	GroupID         string
	DeduplicationID string
}

// ReceivedEvents is a list of events read from a queue.
type ReceivedEvents []ReceivedEvent

// IDs returns the event ids, in the order received.
func (events ReceivedEvents) IDs() []string {
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.EventID)
	}
	return ids
}

// ReceiveEvents reads and deletes messages from the queue until it has at
// least want of them or the timeout runs out, and returns them in the order
// received.
func (l *LocalStack) ReceiveEvents(t *testing.T, queueURL string, want int, timeout time.Duration) ReceivedEvents {
	t.Helper()

	ctx := context.Background()
	deadline := time.Now().Add(timeout)
	var received ReceivedEvents
	for len(received) < want || want == 0 {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		output, err := l.Client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:                    aws.String(queueURL),
			MaxNumberOfMessages:         10,
			WaitTimeSeconds:             int32(min(remaining, time.Second) / time.Second),
			MessageSystemAttributeNames: []types.MessageSystemAttributeName{types.MessageSystemAttributeNameAll},
		})
		require.NoError(t, err)
		for _, message := range output.Messages {
			var body struct {
				EventID     string `json:"eventId"`
				EventType   string `json:"eventType"`
				AggregateID string `json:"aggregateId"`
			}
			require.NoError(t, json.Unmarshal([]byte(aws.ToString(message.Body)), &body))
			received = append(received, ReceivedEvent{
				EventID:         body.EventID,
				EventType:       body.EventType,
				AggregateID:     body.AggregateID,
				GroupID:         message.Attributes[string(types.MessageSystemAttributeNameMessageGroupId)],
				DeduplicationID: message.Attributes[string(types.MessageSystemAttributeNameMessageDeduplicationId)],
			})
			_, err := l.Client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(queueURL),
				ReceiptHandle: message.ReceiptHandle,
			})
			require.NoError(t, err)
		}
	}
	return received
}

// WagerQueues is a pair of FIFO queues of a test's own, configured like the
// wagering queue and its dead letter queue, with the same redrive policy.
type WagerQueues struct {
	Name          string
	URL           string
	DeadLetter    string
	DeadLetterURL string
}

// Env is the configuration an application needs to consume from these queues,
// with short waits so that a test does not linger.
func (q WagerQueues) Env() []func(env map[string]string) {
	return []func(env map[string]string){
		WithEnv("WAGER_QUEUE_NAME", q.Name),
		WithEnv("WAGER_DLQ_NAME", q.DeadLetter),
		WithEnv("CONSUMER_WAIT_TIME", "1s"),
		WithEnv("CONSUMER_VISIBILITY_TIMEOUT", "2s"),
		WithEnv("CONSUMER_POLL_INTERVAL", "100ms"),
	}
}

// CreateWagerQueues creates the wagering queue and its dead letter queue.
func (l *LocalStack) CreateWagerQueues(t *testing.T) WagerQueues {
	t.Helper()

	ctx := context.Background()
	queues := WagerQueues{
		Name:       "wager-" + uuid.NewString() + ".fifo",
		DeadLetter: "wager-dlq-" + uuid.NewString() + ".fifo",
	}
	deadLetter, err := l.Client.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName:  aws.String(queues.DeadLetter),
		Attributes: map[string]string{string(types.QueueAttributeNameFifoQueue): "true"},
	})
	require.NoError(t, err)
	queues.DeadLetterURL = aws.ToString(deadLetter.QueueUrl)

	attributes, err := l.Client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(queues.DeadLetterURL),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	})
	require.NoError(t, err)
	redrive, err := json.Marshal(map[string]string{
		"deadLetterTargetArn": attributes.Attributes[string(types.QueueAttributeNameQueueArn)],
		"maxReceiveCount":     "5",
	})
	require.NoError(t, err)

	queue, err := l.Client.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String(queues.Name),
		Attributes: map[string]string{
			string(types.QueueAttributeNameFifoQueue):         "true",
			string(types.QueueAttributeNameVisibilityTimeout): "2",
			string(types.QueueAttributeNameRedrivePolicy):     string(redrive),
		},
	})
	require.NoError(t, err)
	queues.URL = aws.ToString(queue.QueueUrl)
	return queues
}

// SendWagerMessage sends an operation to the queue in the envelope the
// providers use, and returns the message identifier.
func (l *LocalStack) SendWagerMessage(
	t *testing.T,
	queueURL, messageID string,
	input usecase.ProcessWagerInput,
) string {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"messageId":  messageID,
		"type":       "WagerTransactionRequested",
		"occurredAt": time.Now().UTC(),
		"data": map[string]any{
			"providerId":                     input.ProviderID,
			"externalTransactionId":          input.ExternalTransactionID,
			"idempotencyKey":                 input.IdempotencyKey,
			"playerId":                       input.PlayerID,
			"walletId":                       input.WalletID,
			"roundId":                        input.RoundID,
			"gameId":                         input.GameID,
			"kind":                           input.Kind,
			"money":                          input.Money,
			"referenceExternalTransactionId": input.ReferenceExternalTransactionID,
		},
	})
	require.NoError(t, err)
	l.SendRawMessage(t, queueURL, input.WalletID.String(), body)
	return messageID
}

// SendRawMessage sends the body as it is, for the messages a provider should
// never send.
func (l *LocalStack) SendRawMessage(t *testing.T, queueURL, groupID string, body []byte) {
	t.Helper()

	_, err := l.Client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:               aws.String(queueURL),
		MessageBody:            aws.String(string(body)),
		MessageGroupId:         aws.String(groupID),
		MessageDeduplicationId: aws.String(uuid.NewString()),
	})
	require.NoError(t, err)
}

// ReceiveBodies reads and deletes messages until it has at least want of them
// or the timeout runs out, and returns their bodies.
func (l *LocalStack) ReceiveBodies(t *testing.T, queueURL string, want int, timeout time.Duration) []string {
	t.Helper()

	ctx := context.Background()
	deadline := time.Now().Add(timeout)
	var bodies []string
	for len(bodies) < want || want == 0 {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		output, err := l.Client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     int32(min(remaining, time.Second) / time.Second),
		})
		require.NoError(t, err)
		for _, message := range output.Messages {
			bodies = append(bodies, aws.ToString(message.Body))
			_, err := l.Client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(queueURL),
				ReceiptHandle: message.ReceiptHandle,
			})
			require.NoError(t, err)
		}
	}
	return bodies
}
