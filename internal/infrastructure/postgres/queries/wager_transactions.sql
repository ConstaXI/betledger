-- name: InsertWagerTransaction :exec
INSERT INTO wager_transactions (
    id,
    kind,
    state,
    wallet_id,
    player_id,
    currency,
    amount_minor,
    provider_id,
    external_transaction_id,
    idempotency_key,
    payload_hash,
    round_id,
    game_id,
    reference_external_transaction_id,
    reference_transaction_id,
    failure_code,
    result_balance_minor,
    created_at,
    updated_at,
    next_attempt_at
) VALUES (
    @id,
    @kind,
    @state,
    @wallet_id,
    @player_id,
    @currency,
    @amount_minor,
    sqlc.narg(provider_id),
    sqlc.narg(external_transaction_id),
    sqlc.narg(idempotency_key),
    sqlc.narg(payload_hash),
    sqlc.narg(round_id),
    sqlc.narg(game_id),
    sqlc.narg(reference_external_transaction_id),
    sqlc.narg(reference_transaction_id),
    sqlc.narg(failure_code),
    sqlc.narg(result_balance_minor),
    @created_at,
    @updated_at,
    CASE WHEN @state::text = 'PENDING_REFERENCE' THEN now() END
);

-- name: SelectWagerTransactionByIdempotencyKey :one
SELECT *
FROM wager_transactions
WHERE provider_id = @provider_id
  AND idempotency_key = @idempotency_key;

-- name: SelectWagerTransactionByExternalID :one
SELECT *
FROM wager_transactions
WHERE provider_id = @provider_id
  AND external_transaction_id = @external_transaction_id;

-- name: ExistsProcessedReversal :one
SELECT EXISTS (
    SELECT 1
    FROM wager_transactions
    WHERE reference_transaction_id = @reference_transaction_id
      AND kind IN ('REFUND', 'ROLLBACK')
      AND state = 'PROCESSED'
);

-- name: SelectWagerTransactionByID :one
SELECT *
FROM wager_transactions
WHERE id = @id;

-- name: LeasePendingReferences :many
UPDATE wager_transactions
SET next_attempt_at = sqlc.arg(lease_until)::timestamptz
WHERE id IN (
    SELECT due.id
    FROM wager_transactions AS due
    WHERE due.state = 'PENDING_REFERENCE'
      AND due.next_attempt_at <= sqlc.arg(due_at)::timestamptz
    ORDER BY due.next_attempt_at
    LIMIT sqlc.arg(batch_size)::int
    FOR UPDATE SKIP LOCKED
)
RETURNING id, wallet_id;

-- name: UpdatePendingReference :execrows
UPDATE wager_transactions
SET state = @state,
    failure_code = sqlc.narg(failure_code),
    reference_transaction_id = sqlc.narg(reference_transaction_id),
    result_balance_minor = sqlc.narg(result_balance_minor),
    reference_attempts = @reference_attempts,
    next_attempt_at = sqlc.narg(next_attempt_at),
    updated_at = @updated_at
WHERE id = @id
  AND state = 'PENDING_REFERENCE';
