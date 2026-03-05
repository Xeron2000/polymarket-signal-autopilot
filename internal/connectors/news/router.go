package news

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Router struct {
	sources []Source
}

func NewRouter(sources ...Source) *Router {
	return &Router{sources: sources}
}

func (r *Router) FetchAndNormalize(ctx context.Context, window TimeWindow, ingestTS time.Time) ([]Item, error) {
	items := make([]Item, 0)
	for _, source := range r.sources {
		pulled, err := source.Fetch(ctx, window)
		if err != nil {
			return nil, fmt.Errorf("fetch source %s: %w", source.Name(), err)
		}
		for _, item := range pulled {
			normalized, err := NormalizeItem(item, ingestTS)
			if err != nil {
				return nil, err
			}
			items = append(items, EnrichReliability(normalized))
		}
	}
	return items, nil
}

func NormalizeItem(item Item, ingestTS time.Time) (Item, error) {
	if item.ID == "" {
		return Item{}, fmt.Errorf("news item id is required")
	}
	if ingestTS.IsZero() {
		ingestTS = time.Now().UTC()
	}

	item.Title = strings.TrimSpace(item.Title)
	item.SourceName = strings.TrimSpace(item.SourceName)
	item.PublishedTS = item.PublishedTS.UTC()
	item.FirstSeenIngestTS = ingestTS.UTC()
	if item.Provenance == nil {
		item.Provenance = make(map[string]string)
	}
	return item, nil
}
