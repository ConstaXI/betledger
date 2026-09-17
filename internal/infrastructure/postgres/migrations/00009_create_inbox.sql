-- +goose Up
CREATE TABLE inbox_messages (
    message_id     TEXT        PRIMARY KEY,
    payload_hash   TEXT        NOT NULL,
    transaction_id UUID        NOT NULL REFERENCES wager_transactions (id),
    received_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE inbox_messages;
