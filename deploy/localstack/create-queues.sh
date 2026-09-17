#!/bin/sh
# Provisions the queues of betledger. LocalStack runs it once it is ready, both
# in Docker Compose and in the integration tests.
set -eu

awslocal sqs create-queue \
    --queue-name wallet-events.fifo \
    --attributes FifoQueue=true,ContentBasedDeduplication=false
