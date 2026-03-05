CREATE TABLE IF NOT EXISTS raw_events (
    id TEXT PRIMARY KEY,
    sequence BIGINT NOT NULL DEFAULT 0,
    source TEXT NOT NULL,
    payload JSONB NOT NULL,
    source_ts TIMESTAMPTZ NOT NULL,
    ingest_ts TIMESTAMPTZ NOT NULL,
    normalized_ts TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS raw_events_ingest_ts_idx ON raw_events (ingest_ts);
CREATE INDEX IF NOT EXISTS raw_events_source_ts_idx ON raw_events (source_ts);
