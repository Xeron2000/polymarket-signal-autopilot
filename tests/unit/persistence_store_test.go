package unit

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"polymarket-signal/internal/ingestion"
	"polymarket-signal/internal/persistence"
)

func TestSQLStoreWriteRawEventHandlesInsertAndDuplicate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	defer db.Close()

	store := persistence.NewStore(db)
	now := time.Now().UTC().Truncate(time.Second)
	event := ingestion.RawEvent{
		ID:           "evt-1",
		Sequence:     1,
		Source:       "polymarket-ws",
		Payload:      []byte(`{"bid":0.44}`),
		SourceTS:     now,
		IngestTS:     now,
		NormalizedTS: now,
	}

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO raw_events (id, sequence, source, payload, source_ts, ingest_ts, normalized_ts) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (id) DO NOTHING`)).
		WithArgs(event.ID, event.Sequence, event.Source, event.Payload, event.SourceTS, event.IngestTS, event.NormalizedTS).
		WillReturnResult(sqlmock.NewResult(0, 1))

	stored, err := store.Write(event)
	if err != nil {
		t.Fatalf("expected first write success, got %v", err)
	}
	if !stored {
		t.Fatal("expected first write stored=true")
	}

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO raw_events (id, sequence, source, payload, source_ts, ingest_ts, normalized_ts) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (id) DO NOTHING`)).
		WithArgs(event.ID, event.Sequence, event.Source, event.Payload, event.SourceTS, event.IngestTS, event.NormalizedTS).
		WillReturnResult(sqlmock.NewResult(0, 0))

	stored, err = store.Write(event)
	if err != nil {
		t.Fatalf("expected duplicate write no error, got %v", err)
	}
	if stored {
		t.Fatal("expected duplicate write stored=false")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations not met: %v", err)
	}
}

func TestSQLStorePersistsSignalOrderAndNotification(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	defer db.Close()

	store := persistence.NewStore(db)
	now := time.Now().UTC().Truncate(time.Second)

	signal := persistence.SignalRecord{
		ID:         "sig-1",
		AssetID:    "tok1",
		Direction:  "long",
		Confidence: 0.82,
		Reason:     "rank_flip_polymarket_leads",
		Payload:    []byte(`{"leader":"axiom","lagger":"meteora"}`),
		CreatedAt:  now,
	}
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO signals (id, asset_id, direction, confidence, reason, payload, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`)).
		WithArgs(signal.ID, signal.AssetID, signal.Direction, signal.Confidence, signal.Reason, signal.Payload, signal.CreatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.SaveSignal(signal); err != nil {
		t.Fatalf("expected save signal success, got %v", err)
	}

	order := persistence.OrderRecord{
		ID:        "ord-rec-1",
		SignalID:  signal.ID,
		Status:    "submitted",
		OrderID:   "0xorder1",
		ErrorMsg:  "",
		CreatedAt: now,
	}
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO order_attempts (id, signal_id, status, order_id, error_msg, created_at) VALUES ($1,$2,$3,$4,$5,$6)`)).
		WithArgs(order.ID, order.SignalID, order.Status, order.OrderID, order.ErrorMsg, order.CreatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.SaveOrder(order); err != nil {
		t.Fatalf("expected save order success, got %v", err)
	}

	notify := persistence.NotificationRecord{
		ID:        "notif-1",
		Channel:   "telegram",
		Target:    "chat-1",
		Message:   "signal fired",
		Status:    "sent",
		ErrorMsg:  "",
		CreatedAt: now,
	}
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO notifications (id, channel, target, message, status, error_msg, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`)).
		WithArgs(notify.ID, notify.Channel, notify.Target, notify.Message, notify.Status, notify.ErrorMsg, notify.CreatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.SaveNotification(notify); err != nil {
		t.Fatalf("expected save notification success, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations not met: %v", err)
	}
}

func TestSQLStoreListPendingOrderReconciliations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	defer db.Close()

	store := persistence.NewStore(db)

	rows := sqlmock.NewRows([]string{"signal_id", "order_id", "status"}).
		AddRow("sig-1", "ord-1", "live").
		AddRow("sig-2", "ord-2", "reconcile_pending")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT latest.signal_id, latest.order_id, latest.status FROM (SELECT DISTINCT ON (order_id) signal_id, order_id, status, created_at FROM order_attempts WHERE order_id <> '' ORDER BY order_id, created_at DESC) latest WHERE latest.status NOT IN ('filled','cancelled','canceled','expired','failed','matched') ORDER BY latest.created_at DESC LIMIT $1`)).
		WithArgs(10).
		WillReturnRows(rows)

	pending, err := store.ListPendingOrderReconciliations(10)
	if err != nil {
		t.Fatalf("expected list pending reconciliations success, got %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending rows, got %d", len(pending))
	}
	if pending[0].OrderID != "ord-1" || pending[1].OrderID != "ord-2" {
		t.Fatalf("unexpected pending rows: %+v", pending)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations not met: %v", err)
	}
}

func TestSQLStoreRuntimeStateRoundTrip(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	defer db.Close()

	store := persistence.NewStore(db)
	ctx := context.Background()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO runtime_state (key, value, updated_at) VALUES ($1,$2,$3) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`)).
		WithArgs("nonce:0xabc", "101", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.SetRuntimeState(ctx, "nonce:0xabc", "101"); err != nil {
		t.Fatalf("expected set runtime state success, got %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM runtime_state WHERE key = $1`)).
		WithArgs("nonce:0xabc").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("101"))

	value, found, err := store.GetRuntimeState(ctx, "nonce:0xabc")
	if err != nil {
		t.Fatalf("expected get runtime state success, got %v", err)
	}
	if !found {
		t.Fatal("expected runtime state to exist")
	}
	if value != "101" {
		t.Fatalf("unexpected runtime state value: %q", value)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations not met: %v", err)
	}
}

func TestSQLStoreAllocateRuntimeNonce(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock failed: %v", err)
	}
	defer db.Close()

	store := persistence.NewStore(db)
	ctx := context.Background()

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO runtime_state (key, value, updated_at) VALUES ($1, ($2 + 1)::text, $3) ON CONFLICT (key) DO UPDATE SET value = (CASE WHEN runtime_state.value ~ '^-?[0-9]+$' THEN (runtime_state.value::bigint + 1)::text ELSE EXCLUDED.value END), updated_at = EXCLUDED.updated_at RETURNING (runtime_state.value::bigint - 1) AS allocated_nonce`)).
		WithArgs("nonce:0xabc", int64(7), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"allocated_nonce"}).AddRow(int64(40)))

	nonce, err := store.AllocateRuntimeNonce(ctx, "nonce:0xabc", 7)
	if err != nil {
		t.Fatalf("expected allocate runtime nonce success, got %v", err)
	}
	if nonce != 40 {
		t.Fatalf("expected allocated nonce 40, got %d", nonce)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations not met: %v", err)
	}
}
