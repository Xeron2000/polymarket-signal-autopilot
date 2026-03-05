package integration

import (
	"testing"
	"time"

	"polymarket-signal/internal/connectors/news"
	"polymarket-signal/internal/ingestion"
	"polymarket-signal/internal/linking"
)

func TestLeadLagComputesDualClockAndConsistency(t *testing.T) {
	marketSource := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	marketIngest := marketSource.Add(2 * time.Second)
	newsSource := marketSource.Add(-30 * time.Second)
	newsIngest := marketIngest.Add(-500 * time.Millisecond)

	marketEvents := []ingestion.RawEvent{
		{
			ID:           "mkt-1",
			Source:       "polymarket",
			Payload:      []byte(`{"headline":"inflation shock"}`),
			SourceTS:     marketSource,
			IngestTS:     marketIngest,
			NormalizedTS: marketIngest,
		},
	}

	newsItems := []news.Item{
		{
			ID:                "news-1",
			Title:             "Inflation surprise in latest report",
			SourceName:        "Reuters",
			PublishedTS:       newsSource,
			FirstSeenIngestTS: newsIngest,
			Provenance:        map[string]string{"url": "https://example.com/cpi"},
		},
	}

	links := linking.LinkByKeyword(marketEvents, newsItems)
	if len(links) != 1 {
		t.Fatalf("expected one linked pair, got %d", len(links))
	}
	if links[0].Confidence <= 0 || links[0].Confidence > 1 {
		t.Fatalf("expected confidence in (0,1], got %f", links[0].Confidence)
	}

	delta := linking.ComputeDualClockDelta(links[0])
	if delta.DeltaSource >= 0 {
		t.Fatalf("expected news to lead on source clock, delta_source=%s", delta.DeltaSource)
	}
	if delta.DeltaIngest >= 0 {
		t.Fatalf("expected news to lead on ingest clock, delta_ingest=%s", delta.DeltaIngest)
	}
	if !linking.DirectionConsistent(delta) {
		t.Fatalf("expected direction consistency, got source=%s ingest=%s", delta.DirectionSource, delta.DirectionIngest)
	}
}

func TestLinkerRejectsUnrelatedNews(t *testing.T) {
	now := time.Now().UTC()
	marketEvents := []ingestion.RawEvent{
		{
			ID:           "mkt-2",
			Source:       "polymarket",
			Payload:      []byte(`{"headline":"jobs report"}`),
			SourceTS:     now,
			IngestTS:     now,
			NormalizedTS: now,
		},
	}
	newsItems := []news.Item{
		{
			ID:                "news-2",
			Title:             "Hurricane warning for coastal region",
			SourceName:        "Reuters",
			PublishedTS:       now,
			FirstSeenIngestTS: now,
		},
	}

	links := linking.LinkByKeyword(marketEvents, newsItems)
	if len(links) != 0 {
		t.Fatalf("expected unrelated news to produce no links, got %d", len(links))
	}
}
