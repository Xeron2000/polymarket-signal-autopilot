package unit

import (
	"testing"
	"time"

	"polymarket-signal/internal/features"
	"polymarket-signal/internal/risk"
	"polymarket-signal/internal/signals"
)

func TestSignalHasReasonCodeAndRiskFilterBlocks(t *testing.T) {
	asOf := time.Now().UTC()
	inputs := []features.Feature{
		{Name: "price_velocity", Value: 0.9, ComputedAt: asOf},
		{Name: "contradiction_index", Value: 0.1, ComputedAt: asOf},
		{Name: "lead_lag_magnitude", Value: 0.8, ComputedAt: asOf},
	}

	sig := signals.Generate(inputs)
	if sig.ReasonCode == "" {
		t.Fatal("expected signal to contain reason code")
	}

	filtered := risk.Apply(sig, risk.Context{
		CurrentExposure: 150,
		MaxExposure:     100,
		DataAge:         2 * time.Second,
		StaleAfter:      5 * time.Second,
	})

	if !filtered.Blocked {
		t.Fatal("expected signal blocked due to exposure cap")
	}
	if filtered.BlockReason == "" {
		t.Fatal("expected block reason for auditability")
	}
}
