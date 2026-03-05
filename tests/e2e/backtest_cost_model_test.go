package e2e

import (
	"testing"

	"polymarket-signal/internal/execution"
)

func TestBacktestCostAdjustedExpectancy(t *testing.T) {
	trades := []execution.TradeOutcome{
		{GrossReturnPct: 0.020},
		{GrossReturnPct: 0.010},
		{GrossReturnPct: -0.005},
	}

	report := execution.ComputeExpectancy(trades, 10, 10)
	if report.NetExpectancyPct <= 0 {
		t.Fatalf("expected positive net expectancy, got %.6f", report.NetExpectancyPct)
	}
	if report.CostPerTradePct <= 0 {
		t.Fatalf("expected positive per-trade cost, got %.6f", report.CostPerTradePct)
	}
}
