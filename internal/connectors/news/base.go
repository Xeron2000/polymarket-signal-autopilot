package news

import (
	"context"
	"time"
)

type ReliabilityTier string

const (
	TierA ReliabilityTier = "A"
	TierB ReliabilityTier = "B"
	TierC ReliabilityTier = "C"
)

type Item struct {
	ID                string
	Title             string
	Body              string
	SourceName        string
	SourceURL         string
	PublishedTS       time.Time
	FirstSeenIngestTS time.Time
	ReliabilityTier   ReliabilityTier
	Provenance        map[string]string
}

type TimeWindow struct {
	From time.Time
	To   time.Time
}

type Source interface {
	Name() string
	Fetch(ctx context.Context, window TimeWindow) ([]Item, error)
}
