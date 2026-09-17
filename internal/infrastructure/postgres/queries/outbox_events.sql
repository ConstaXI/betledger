-- name: InsertOutboxEvent :exec
INSERT INTO outbox_events (id, aggregate_id, event_type, payload, occurred_at)
VALUES (@id, @aggregate_id, @event_type, @payload, @occurred_at);

-- name: LeasePendingOutboxEvents :many
UPDATE outbox_events
SET next_attempt_at = sqlc.arg(lease_until)::timestamptz
WHERE id IN (
    SELECT head.id
    FROM outbox_events AS head
    WHERE head.published_at IS NULL
      AND head.next_attempt_at <= sqlc.arg(due_at)::timestamptz
      AND NOT EXISTS (
          SELECT 1
          FROM outbox_events AS earlier
          WHERE earlier.aggregate_id = head.aggregate_id
            AND earlier.published_at IS NULL
            AND earlier.sequence < head.sequence
      )
    ORDER BY head.sequence
    LIMIT sqlc.arg(batch_size)::int
    FOR UPDATE SKIP LOCKED
)
RETURNING id, aggregate_id, event_type, payload, attempts;

-- name: MarkOutboxEventPublished :execrows
UPDATE outbox_events
SET published_at = sqlc.arg(published_at)::timestamptz
WHERE id = @id
  AND published_at IS NULL;

-- name: RescheduleOutboxEvent :execrows
UPDATE outbox_events
SET attempts = @attempts,
    next_attempt_at = sqlc.arg(next_attempt_at)::timestamptz
WHERE id = @id
  AND published_at IS NULL;
