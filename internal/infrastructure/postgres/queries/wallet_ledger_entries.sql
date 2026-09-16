-- name: InsertLedgerEntry :exec
INSERT INTO wallet_ledger_entries (
    id,
    wallet_id,
    transaction_id,
    direction,
    currency,
    amount_minor,
    balance_before_minor,
    balance_after_minor
) VALUES (
    @id,
    @wallet_id,
    @transaction_id,
    @direction,
    @currency,
    @amount_minor,
    @balance_before_minor,
    @balance_after_minor
);
