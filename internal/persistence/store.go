package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"polymarket-signal/internal/ingestion"
)

const (
	insertRawEventQuery = `INSERT INTO raw_events (id, sequence, source, payload, source_ts, ingest_ts, normalized_ts) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (id) DO NOTHING`
	insertSignalQuery   = `INSERT INTO signals (id, asset_id, direction, confidence, reason, payload, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`
	insertOrderQuery    = `INSERT INTO order_attempts (id, signal_id, status, order_id, error_msg, created_at) VALUES ($1,$2,$3,$4,$5,$6)`
	insertNotifyQuery   = `INSERT INTO notifications (id, channel, target, message, status, error_msg, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`
	selectPendingOrders = `SELECT latest.signal_id, latest.order_id, latest.status FROM (SELECT DISTINCT ON (order_id) signal_id, order_id, status, created_at FROM order_attempts WHERE order_id <> '' ORDER BY order_id, created_at DESC) latest WHERE latest.status NOT IN ('filled','cancelled','canceled','expired','failed','matched') ORDER BY latest.created_at DESC LIMIT $1`
	upsertStateQuery    = `INSERT INTO runtime_state (key, value, updated_at) VALUES ($1,$2,$3) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`
	selectStateQuery    = `SELECT value FROM runtime_state WHERE key = $1`
	allocateNonceQuery  = `INSERT INTO runtime_state (key, value, updated_at) VALUES ($1, ($2 + 1)::text, $3) ON CONFLICT (key) DO UPDATE SET value = (CASE WHEN runtime_state.value ~ '^-?[0-9]+$' THEN (runtime_state.value::bigint + 1)::text ELSE EXCLUDED.value END), updated_at = EXCLUDED.updated_at RETURNING (runtime_state.value::bigint - 1) AS allocated_nonce`
)

type dbExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Store struct {
	db dbExecutor
}

type SignalRecord struct {
	ID         string
	AssetID    string
	Direction  string
	Confidence float64
	Reason     string
	Payload    []byte
	CreatedAt  time.Time
}

type OrderRecord struct {
	ID        string
	SignalID  string
	Status    string
	OrderID   string
	ErrorMsg  string
	CreatedAt time.Time
}

type NotificationRecord struct {
	ID        string
	Channel   string
	Target    string
	Message   string
	Status    string
	ErrorMsg  string
	CreatedAt time.Time
}

type PendingOrderRecord struct {
	SignalID string
	OrderID  string
	Status   string
}

func NewStore(db dbExecutor) *Store {
	return &Store{db: db}
}

func OpenPostgres(dsn string) (*sql.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres connection: %w", err)
	}
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetMaxOpenConns(12)
	db.SetMaxIdleConns(6)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres connection: %w", err)
	}
	return db, nil
}

func (s *Store) Write(event ingestion.RawEvent) (bool, error) {
	if strings.TrimSpace(event.ID) == "" {
		return false, fmt.Errorf("raw event id is required")
	}
	if event.SourceTS.IsZero() || event.IngestTS.IsZero() || event.NormalizedTS.IsZero() {
		return false, fmt.Errorf("raw event timestamps are required")
	}

	result, err := s.db.ExecContext(
		context.Background(),
		insertRawEventQuery,
		event.ID,
		event.Sequence,
		event.Source,
		event.Payload,
		event.SourceTS,
		event.IngestTS,
		event.NormalizedTS,
	)
	if err != nil {
		return false, fmt.Errorf("insert raw event: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read raw event affected rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) SaveSignal(record SignalRecord) error {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(record.ID) == "" {
		return fmt.Errorf("signal id is required")
	}

	_, err := s.db.ExecContext(
		context.Background(),
		insertSignalQuery,
		record.ID,
		record.AssetID,
		record.Direction,
		record.Confidence,
		record.Reason,
		record.Payload,
		record.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert signal record: %w", err)
	}
	return nil
}

func (s *Store) SaveOrder(record OrderRecord) error {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(record.ID) == "" {
		return fmt.Errorf("order record id is required")
	}

	_, err := s.db.ExecContext(
		context.Background(),
		insertOrderQuery,
		record.ID,
		record.SignalID,
		record.Status,
		record.OrderID,
		record.ErrorMsg,
		record.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert order record: %w", err)
	}
	return nil
}

func (s *Store) SaveNotification(record NotificationRecord) error {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(record.ID) == "" {
		return fmt.Errorf("notification id is required")
	}

	_, err := s.db.ExecContext(
		context.Background(),
		insertNotifyQuery,
		record.ID,
		record.Channel,
		record.Target,
		record.Message,
		record.Status,
		record.ErrorMsg,
		record.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert notification record: %w", err)
	}
	return nil
}

func (s *Store) ListPendingOrderReconciliations(limit int) ([]PendingOrderRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(context.Background(), selectPendingOrders, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending order reconciliations: %w", err)
	}
	defer rows.Close()

	pending := make([]PendingOrderRecord, 0, limit)
	for rows.Next() {
		entry := PendingOrderRecord{}
		if scanErr := rows.Scan(&entry.SignalID, &entry.OrderID, &entry.Status); scanErr != nil {
			return nil, fmt.Errorf("scan pending order reconciliation row: %w", scanErr)
		}
		pending = append(pending, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending order reconciliation rows: %w", err)
	}
	return pending, nil
}

func (s *Store) SetRuntimeState(ctx context.Context, key string, value string) error {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return fmt.Errorf("runtime state key is required")
	}
	_, err := s.db.ExecContext(ctx, upsertStateQuery, trimmedKey, value, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("upsert runtime state: %w", err)
	}
	return nil
}

func (s *Store) GetRuntimeState(ctx context.Context, key string) (string, bool, error) {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return "", false, fmt.Errorf("runtime state key is required")
	}
	var value string
	err := s.db.QueryRowContext(ctx, selectStateQuery, trimmedKey).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query runtime state: %w", err)
	}
	return value, true, nil
}

func (s *Store) AllocateRuntimeNonce(ctx context.Context, key string, seed int64) (int64, error) {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return 0, fmt.Errorf("runtime nonce key is required")
	}
	if seed < 0 {
		return 0, fmt.Errorf("runtime nonce seed must be >= 0")
	}
	var allocated int64
	err := s.db.QueryRowContext(ctx, allocateNonceQuery, trimmedKey, seed, time.Now().UTC()).Scan(&allocated)
	if err != nil {
		return 0, fmt.Errorf("allocate runtime nonce: %w", err)
	}
	if allocated < 0 {
		return 0, fmt.Errorf("allocated runtime nonce must be >= 0")
	}
	return allocated, nil
}
