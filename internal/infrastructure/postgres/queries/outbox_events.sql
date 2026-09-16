-- name: InsertOutboxEvent :exec
INSERT INTO outbox_events (id, aggregate_id, event_type, payload, occurred_at)
VALUES (@id, @aggregate_id, @event_type, @payload, @occurred_at);
