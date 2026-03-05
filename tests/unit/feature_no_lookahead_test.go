package unit

import (
	"testing"
	"time"

	"polymarket-signal/internal/features"
	"polymarket-signal/internal/linking"
)

func TestFeatureBuilderNoLookahead(t *testing.T) {
	asOf := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)

	deltas := []linking.LeadLagDelta{
		{LinkID: "l1", DeltaSource: -1 * time.Second, DeltaIngest: -500 * time.Millisecond},
	}

	seriesWithFuture := []features.MarketPoint{
		{At: asOf.Add(-2 * time.Second), Price: 0.45, Spread: 0.01, Volume: 1000},
		{At: asOf.Add(1 * time.Second), Price: 0.48, Spread: 0.02, Volume: 1200},
	}

	if _, err := features.BuildFeatures(deltas, seriesWithFuture, asOf); err == nil {
		t.Fatal("expected no-lookahead validation to fail on future data")
	}

	seriesValid := []features.MarketPoint{
		{At: asOf.Add(-3 * time.Second), Price: 0.42, Spread: 0.02, Volume: 900},
		{At: asOf.Add(-1 * time.Second), Price: 0.45, Spread: 0.01, Volume: 1100},
	}

	computed, err := features.BuildFeatures(deltas, seriesValid, asOf)
	if err != nil {
		t.Fatalf("expected valid feature build, got error: %v", err)
	}
	if len(computed) == 0 {
		t.Fatal("expected non-empty feature set")
	}
}

func TestFeatureBuilderSortsSeriesByTimestamp(t *testing.T) {
	asOf := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	deltas := []linking.LeadLagDelta{{LinkID: "l1", DeltaSource: -1 * time.Second, DeltaIngest: -1 * time.Second}}

	unsorted := []features.MarketPoint{
		{At: asOf.Add(-1 * time.Second), Price: 0.50, Spread: 0.01, Volume: 1200},
		{At: asOf.Add(-3 * time.Second), Price: 0.40, Spread: 0.03, Volume: 900},
	}

	computed, err := features.BuildFeatures(deltas, unsorted, asOf)
	if err != nil {
		t.Fatalf("expected unsorted input to be handled, got error: %v", err)
	}

	velocity := 0.0
	for _, feature := range computed {
		if feature.Name == "price_velocity" {
			velocity = feature.Value
		}
	}
	if velocity <= 0 {
		t.Fatalf("expected positive velocity after chronological sort, got %.6f", velocity)
	}
}
