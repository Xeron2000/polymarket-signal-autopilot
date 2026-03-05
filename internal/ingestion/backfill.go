package ingestion

import (
	"fmt"
	"time"
)

func FillGaps(gaps []SequenceGap, baseTS time.Time) []RawEvent {
	if baseTS.IsZero() {
		baseTS = time.Now().UTC()
	}

	filled := make([]RawEvent, 0)
	for _, gap := range gaps {
		for seq := gap.From; seq <= gap.To; seq++ {
			filled = append(filled, RawEvent{
				ID:           fmt.Sprintf("backfill-%d", seq),
				Sequence:     seq,
				Source:       "backfill",
				Payload:      []byte(`{"closure":true}`),
				SourceTS:     baseTS,
				IngestTS:     baseTS,
				NormalizedTS: baseTS,
			})
		}
	}
	return filled
}
