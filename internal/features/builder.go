package features

import (
	"fmt"
	"math"
	"sort"
	"time"

	"polymarket-signal/internal/linking"
)

type MarketPoint struct {
	At     time.Time
	Price  float64
	Spread float64
	Volume float64
}

type Feature struct {
	Name       string
	Value      float64
	ComputedAt time.Time
}

func BuildFeatures(deltas []linking.LeadLagDelta, series []MarketPoint, asOf time.Time) ([]Feature, error) {
	if asOf.IsZero() {
		return nil, fmt.Errorf("asOf is required")
	}
	if len(series) < 2 {
		return nil, fmt.Errorf("at least two market points are required")
	}

	sorted := make([]MarketPoint, len(series))
	copy(sorted, series)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].At.Before(sorted[j].At)
	})

	for _, point := range sorted {
		if point.At.After(asOf) {
			return nil, fmt.Errorf("no-lookahead violation: point %s after asOf %s", point.At, asOf)
		}
	}

	first := sorted[0]
	last := sorted[len(sorted)-1]
	dt := last.At.Sub(first.At).Seconds()
	if dt <= 0 {
		dt = 1
	}
	velocity := (last.Price - first.Price) / dt

	absSum := 0.0
	for _, delta := range deltas {
		absSum += math.Abs(delta.DeltaSource.Seconds())
	}
	leadLagMagnitude := 0.0
	if len(deltas) > 0 {
		leadLagMagnitude = absSum / float64(len(deltas))
	}

	contradiction := ComputeContradictionIndex(deltas)
	novelty := math.Abs(last.Spread-first.Spread) + math.Abs(last.Volume-first.Volume)/10000
	cluster := ClusterConfirmationScore(sorted)

	return []Feature{
		{Name: "price_velocity", Value: velocity, ComputedAt: asOf},
		{Name: "lead_lag_magnitude", Value: leadLagMagnitude, ComputedAt: asOf},
		{Name: "contradiction_index", Value: contradiction, ComputedAt: asOf},
		{Name: "novelty_score", Value: novelty, ComputedAt: asOf},
		{Name: "cluster_confirmation", Value: cluster, ComputedAt: asOf},
	}, nil
}
