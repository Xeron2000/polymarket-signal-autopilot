package unit

import (
	"testing"

	"polymarket-signal/internal/config"
)

func TestContractTimestampRequirementValidate(t *testing.T) {
	req := config.TimestampRequirement{
		RequireSourceTS:     true,
		RequireIngestTS:     true,
		RequireNormalizedTS: true,
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("expected timestamp requirement to be valid, got error: %v", err)
	}

	invalid := config.TimestampRequirement{}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected missing timestamp requirements to fail validation")
	}
}

func TestContractGoNoGoGateValidate(t *testing.T) {
	gates := config.GoNoGoGates{
		DataQuality: config.DataQualityGate{
			MissingFieldPctMax:  0.5,
			P99IngestLatencyMs:  3000,
			ReconnectSuccessMin: 99,
			UnresolvedGapMax:    0,
		},
		CausalRobustness: config.CausalRobustnessGate{
			FDRQMax:          0.05,
			AntiLookaheadMin: 100,
		},
		EconomicViability: config.EconomicViabilityGate{
			MaxDrawdownPct:      8,
			CapacityDecayPctMax: 20,
		},
		OperationalSafety: config.OperationalSafetyGate{
			SlippageDeviationPctMax: 20,
		},
	}

	if err := gates.Validate(); err != nil {
		t.Fatalf("expected valid gate config, got error: %v", err)
	}

	invalid := gates
	invalid.DataQuality.MissingFieldPctMax = 2
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected invalid gate config to fail validation")
	}
}
