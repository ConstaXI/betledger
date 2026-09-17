-- name: InsertInboxMessage :exec
INSERT INTO inbox_messages (message_id, payload_hash, transaction_id)
VALUES (@message_id, @payload_hash, @transaction_id);

-- name: SelectInboxMessage :one
SELECT message_id, payload_hash, transaction_id
FROM inbox_messages
WHERE message_id = @message_id;
