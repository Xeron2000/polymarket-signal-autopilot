package e2e

import (
	"testing"
	"time"

	"polymarket-signal/internal/ingestion"
	"polymarket-signal/internal/ops"
)

func TestGapRecoveryFillsMissingSequences(t *testing.T) {
	now := time.Now().UTC()
	events := []ingestion.RawEvent{
		{ID: "1", Sequence: 1, Source: "polymarket", Payload: []byte(`{"v":1}`), SourceTS: now, IngestTS: now, NormalizedTS: now},
		{ID: "3", Sequence: 3, Source: "polymarket", Payload: []byte(`{"v":3}`), SourceTS: now, IngestTS: now, NormalizedTS: now},
	}

	gaps := ingestion.DetectSequenceGaps(events)
	if len(gaps) != 1 || gaps[0].From != 2 || gaps[0].To != 2 {
		t.Fatalf("expected single missing sequence 2, got %+v", gaps)
	}

	backfilled := ingestion.FillGaps(gaps, now)
	if len(backfilled) != 1 {
		t.Fatalf("expected 1 backfilled record, got %d", len(backfilled))
	}
	if backfilled[0].Sequence != 2 || backfilled[0].Source != "backfill" {
		t.Fatalf("unexpected backfill event: %+v", backfilled[0])
	}
}

func TestReplayIsDeterministic(t *testing.T) {
	now := time.Now().UTC()
	inputA := []ingestion.RawEvent{
		{ID: "evt-2", Sequence: 2, Source: "polymarket", Payload: []byte(`2`), SourceTS: now, IngestTS: now, NormalizedTS: now},
		{ID: "evt-1", Sequence: 1, Source: "polymarket", Payload: []byte(`1`), SourceTS: now, IngestTS: now, NormalizedTS: now},
	}
	inputB := []ingestion.RawEvent{
		{ID: "evt-1", Sequence: 1, Source: "polymarket", Payload: []byte(`1`), SourceTS: now, IngestTS: now, NormalizedTS: now},
		{ID: "evt-2", Sequence: 2, Source: "polymarket", Payload: []byte(`2`), SourceTS: now, IngestTS: now, NormalizedTS: now},
	}

	first := ops.ReplayDeterministic(inputA)
	second := ops.ReplayDeterministic(inputB)

	if first.Checksum != second.Checksum {
		t.Fatalf("expected deterministic replay checksum, got %s vs %s", first.Checksum, second.Checksum)
	}
	if len(first.OrderedIDs) != 2 || first.OrderedIDs[0] != "evt-1" || first.OrderedIDs[1] != "evt-2" {
		t.Fatalf("unexpected replay order: %+v", first.OrderedIDs)
	}
}

func TestReplayChecksumIncludesTimestampAndSource(t *testing.T) {
	now := time.Now().UTC()
	base := []ingestion.RawEvent{{
		ID:           "evt-1",
		Sequence:     1,
		Source:       "polymarket",
		Payload:      []byte(`1`),
		SourceTS:     now,
		IngestTS:     now,
		NormalizedTS: now,
	}}
	changed := []ingestion.RawEvent{{
		ID:           "evt-1",
		Sequence:     1,
		Source:       "news",
		Payload:      []byte(`1`),
		SourceTS:     now,
		IngestTS:     now,
		NormalizedTS: now.Add(1 * time.Second),
	}}

	first := ops.ReplayDeterministic(base)
	second := ops.ReplayDeterministic(changed)

	if first.Checksum == second.Checksum {
		t.Fatalf("expected checksum change when source/timestamp changes, got identical %s", first.Checksum)
	}
}
