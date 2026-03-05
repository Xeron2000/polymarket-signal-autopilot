package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"polymarket-signal/internal/ingestion"
)

type ReplayResult struct {
	OrderedIDs []string
	Checksum   string
}

func ReplayDeterministic(events []ingestion.RawEvent) ReplayResult {
	cloned := make([]ingestion.RawEvent, len(events))
	copy(cloned, events)

	sort.Slice(cloned, func(i, j int) bool {
		if cloned[i].Sequence == cloned[j].Sequence {
			return cloned[i].ID < cloned[j].ID
		}
		return cloned[i].Sequence < cloned[j].Sequence
	})

	ids := make([]string, 0, len(cloned))
	parts := make([]string, 0, len(cloned))
	for _, event := range cloned {
		ids = append(ids, event.ID)
		parts = append(parts,
			event.ID+":"+
				strconv.FormatInt(event.Sequence, 10)+":"+
				event.Source+":"+
				strconv.FormatInt(event.SourceTS.UnixNano(), 10)+":"+
				strconv.FormatInt(event.IngestTS.UnixNano(), 10)+":"+
				strconv.FormatInt(event.NormalizedTS.UnixNano(), 10)+":"+
				string(event.Payload),
		)
	}

	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return ReplayResult{
		OrderedIDs: ids,
		Checksum:   hex.EncodeToString(h[:]),
	}
}
