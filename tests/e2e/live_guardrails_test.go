package e2e

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"polymarket-signal/internal/execution"
)

func TestLiveGuardrailsWhitelistIdempotencyStalenessAndKillSwitch(t *testing.T) {
	router := execution.NewLiveRouter(execution.LiveConfig{
		WhitelistMarkets:      map[string]struct{}{"mkt-1": {}, "mkt-3": {}},
		MarketCluster:         map[string]string{"mkt-1": "cluster-a", "mkt-3": "cluster-a"},
		PerClusterExposureCap: map[string]float64{"cluster-a": 100},
		PerMarketExposureCap:  100,
		StaleCutoff:           5 * time.Second,
		DailyLossCap:          50,
		AllowedCountries:      map[string]struct{}{"US": {}},
		ComplianceHook: func(order execution.LiveOrder) error {
			if order.Notional > 90 {
				return errors.New("compliance_limit")
			}
			return nil
		},
	})

	if ok, reason := router.Submit(execution.LiveOrder{Key: "k1", Market: "mkt-2", Country: "US", Notional: 10}, 1*time.Second); ok || reason == "" {
		t.Fatalf("expected non-whitelist market blocked, ok=%v reason=%q", ok, reason)
	}
	if ok, reason := router.Submit(execution.LiveOrder{Key: "k-country", Market: "mkt-1", Country: "IR", Notional: 10}, 1*time.Second); ok || reason == "" {
		t.Fatalf("expected geoblocked country blocked, ok=%v reason=%q", ok, reason)
	}
	if ok, reason := router.Submit(execution.LiveOrder{Key: "k-compliance", Market: "mkt-1", Country: "US", Notional: 95}, 1*time.Second); ok || reason == "" {
		t.Fatalf("expected compliance check blocked, ok=%v reason=%q", ok, reason)
	}

	if ok, reason := router.Submit(execution.LiveOrder{Key: "k2", Market: "mkt-1", Country: "US", Notional: 80}, 1*time.Second); !ok || reason != "" {
		t.Fatalf("expected first live order accepted, ok=%v reason=%q", ok, reason)
	}
	if ok, reason := router.Submit(execution.LiveOrder{Key: "k2b", Market: "mkt-3", Country: "US", Notional: 30}, 1*time.Second); ok || reason == "" {
		t.Fatalf("expected cluster cap to block second market in same cluster, ok=%v reason=%q", ok, reason)
	}

	if ok, reason := router.Submit(execution.LiveOrder{Key: "k2", Market: "mkt-1", Country: "US", Notional: 5}, 1*time.Second); ok || reason == "" {
		t.Fatalf("expected duplicate key blocked, ok=%v reason=%q", ok, reason)
	}

	if ok, reason := router.Submit(execution.LiveOrder{Key: "k3", Market: "mkt-1", Country: "US", Notional: 5}, 6*time.Second); ok || reason == "" {
		t.Fatalf("expected stale-data order blocked, ok=%v reason=%q", ok, reason)
	}

	router.RecordPnL(-60)
	if ok, reason := router.Submit(execution.LiveOrder{Key: "k3b", Market: "mkt-1", Country: "US", Notional: 5}, 1*time.Second); ok || reason == "" {
		t.Fatalf("expected daily-loss-cap halt, ok=%v reason=%q", ok, reason)
	}

	router.KillSwitch().Trigger("manual_drill")
	if ok, reason := router.Submit(execution.LiveOrder{Key: "k4", Market: "mkt-1", Country: "US", Notional: 5}, 1*time.Second); ok || reason == "" {
		t.Fatalf("expected kill-switch blocked order, ok=%v reason=%q", ok, reason)
	}
}

func TestLiveRouterConcurrentSubmitIsSafe(t *testing.T) {
	router := execution.NewLiveRouter(execution.LiveConfig{
		WhitelistMarkets:     map[string]struct{}{"mkt-1": {}},
		PerMarketExposureCap: 100000,
		StaleCutoff:          5 * time.Second,
		AllowedCountries:     map[string]struct{}{"US": {}},
	})

	const workers = 64
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		idx := i
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("kc-%d", idx)
			_, _ = router.Submit(execution.LiveOrder{Key: key, Market: "mkt-1", Country: "US", Notional: 1}, 1*time.Second)
		}()
	}

	wg.Wait()
}
