package risk

import (
	"polymarket-signal/internal/signals"
	"time"
)

type Context struct {
	CurrentExposure float64
	MaxExposure     float64
	DataAge         time.Duration
	StaleAfter      time.Duration
}

type FilteredSignal struct {
	signals.Signal
	Blocked     bool
	BlockReason string
}

func Apply(signal signals.Signal, ctx Context) FilteredSignal {
	if ctx.MaxExposure > 0 && ctx.CurrentExposure > ctx.MaxExposure {
		return FilteredSignal{
			Signal:      signal,
			Blocked:     true,
			BlockReason: "exposure_cap_exceeded",
		}
	}

	if ctx.StaleAfter > 0 && ctx.DataAge > ctx.StaleAfter {
		return FilteredSignal{
			Signal:      signal,
			Blocked:     true,
			BlockReason: "stale_data_cutoff",
		}
	}

	return FilteredSignal{Signal: signal}
}
