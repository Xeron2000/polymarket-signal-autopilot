package execution

type PaperLimits struct {
	PerMarketExposureCap float64
	DailyLossCap         float64
}

type Order struct {
	ID       string
	Market   string
	Notional float64
}

type PaperRouter struct {
	limits   PaperLimits
	exposure map[string]float64
	dayPnL   float64
	halted   bool
}

func NewPaperRouter(limits PaperLimits) *PaperRouter {
	return &PaperRouter{
		limits:   limits,
		exposure: make(map[string]float64),
	}
}

func (r *PaperRouter) Submit(order Order) (bool, string) {
	if r.halted {
		return false, "trading_halted"
	}
	if order.ID == "" || order.Market == "" || order.Notional <= 0 {
		return false, "invalid_order"
	}

	nextExposure := r.exposure[order.Market] + order.Notional
	if r.limits.PerMarketExposureCap > 0 && nextExposure > r.limits.PerMarketExposureCap {
		return false, "market_exposure_cap"
	}

	r.exposure[order.Market] = nextExposure
	return true, ""
}

func (r *PaperRouter) RecordPnL(pnl float64) {
	r.dayPnL += pnl
	if r.limits.DailyLossCap > 0 && -r.dayPnL > r.limits.DailyLossCap {
		r.halted = true
	}
}

func (r *PaperRouter) TradingHalted() bool {
	return r.halted
}
