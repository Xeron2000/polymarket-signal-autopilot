package ingestion

import "sort"

type SequenceGap struct {
	From int64
	To   int64
}

func DetectSequenceGaps(events []RawEvent) []SequenceGap {
	if len(events) < 2 {
		return nil
	}

	seq := make([]int64, 0, len(events))
	seen := make(map[int64]struct{}, len(events))
	for _, event := range events {
		if event.Sequence <= 0 {
			continue
		}
		if _, ok := seen[event.Sequence]; ok {
			continue
		}
		seen[event.Sequence] = struct{}{}
		seq = append(seq, event.Sequence)
	}

	if len(seq) < 2 {
		return nil
	}
	sort.Slice(seq, func(i, j int) bool { return seq[i] < seq[j] })

	var gaps []SequenceGap
	prev := seq[0]
	for i := 1; i < len(seq); i++ {
		curr := seq[i]
		if curr-prev > 1 {
			gaps = append(gaps, SequenceGap{From: prev + 1, To: curr - 1})
		}
		prev = curr
	}
	return gaps
}
