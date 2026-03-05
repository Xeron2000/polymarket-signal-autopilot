CREATE TABLE IF NOT EXISTS linked_events (
    id TEXT PRIMARY KEY,
    market_event_id TEXT NOT NULL,
    news_event_id TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL,
    delta_source_ms BIGINT NOT NULL,
    delta_ingest_ms BIGINT NOT NULL,
    direction_source TEXT NOT NULL,
    direction_ingest TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS linked_events_market_idx ON linked_events (market_event_id);
CREATE INDEX IF NOT EXISTS linked_events_news_idx ON linked_events (news_event_id);
