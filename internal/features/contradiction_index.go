package features

import "polymarket-signal/internal/linking"

func ComputeContradictionIndex(deltas []linking.LeadLagDelta) float64 {
	if len(deltas) == 0 {
		return 0
	}

	contradictions := 0
	for _, delta := range deltas {
		if !linking.DirectionConsistent(delta) {
			contradictions++
		}
	}
	return float64(contradictions) / float64(len(deltas))
}
