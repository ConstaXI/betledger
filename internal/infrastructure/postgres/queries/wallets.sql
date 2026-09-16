-- name: InsertWallet :exec
INSERT INTO wallets (id, player_id, currency, balance_minor, version)
VALUES (@id, @player_id, @currency, @balance_minor, @version);
