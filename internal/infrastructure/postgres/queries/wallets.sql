-- name: InsertWallet :exec
INSERT INTO wallets (id, player_id, currency, balance_minor, version, created_at, updated_at)
VALUES (@id, @player_id, @currency, @balance_minor, @version, @created_at, @updated_at);

-- name: SelectWalletForUpdate :one
SELECT id, player_id, currency, balance_minor, version, created_at, updated_at
FROM wallets
WHERE id = @id
FOR UPDATE;

-- name: UpdateWalletBalance :execrows
UPDATE wallets
SET balance_minor = @balance_minor,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @expected_version;

-- name: SelectWallet :one
SELECT id, player_id, currency, balance_minor, version, created_at, updated_at
FROM wallets
WHERE id = @id;

-- name: SelectWalletReconciliation :one
SELECT w.currency,
    w.balance_minor,
    COALESCE(SUM(CASE l.direction WHEN 'CREDIT' THEN l.amount_minor ELSE -l.amount_minor END), 0)::bigint
        AS rebuilt_minor,
    count(l.id)::bigint AS checked_entries
FROM wallets AS w
LEFT JOIN wallet_ledger_entries AS l ON l.wallet_id = w.id
WHERE w.id = @id
GROUP BY w.currency, w.balance_minor;
