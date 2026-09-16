-- name: InsertWallet :exec
INSERT INTO wallets (id, player_id, currency, balance_minor, version)
VALUES (@id, @player_id, @currency, @balance_minor, @version);

-- name: SelectWalletForUpdate :one
SELECT id, player_id, currency, balance_minor, version
FROM wallets
WHERE id = @id
FOR UPDATE;

-- name: UpdateWalletBalance :execrows
UPDATE wallets
SET balance_minor = @balance_minor,
    version = @version,
    updated_at = now()
WHERE id = @id
  AND version = @expected_version;
