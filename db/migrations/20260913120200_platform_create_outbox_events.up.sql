CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,
    type TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    available_at TIMESTAMPTZ NOT NULL,
    processed_at TIMESTAMPTZ NULL,
    attempts INT NOT NULL DEFAULT 0,
    last_error TEXT NULL,

    -- Set once attempts reaches the configured ceiling: a terminal,
    -- permanent give-up. Distinct from processed_at (success) and from
    -- merely "not yet due" (available_at in the future). An event with
    -- failed_at set is never claimed again, but stays queryable here
    -- for operator visibility rather than disappearing into "only
    -- reachable via raw SQL and attempts >= N reasoning."
    failed_at TIMESTAMPTZ NULL
);

-- Partial index on exactly the columns/predicate the claim query
-- filters and orders by, so polling for pending work stays cheap as
-- the table grows (processed/failed rows fall out of the index
-- entirely).
CREATE INDEX outbox_events_pending_idx ON outbox_events (available_at)
    WHERE processed_at IS NULL AND failed_at IS NULL;

-- For operational inspection of permanently failed events.
CREATE INDEX outbox_events_failed_idx ON outbox_events (failed_at) WHERE failed_at IS NOT NULL;
