-- +goose Up
CREATE UNIQUE INDEX wager_transactions_provider_idempotency_key
    ON wager_transactions (provider_id, idempotency_key)
    WHERE kind <> 'OPENING';

-- +goose Down
DROP INDEX wager_transactions_provider_idempotency_key;
