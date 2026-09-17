-- +goose Up
ALTER TABLE outbox_events
    ADD COLUMN sequence BIGSERIAL NOT NULL;

CREATE UNIQUE INDEX outbox_events_sequence ON outbox_events (sequence);

DROP INDEX outbox_events_pending;

CREATE INDEX outbox_events_pending
    ON outbox_events (aggregate_id, sequence)
    WHERE published_at IS NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION forbid_outbox_snapshot_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.aggregate_id IS DISTINCT FROM OLD.aggregate_id
        OR NEW.event_type IS DISTINCT FROM OLD.event_type
        OR NEW.payload::text IS DISTINCT FROM OLD.payload::text
        OR NEW.occurred_at IS DISTINCT FROM OLD.occurred_at
        OR NEW.sequence IS DISTINCT FROM OLD.sequence THEN
        RAISE EXCEPTION 'outbox_events snapshot is immutable'
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION forbid_outbox_snapshot_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.aggregate_id IS DISTINCT FROM OLD.aggregate_id
        OR NEW.event_type IS DISTINCT FROM OLD.event_type
        OR NEW.payload::text IS DISTINCT FROM OLD.payload::text
        OR NEW.occurred_at IS DISTINCT FROM OLD.occurred_at THEN
        RAISE EXCEPTION 'outbox_events snapshot is immutable'
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP INDEX outbox_events_pending;

CREATE INDEX outbox_events_pending
    ON outbox_events (next_attempt_at)
    WHERE published_at IS NULL;

DROP INDEX outbox_events_sequence;

ALTER TABLE outbox_events
    DROP COLUMN sequence;
