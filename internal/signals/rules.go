package signals

import "polymarket-signal/internal/features"

type Direction string

const (
	Long    Direction = "long"
	Short   Direction = "short"
	Neutral Direction = "neutral"
)

type Signal struct {
	Direction  Direction
	Confidence float64
	ReasonCode string
}

func Generate(inputs []features.Feature) Signal {
	values := make(map[string]float64, len(inputs))
	for _, feature := range inputs {
		values[feature.Name] = feature.Value
	}

	if values["contradiction_index"] > 0.6 {
		return Signal{Direction: Neutral, Confidence: 0.2, ReasonCode: "contradiction_high"}
	}

	velocity := values["price_velocity"]
	leadLag := values["lead_lag_magnitude"]

	if velocity > 0 && leadLag > 0 {
		return Signal{Direction: Long, Confidence: min(1, velocity+leadLag), ReasonCode: "momentum_leadlag_long"}
	}
	if velocity < 0 && leadLag > 0 {
		return Signal{Direction: Short, Confidence: min(1, -velocity+leadLag), ReasonCode: "momentum_leadlag_short"}
	}

	return Signal{Direction: Neutral, Confidence: 0.5, ReasonCode: "insufficient_edge"}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
