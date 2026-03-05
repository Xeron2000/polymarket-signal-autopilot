CREATE TABLE IF NOT EXISTS signals (
    id TEXT PRIMARY KEY,
    asset_id TEXT NOT NULL,
    direction TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL,
    reason TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS signals_asset_idx ON signals (asset_id);
CREATE INDEX IF NOT EXISTS signals_created_at_idx ON signals (created_at);

CREATE TABLE IF NOT EXISTS order_attempts (
    id TEXT PRIMARY KEY,
    signal_id TEXT NOT NULL,
    status TEXT NOT NULL,
    order_id TEXT NOT NULL,
    error_msg TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS order_attempts_signal_idx ON order_attempts (signal_id);
CREATE INDEX IF NOT EXISTS order_attempts_status_idx ON order_attempts (status);

CREATE TABLE IF NOT EXISTS notifications (
    id TEXT PRIMARY KEY,
    channel TEXT NOT NULL,
    target TEXT NOT NULL,
    message TEXT NOT NULL,
    status TEXT NOT NULL,
    error_msg TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS notifications_channel_idx ON notifications (channel);
CREATE INDEX IF NOT EXISTS notifications_created_at_idx ON notifications (created_at);
