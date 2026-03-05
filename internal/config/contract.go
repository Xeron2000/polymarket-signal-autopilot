package config

import "fmt"

type TimestampRequirement struct {
	RequireSourceTS     bool
	RequireIngestTS     bool
	RequireNormalizedTS bool
}

func (r TimestampRequirement) Validate() error {
	if !r.RequireSourceTS || !r.RequireIngestTS || !r.RequireNormalizedTS {
		return fmt.Errorf("all timestamp requirements must be enabled")
	}
	return nil
}

type DataQualityGate struct {
	MissingFieldPctMax  float64
	P99IngestLatencyMs  int
	ReconnectSuccessMin float64
	UnresolvedGapMax    int
}

type CausalRobustnessGate struct {
	FDRQMax          float64
	AntiLookaheadMin float64
}

type EconomicViabilityGate struct {
	MaxDrawdownPct      float64
	CapacityDecayPctMax float64
}

type OperationalSafetyGate struct {
	SlippageDeviationPctMax float64
}

type GoNoGoGates struct {
	DataQuality       DataQualityGate
	CausalRobustness  CausalRobustnessGate
	EconomicViability EconomicViabilityGate
	OperationalSafety OperationalSafetyGate
}

func (g GoNoGoGates) Validate() error {
	if g.DataQuality.MissingFieldPctMax <= 0 || g.DataQuality.MissingFieldPctMax > 1 {
		return fmt.Errorf("missing field threshold must be in (0, 1]")
	}
	if g.DataQuality.P99IngestLatencyMs <= 0 {
		return fmt.Errorf("p99 ingest latency must be positive")
	}
	if g.DataQuality.ReconnectSuccessMin < 0 || g.DataQuality.ReconnectSuccessMin > 100 {
		return fmt.Errorf("reconnect success must be between 0 and 100")
	}
	if g.DataQuality.UnresolvedGapMax < 0 {
		return fmt.Errorf("unresolved gap max cannot be negative")
	}

	if g.CausalRobustness.FDRQMax <= 0 || g.CausalRobustness.FDRQMax > 1 {
		return fmt.Errorf("fdr q threshold must be in (0, 1]")
	}
	if g.CausalRobustness.AntiLookaheadMin != 100 {
		return fmt.Errorf("anti-lookahead minimum must be 100")
	}

	if g.EconomicViability.MaxDrawdownPct <= 0 || g.EconomicViability.MaxDrawdownPct > 100 {
		return fmt.Errorf("max drawdown must be in (0, 100]")
	}
	if g.EconomicViability.CapacityDecayPctMax < 0 || g.EconomicViability.CapacityDecayPctMax > 100 {
		return fmt.Errorf("capacity decay max must be in [0, 100]")
	}

	if g.OperationalSafety.SlippageDeviationPctMax < 0 || g.OperationalSafety.SlippageDeviationPctMax > 100 {
		return fmt.Errorf("slippage deviation max must be in [0, 100]")
	}

	return nil
}
