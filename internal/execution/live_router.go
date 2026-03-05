package execution

import (
	"sync"
	"time"
)

type LiveConfig struct {
	WhitelistMarkets      map[string]struct{}
	MarketCluster         map[string]string
	PerClusterExposureCap map[string]float64
	PerMarketExposureCap  float64
	StaleCutoff           time.Duration
	DailyLossCap          float64
	AllowedCountries      map[string]struct{}
	ComplianceHook        func(order LiveOrder) error
}

type LiveOrder struct {
	Key      string
	Market   string
	Country  string
	Notional float64
}

type LiveRouter struct {
	mu              sync.Mutex
	cfg             LiveConfig
	seenKeys        map[string]struct{}
	exposure        map[string]float64
	clusterExposure map[string]float64
	dayPnL          float64
	kill            *KillSwitch
}

func NewLiveRouter(cfg LiveConfig) *LiveRouter {
	return &LiveRouter{
		cfg:             cfg,
		seenKeys:        make(map[string]struct{}),
		exposure:        make(map[string]float64),
		clusterExposure: make(map[string]float64),
		kill:            NewKillSwitch(),
	}
}

func (r *LiveRouter) KillSwitch() *KillSwitch {
	return r.kill
}

func (r *LiveRouter) Submit(order LiveOrder, dataAge time.Duration) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if active, reason := r.kill.Active(); active {
		if reason == "" {
			reason = "kill_switch"
		}
		return false, reason
	}
	if r.cfg.DailyLossCap > 0 && -r.dayPnL > r.cfg.DailyLossCap {
		return false, "daily_loss_cap"
	}

	if order.Key == "" || order.Market == "" || order.Country == "" || order.Notional <= 0 {
		return false, "invalid_live_order"
	}
	if _, allowed := r.cfg.WhitelistMarkets[order.Market]; !allowed {
		return false, "market_not_whitelisted"
	}
	if len(r.cfg.AllowedCountries) > 0 {
		if _, allowed := r.cfg.AllowedCountries[order.Country]; !allowed {
			return false, "geoblock_compliance"
		}
	}
	if r.cfg.ComplianceHook != nil {
		if err := r.cfg.ComplianceHook(order); err != nil {
			return false, "compliance_failed"
		}
	}
	if r.cfg.StaleCutoff > 0 && dataAge > r.cfg.StaleCutoff {
		return false, "stale_data_cutoff"
	}
	if _, seen := r.seenKeys[order.Key]; seen {
		return false, "duplicate_order_key"
	}

	nextExposure := r.exposure[order.Market] + order.Notional
	if r.cfg.PerMarketExposureCap > 0 && nextExposure > r.cfg.PerMarketExposureCap {
		return false, "market_exposure_cap"
	}

	cluster := order.Market
	if mapped, ok := r.cfg.MarketCluster[order.Market]; ok && mapped != "" {
		cluster = mapped
	}
	nextClusterExposure := r.clusterExposure[cluster] + order.Notional
	if capValue, ok := r.cfg.PerClusterExposureCap[cluster]; ok && capValue > 0 && nextClusterExposure > capValue {
		return false, "cluster_exposure_cap"
	}

	r.exposure[order.Market] = nextExposure
	r.clusterExposure[cluster] = nextClusterExposure
	r.seenKeys[order.Key] = struct{}{}
	return true, ""
}

func (r *LiveRouter) RecordPnL(pnl float64) {
	r.mu.Lock()
	r.dayPnL += pnl
	exceeded := r.cfg.DailyLossCap > 0 && -r.dayPnL > r.cfg.DailyLossCap
	r.mu.Unlock()

	if exceeded {
		r.kill.Trigger("daily_loss_cap")
	}
}
