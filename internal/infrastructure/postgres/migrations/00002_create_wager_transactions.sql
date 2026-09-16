-- +goose Up
CREATE TABLE wager_transactions (
    id                                UUID        PRIMARY KEY,
    kind                              TEXT        NOT NULL,
    state                             TEXT        NOT NULL,
    wallet_id                         UUID        NOT NULL REFERENCES wallets (id),
    player_id                         UUID        NOT NULL,
    currency                          CHAR(3)     NOT NULL,
    amount_minor                      BIGINT      NOT NULL,
    provider_id                       TEXT,
    external_transaction_id           TEXT,
    idempotency_key                   TEXT,
    payload_hash                      TEXT,
    round_id                          TEXT,
    game_id                           TEXT,
    reference_external_transaction_id TEXT,
    reference_transaction_id          UUID        REFERENCES wager_transactions (id),
    failure_code                      TEXT,
    result_balance_minor              BIGINT,
    created_at                        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                        TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT wager_transactions_kind_known
        CHECK (kind IN ('OPENING', 'BET', 'WIN', 'LOSS', 'REFUND', 'ROLLBACK')),
    CONSTRAINT wager_transactions_state_known
        CHECK (state IN ('PENDING', 'PENDING_REFERENCE', 'PROCESSED', 'REJECTED', 'FAILED')),
    CONSTRAINT wager_transactions_amount_non_negative CHECK (amount_minor >= 0),
    CONSTRAINT wager_transactions_origin CHECK (
        (
            kind = 'OPENING'
            AND state = 'PROCESSED'
            AND provider_id IS NULL
            AND external_transaction_id IS NULL
            AND idempotency_key IS NULL
            AND payload_hash IS NULL
            AND round_id IS NULL
            AND game_id IS NULL
            AND reference_external_transaction_id IS NULL
        )
        OR (
            kind <> 'OPENING'
            AND provider_id IS NOT NULL
            AND external_transaction_id IS NOT NULL
            AND idempotency_key IS NOT NULL
            AND payload_hash IS NOT NULL
            AND round_id IS NOT NULL
            AND game_id IS NOT NULL
        )
    )
);

CREATE UNIQUE INDEX wager_transactions_one_opening_per_wallet
    ON wager_transactions (wallet_id)
    WHERE kind = 'OPENING';

CREATE UNIQUE INDEX wager_transactions_provider_external_id
    ON wager_transactions (provider_id, external_transaction_id)
    WHERE kind <> 'OPENING';

-- +goose Down
DROP TABLE wager_transactions;
