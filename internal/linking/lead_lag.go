package linking

import "time"

type Direction string

const (
	DirectionNewsLeads   Direction = "news_leads"
	DirectionMarketLeads Direction = "market_leads"
	DirectionConcurrent  Direction = "concurrent"
)

type LeadLagDelta struct {
	LinkID          string
	DeltaSource     time.Duration
	DeltaIngest     time.Duration
	DirectionSource Direction
	DirectionIngest Direction
}

func ComputeDualClockDelta(link Link) LeadLagDelta {
	deltaSource := link.NewsItem.PublishedTS.Sub(link.MarketEvent.SourceTS)
	deltaIngest := link.NewsItem.FirstSeenIngestTS.Sub(link.MarketEvent.IngestTS)

	return LeadLagDelta{
		LinkID:          link.ID,
		DeltaSource:     deltaSource,
		DeltaIngest:     deltaIngest,
		DirectionSource: directionFromDelta(deltaSource),
		DirectionIngest: directionFromDelta(deltaIngest),
	}
}

func DirectionConsistent(delta LeadLagDelta) bool {
	if delta.DirectionSource == DirectionConcurrent || delta.DirectionIngest == DirectionConcurrent {
		return false
	}
	return delta.DirectionSource == delta.DirectionIngest
}

func directionFromDelta(delta time.Duration) Direction {
	if delta < 0 {
		return DirectionNewsLeads
	}
	if delta > 0 {
		return DirectionMarketLeads
	}
	return DirectionConcurrent
}
