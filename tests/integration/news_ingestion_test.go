package integration

import (
	"testing"
	"time"

	"polymarket-signal/internal/connectors/news"
)

func TestNewsNormalizationUsesIndependentFirstSeenIngestTS(t *testing.T) {
	published := time.Date(2026, 3, 5, 10, 0, 0, 0, time.FixedZone("UTC-5", -5*3600))
	ingest := published.Add(11 * time.Minute).UTC()

	item := news.Item{
		ID:          "news-1",
		Title:       "  CPI surprise lifts election odds  ",
		SourceName:  "Reuters",
		PublishedTS: published,
		Provenance:  map[string]string{"url": "https://example.com/cpi"},
	}

	normalized, err := news.NormalizeItem(item, ingest)
	if err != nil {
		t.Fatalf("unexpected normalize error: %v", err)
	}
	if normalized.Title != "CPI surprise lifts election odds" {
		t.Fatalf("expected trimmed title, got %q", normalized.Title)
	}
	if normalized.PublishedTS.Location() != time.UTC {
		t.Fatalf("expected published_ts normalized to UTC, got %s", normalized.PublishedTS.Location())
	}
	if !normalized.FirstSeenIngestTS.Equal(ingest) {
		t.Fatalf("expected first_seen_ingest_ts=%s, got %s", ingest, normalized.FirstSeenIngestTS)
	}
	if normalized.FirstSeenIngestTS.Equal(normalized.PublishedTS) {
		t.Fatal("expected independent first_seen_ingest_ts not equal publisher-declared time")
	}
}

func TestNewsReliabilityTierAndProvenance(t *testing.T) {
	item := news.Item{
		ID:          "news-2",
		Title:       "Fed statement update",
		SourceName:  "Reuters",
		PublishedTS: time.Now().UTC(),
		Provenance:  map[string]string{"url": "https://example.com/fed"},
	}

	enriched := news.EnrichReliability(item)
	if enriched.ReliabilityTier != news.TierA {
		t.Fatalf("expected Reuters to map to TierA, got %s", enriched.ReliabilityTier)
	}
	if enriched.Provenance["original_source"] != "Reuters" {
		t.Fatalf("expected provenance to preserve original source, got %q", enriched.Provenance["original_source"])
	}
}
