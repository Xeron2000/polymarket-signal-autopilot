package linking

import (
	"time"

	"polymarket-signal/internal/connectors/news"
)

func EarliestFirstSeen(items []news.Item) time.Time {
	if len(items) == 0 {
		return time.Time{}
	}
	earliest := items[0].FirstSeenIngestTS
	for _, item := range items[1:] {
		if item.FirstSeenIngestTS.Before(earliest) {
			earliest = item.FirstSeenIngestTS
		}
	}
	return earliest
}
