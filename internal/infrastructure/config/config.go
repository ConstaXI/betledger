// Package config loads the application configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the validated application configuration.
type Config struct {
	HTTPPort    string
	DatabaseURL string
	// OIDCIssuerURL is the issuer every accepted token must carry; its
	// discovery document provides the signing keys.
	OIDCIssuerURL string
	// OIDCDiscoveryURL is where the discovery document is fetched when the
	// provider is reached through an address other than the issuer, as inside
	// the Compose network; it defaults to the issuer.
	OIDCDiscoveryURL string
	// OIDCAudience must appear in the aud claim of every accepted token.
	OIDCAudience string
	// ReferenceMaxAttempts is how many attempts may find no concluded reference
	// before the operation is rejected with REFERENCE_NOT_FOUND.
	ReferenceMaxAttempts int
	// ReferenceRetryBaseDelay is the wait after the first unsuccessful attempt,
	// doubled after each further one.
	ReferenceRetryBaseDelay time.Duration
	// ReferenceRetryMaxDelay caps the wait between two attempts.
	ReferenceRetryMaxDelay time.Duration
	// ReferenceRetryLease is how long a worker holds the operations it took
	// before another worker may take them.
	ReferenceRetryLease time.Duration
	// ReferencePollInterval is how often an idle worker looks for due
	// operations.
	ReferencePollInterval time.Duration
	AWSRegion             string
	// AWSEndpointURL overrides the AWS endpoint, pointing at LocalStack locally;
	// empty uses the regular AWS endpoints.
	AWSEndpointURL string
	// EventsQueueName is the FIFO queue that receives the published events.
	EventsQueueName string
	// OutboxPollInterval is how often an idle publisher looks for due events.
	OutboxPollInterval time.Duration
	// OutboxRetryBaseDelay is the wait after the first failed publication of an
	// event, doubled after each further failure.
	OutboxRetryBaseDelay time.Duration
	// OutboxRetryMaxDelay caps the wait between two publications of an event.
	OutboxRetryMaxDelay time.Duration
	// OutboxLease is how long a publisher holds the events it took before
	// another publisher may take them.
	OutboxLease time.Duration
	// WagerQueueName is the FIFO queue the providers send operations to.
	WagerQueueName string
	// WagerDeadLetterQueueName receives the messages that cannot be handled.
	WagerDeadLetterQueueName string
	// ConsumerWaitTime is how long a receive waits for messages before coming
	// back empty, which is the long polling of SQS.
	ConsumerWaitTime time.Duration
	// ConsumerVisibilityTimeout is how long a taken message stays hidden from
	// other consumers; it must cover the handling of a message.
	ConsumerVisibilityTimeout time.Duration
	// ConsumerBatchSize is how many messages a receive takes at most.
	ConsumerBatchSize int
	// ConsumerPollInterval is how long the consumer waits after an empty
	// receive before asking again.
	ConsumerPollInterval time.Duration
}

// Load reads the configuration from the environment and fails when a required
// value is missing or invalid.
func Load() (Config, error) {
	var errs []error
	cfg := Config{
		HTTPPort:                envOrDefault("HTTP_PORT", "8080"),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		OIDCIssuerURL:           os.Getenv("OIDC_ISSUER_URL"),
		OIDCDiscoveryURL:        envOrDefault("OIDC_DISCOVERY_URL", os.Getenv("OIDC_ISSUER_URL")),
		OIDCAudience:            envOrDefault("OIDC_AUDIENCE", "betledger-api"),
		ReferenceMaxAttempts:    intOrDefault("REFERENCE_MAX_ATTEMPTS", 8, &errs),
		ReferenceRetryBaseDelay: durationOrDefault("REFERENCE_RETRY_BASE_DELAY", time.Second, &errs),
		ReferenceRetryMaxDelay:  durationOrDefault("REFERENCE_RETRY_MAX_DELAY", 5*time.Minute, &errs),
		ReferenceRetryLease:     durationOrDefault("REFERENCE_RETRY_LEASE", 30*time.Second, &errs),
		ReferencePollInterval:   durationOrDefault("REFERENCE_POLL_INTERVAL", time.Second, &errs),
		AWSRegion:               envOrDefault("AWS_REGION", "us-east-1"),
		AWSEndpointURL:          os.Getenv("AWS_ENDPOINT_URL"),
		EventsQueueName:         envOrDefault("EVENTS_QUEUE_NAME", "wallet-events.fifo"),
		OutboxPollInterval:      durationOrDefault("OUTBOX_POLL_INTERVAL", 500*time.Millisecond, &errs),
		OutboxRetryBaseDelay:    durationOrDefault("OUTBOX_RETRY_BASE_DELAY", time.Second, &errs),
		OutboxRetryMaxDelay:     durationOrDefault("OUTBOX_RETRY_MAX_DELAY", time.Minute, &errs),
		OutboxLease:             durationOrDefault("OUTBOX_LEASE", 30*time.Second, &errs),

		WagerQueueName:            envOrDefault("WAGER_QUEUE_NAME", "wager-transactions.fifo"),
		WagerDeadLetterQueueName:  envOrDefault("WAGER_DLQ_NAME", "wager-transactions-dlq.fifo"),
		ConsumerWaitTime:          durationOrDefault("CONSUMER_WAIT_TIME", 20*time.Second, &errs),
		ConsumerVisibilityTimeout: durationOrDefault("CONSUMER_VISIBILITY_TIMEOUT", 30*time.Second, &errs),
		ConsumerBatchSize:         intOrDefault("CONSUMER_BATCH_SIZE", 10, &errs),
		ConsumerPollInterval:      durationOrDefault("CONSUMER_POLL_INTERVAL", time.Second, &errs),
	}
	errs = append(errs, cfg.validate())
	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	var errs []error
	if c.HTTPPort == "" {
		errs = append(errs, errors.New("HTTP_PORT cannot be empty"))
	}
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.OIDCIssuerURL == "" {
		errs = append(errs, errors.New("OIDC_ISSUER_URL is required"))
	}
	if c.OIDCAudience == "" {
		errs = append(errs, errors.New("OIDC_AUDIENCE cannot be empty"))
	}
	if c.ReferenceMaxAttempts < 1 {
		errs = append(errs, errors.New("REFERENCE_MAX_ATTEMPTS must be at least 1"))
	}
	if c.ReferenceRetryBaseDelay <= 0 || c.ReferenceRetryMaxDelay < c.ReferenceRetryBaseDelay {
		errs = append(errs, errors.New("REFERENCE_RETRY_BASE_DELAY must be positive and at most REFERENCE_RETRY_MAX_DELAY"))
	}
	if c.ReferenceRetryLease <= 0 || c.ReferencePollInterval <= 0 {
		errs = append(errs, errors.New("REFERENCE_RETRY_LEASE and REFERENCE_POLL_INTERVAL must be positive"))
	}
	if c.EventsQueueName == "" {
		errs = append(errs, errors.New("EVENTS_QUEUE_NAME cannot be empty"))
	}
	if c.OutboxRetryBaseDelay <= 0 || c.OutboxRetryMaxDelay < c.OutboxRetryBaseDelay {
		errs = append(errs, errors.New("OUTBOX_RETRY_BASE_DELAY must be positive and at most OUTBOX_RETRY_MAX_DELAY"))
	}
	if c.OutboxLease <= 0 || c.OutboxPollInterval <= 0 {
		errs = append(errs, errors.New("OUTBOX_LEASE and OUTBOX_POLL_INTERVAL must be positive"))
	}
	if c.WagerQueueName == "" || c.WagerDeadLetterQueueName == "" {
		errs = append(errs, errors.New("WAGER_QUEUE_NAME and WAGER_DLQ_NAME cannot be empty"))
	}
	if c.ConsumerWaitTime < 0 || c.ConsumerWaitTime > 20*time.Second {
		errs = append(errs, errors.New("CONSUMER_WAIT_TIME must be between 0s and 20s"))
	}
	if c.ConsumerVisibilityTimeout <= 0 || c.ConsumerPollInterval <= 0 {
		errs = append(errs, errors.New("CONSUMER_VISIBILITY_TIMEOUT and CONSUMER_POLL_INTERVAL must be positive"))
	}
	if c.ConsumerBatchSize < 1 || c.ConsumerBatchSize > 10 {
		errs = append(errs, errors.New("CONSUMER_BATCH_SIZE must be between 1 and 10"))
	}
	return errors.Join(errs...)
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func intOrDefault(key string, fallback int, errs *[]error) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be an integer, got %q", key, value))
	}
	return parsed
}

func durationOrDefault(key string, fallback time.Duration, errs *[]error) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s must be a duration such as 1s or 5m, got %q", key, value))
	}
	return parsed
}
