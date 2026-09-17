-- +goose Up
ALTER TABLE wager_transactions
    ADD COLUMN reference_attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN next_attempt_at TIMESTAMPTZ,
    ADD CONSTRAINT wager_transactions_reference_attempts_non_negative CHECK (reference_attempts >= 0),
    ADD CONSTRAINT wager_transactions_pending_reference_scheduled CHECK (
        state <> 'PENDING_REFERENCE' OR next_attempt_at IS NOT NULL
    );

CREATE INDEX wager_transactions_pending_references
    ON wager_transactions (next_attempt_at)
    WHERE state = 'PENDING_REFERENCE';

-- +goose Down
DROP INDEX wager_transactions_pending_references;

ALTER TABLE wager_transactions
    DROP CONSTRAINT wager_transactions_pending_reference_scheduled,
    DROP CONSTRAINT wager_transactions_reference_attempts_non_negative,
    DROP COLUMN next_attempt_at,
    DROP COLUMN reference_attempts;
