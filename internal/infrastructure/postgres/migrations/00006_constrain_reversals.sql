-- +goose Up
ALTER TABLE wager_transactions
    ADD CONSTRAINT wager_transactions_processed_reversal_resolved CHECK (
        kind NOT IN ('REFUND', 'ROLLBACK')
        OR state <> 'PROCESSED'
        OR reference_transaction_id IS NOT NULL
    );

CREATE UNIQUE INDEX wager_transactions_one_reversal_per_reference
    ON wager_transactions (reference_transaction_id)
    WHERE kind IN ('REFUND', 'ROLLBACK') AND state = 'PROCESSED';

-- +goose Down
DROP INDEX wager_transactions_one_reversal_per_reference;

ALTER TABLE wager_transactions
    DROP CONSTRAINT wager_transactions_processed_reversal_resolved;
