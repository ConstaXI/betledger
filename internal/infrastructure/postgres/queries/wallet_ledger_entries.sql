-- name: InsertLedgerEntry :exec
INSERT INTO wallet_ledger_entries (
    id,
    wallet_id,
    transaction_id,
    direction,
    currency,
    amount_minor,
    balance_before_minor,
    balance_after_minor,
    created_at
) VALUES (
    @id,
    @wallet_id,
    @transaction_id,
    @direction,
    @currency,
    @amount_minor,
    @balance_before_minor,
    @balance_after_minor,
    @created_at
);

-- name: SelectLedgerPage :many
SELECT id, wallet_id, transaction_id, direction, currency, amount_minor,
    balance_before_minor, balance_after_minor, created_at
FROM wallet_ledger_entries
WHERE wallet_id = @wallet_id
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) > (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_entry_id)::uuid)
  )
ORDER BY created_at, id
LIMIT sqlc.arg(page_size)::int;
