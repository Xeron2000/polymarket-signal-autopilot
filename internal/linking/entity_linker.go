package linking

import (
	"fmt"
	"regexp"
	"strings"

	"polymarket-signal/internal/connectors/news"
	"polymarket-signal/internal/ingestion"
)

var tokenSplit = regexp.MustCompile(`[^a-z0-9]+`)

type Link struct {
	ID          string
	MarketEvent ingestion.RawEvent
	NewsItem    news.Item
	Confidence  float64
}

func LinkByKeyword(events []ingestion.RawEvent, items []news.Item) []Link {
	links := make([]Link, 0)
	for _, event := range events {
		eventTokens := tokenize(string(event.Payload))
		if len(eventTokens) == 0 {
			continue
		}
		for _, item := range items {
			titleTokens := tokenize(item.Title)
			if len(titleTokens) == 0 {
				continue
			}
			overlap := tokenOverlap(eventTokens, titleTokens)
			if overlap > 0 {
				confidence := float64(overlap) / float64(max(len(eventTokens), len(titleTokens)))
				if confidence < 0.1 {
					confidence = 0.1
				}
				links = append(links, Link{
					ID:          fmt.Sprintf("%s-%s", event.ID, item.ID),
					MarketEvent: event,
					NewsItem:    item,
					Confidence:  confidence,
				})
			}
		}
	}
	return links
}

func tokenize(text string) map[string]struct{} {
	parts := tokenSplit.Split(strings.ToLower(text), -1)
	out := make(map[string]struct{})
	for _, part := range parts {
		if len(part) < 4 {
			continue
		}
		out[part] = struct{}{}
	}
	return out
}

func tokenOverlap(left map[string]struct{}, right map[string]struct{}) int {
	count := 0
	for token := range left {
		if _, ok := right[token]; ok {
			count++
		}
	}
	return count
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
