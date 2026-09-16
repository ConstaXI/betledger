-- +goose Up
CREATE TABLE outbox_events (
    id              UUID        PRIMARY KEY,
    aggregate_id    UUID        NOT NULL,
    event_type      TEXT        NOT NULL,
    payload         JSON        NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL,
    attempts        INTEGER     NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT outbox_events_attempts_non_negative CHECK (attempts >= 0)
);

CREATE INDEX outbox_events_pending
    ON outbox_events (next_attempt_at)
    WHERE published_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION forbid_outbox_snapshot_change() RETURNS trigger
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

CREATE TRIGGER outbox_events_immutable_snapshot
    BEFORE UPDATE ON outbox_events
    FOR EACH ROW EXECUTE FUNCTION forbid_outbox_snapshot_change();

-- +goose Down
DROP TABLE outbox_events;
DROP FUNCTION forbid_outbox_snapshot_change();
