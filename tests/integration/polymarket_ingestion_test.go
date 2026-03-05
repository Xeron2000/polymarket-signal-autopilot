package integration

import (
	"testing"
	"time"

	"polymarket-signal/internal/connectors/polymarket"
	"polymarket-signal/internal/ingestion"
)

func TestIngestionPersistsImmutableRawEventAndTimestamps(t *testing.T) {
	writer := ingestion.NewMemoryRawWriter()
	now := time.Now().UTC()

	event := ingestion.RawEvent{
		ID:           "evt-1",
		Source:       "polymarket",
		Payload:      []byte(`{"price":0.44}`),
		SourceTS:     now.Add(-2 * time.Second),
		IngestTS:     now,
		NormalizedTS: now,
	}

	stored, err := writer.Write(event)
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if !stored {
		t.Fatal("expected first event write to be stored")
	}

	event.Payload[0] = 'x'

	saved, ok := writer.Get("evt-1")
	if !ok {
		t.Fatal("expected stored event to exist")
	}
	if string(saved.Payload) != `{"price":0.44}` {
		t.Fatalf("expected stored payload immutable, got %s", string(saved.Payload))
	}
	if saved.SourceTS.IsZero() || saved.IngestTS.IsZero() || saved.NormalizedTS.IsZero() {
		t.Fatal("expected source_ts/ingest_ts/normalized_ts to be persisted")
	}
}

func TestIngestionDeduplicatesEventID(t *testing.T) {
	writer := ingestion.NewMemoryRawWriter()
	now := time.Now().UTC()

	first := ingestion.RawEvent{
		ID:           "evt-dup",
		Source:       "polymarket",
		Payload:      []byte(`{"price":0.5}`),
		SourceTS:     now,
		IngestTS:     now,
		NormalizedTS: now,
	}

	second := first
	second.Payload = []byte(`{"price":0.9}`)

	if stored, err := writer.Write(first); err != nil || !stored {
		t.Fatalf("expected first write stored, stored=%v err=%v", stored, err)
	}
	if stored, err := writer.Write(second); err != nil || stored {
		t.Fatalf("expected duplicate write to be idempotent drop, stored=%v err=%v", stored, err)
	}

	saved, ok := writer.Get("evt-dup")
	if !ok {
		t.Fatal("expected deduplicated event to remain available")
	}
	if string(saved.Payload) != `{"price":0.5}` {
		t.Fatalf("expected first payload to remain authoritative, got %s", string(saved.Payload))
	}
}

func TestRetryPolicyHandles425(t *testing.T) {
	if !polymarket.ShouldRetryHTTPStatus(425) {
		t.Fatal("expected http 425 to be retriable")
	}
	if polymarket.ShouldRetryHTTPStatus(400) {
		t.Fatal("expected http 400 to be non-retriable")
	}
}
