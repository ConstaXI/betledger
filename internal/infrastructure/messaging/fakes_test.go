package messaging

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/davibanfi/betledger/internal/usecase"
)

var errBrokerDown = errors.New("fake: broker unreachable")

// fakeQueue records what the publisher and the consumer ask of SQS, and fails
// the calls named in its errors.
type fakeQueue struct {
	received   []types.Message
	sent       []*sqs.SendMessageInput
	deleted    []string
	sendErr    error
	receiveErr error
}

func (f *fakeQueue) GetQueueUrl(
	_ context.Context,
	input *sqs.GetQueueUrlInput,
	_ ...func(*sqs.Options),
) (*sqs.GetQueueUrlOutput, error) {
	return &sqs.GetQueueUrlOutput{QueueUrl: aws.String("http://queues/" + aws.ToString(input.QueueName))}, nil
}

func (f *fakeQueue) GetQueueAttributes(
	context.Context,
	*sqs.GetQueueAttributesInput,
	...func(*sqs.Options),
) (*sqs.GetQueueAttributesOutput, error) {
	return &sqs.GetQueueAttributesOutput{}, nil
}

func (f *fakeQueue) SendMessage(
	_ context.Context,
	input *sqs.SendMessageInput,
	_ ...func(*sqs.Options),
) (*sqs.SendMessageOutput, error) {
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	f.sent = append(f.sent, input)
	return &sqs.SendMessageOutput{}, nil
}

func (f *fakeQueue) ReceiveMessage(
	context.Context,
	*sqs.ReceiveMessageInput,
	...func(*sqs.Options),
) (*sqs.ReceiveMessageOutput, error) {
	if f.receiveErr != nil {
		return nil, f.receiveErr
	}
	messages := f.received
	f.received = nil
	return &sqs.ReceiveMessageOutput{Messages: messages}, nil
}

func (f *fakeQueue) DeleteMessage(
	_ context.Context,
	input *sqs.DeleteMessageInput,
	_ ...func(*sqs.Options),
) (*sqs.DeleteMessageOutput, error) {
	f.deleted = append(f.deleted, aws.ToString(input.ReceiptHandle))
	return &sqs.DeleteMessageOutput{}, nil
}

// fakeProcessor stands for the use case that applies a received operation.
type fakeProcessor struct {
	result   usecase.InboundResult
	err      error
	received []usecase.InboundMessage
}

func (f *fakeProcessor) Execute(
	_ context.Context,
	message usecase.InboundMessage,
) (usecase.InboundResult, error) {
	f.received = append(f.received, message)
	return f.result, f.err
}

// fakeDeadLetterRecorder counts the messages reported as dead lettered, which
// the table states through the dead letters it expects in the queue.
type fakeDeadLetterRecorder struct{ calls int }

func (f *fakeDeadLetterRecorder) MessageDeadLettered(context.Context) { f.calls++ }
