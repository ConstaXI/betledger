#!/bin/sh
# Provisions the queues of betledger. LocalStack runs it once it is ready, both
# in Docker Compose and in the integration tests.
set -eu

awslocal sqs create-queue \
    --queue-name wallet-events.fifo \
    --attributes FifoQueue=true,ContentBasedDeduplication=false

awslocal sqs create-queue \
    --queue-name wager-transactions-dlq.fifo \
    --attributes FifoQueue=true,ContentBasedDeduplication=false

dead_letter_arn=$(awslocal sqs get-queue-attributes \
    --queue-url "$(awslocal sqs get-queue-url --queue-name wager-transactions-dlq.fifo --output text)" \
    --attribute-names QueueArn --output text --query 'Attributes.QueueArn')

# Messages the consumer cannot delete, because handling them keeps failing, are
# moved to the dead letter queue after maxReceiveCount deliveries.
awslocal sqs create-queue \
    --queue-name wager-transactions.fifo \
    --attributes "{\"FifoQueue\":\"true\",\"ContentBasedDeduplication\":\"false\",\"VisibilityTimeout\":\"30\",\"RedrivePolicy\":\"{\\\"deadLetterTargetArn\\\":\\\"${dead_letter_arn}\\\",\\\"maxReceiveCount\\\":\\\"5\\\"}\"}"
