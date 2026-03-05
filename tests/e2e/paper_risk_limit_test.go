package e2e

import (
	"testing"

	"polymarket-signal/internal/execution"
)

func TestPaperRouterRiskLimitEnforcement(t *testing.T) {
	router := execution.NewPaperRouter(execution.PaperLimits{
		PerMarketExposureCap: 100,
		DailyLossCap:         50,
	})

	if accepted, reason := router.Submit(execution.Order{ID: "o1", Market: "mkt-1", Notional: 80}); !accepted || reason != "" {
		t.Fatalf("expected first order accepted, accepted=%v reason=%q", accepted, reason)
	}

	if accepted, reason := router.Submit(execution.Order{ID: "o2", Market: "mkt-1", Notional: 30}); accepted || reason == "" {
		t.Fatalf("expected second order blocked by exposure cap, accepted=%v reason=%q", accepted, reason)
	}

	router.RecordPnL(-60)
	if !router.TradingHalted() {
		t.Fatal("expected trading to halt after daily loss cap breach")
	}
}
