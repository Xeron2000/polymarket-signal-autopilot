CREATE TABLE IF NOT EXISTS news_events (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    source_name TEXT NOT NULL,
    source_url TEXT NOT NULL DEFAULT '',
    reliability_tier TEXT NOT NULL,
    published_ts TIMESTAMPTZ NOT NULL,
    first_seen_ingest_ts TIMESTAMPTZ NOT NULL,
    provenance JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS news_events_first_seen_idx ON news_events (first_seen_ingest_ts);
CREATE INDEX IF NOT EXISTS news_events_source_idx ON news_events (source_name);
