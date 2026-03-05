package news

import "strings"

func EnrichReliability(item Item) Item {
	if item.Provenance == nil {
		item.Provenance = make(map[string]string)
	}
	item.Provenance["original_source"] = item.SourceName
	item.ReliabilityTier = AssignReliability(item.SourceName)
	return item
}

func AssignReliability(sourceName string) ReliabilityTier {
	source := strings.ToLower(strings.TrimSpace(sourceName))
	switch source {
	case "reuters", "associated press", "ap", "bloomberg":
		return TierA
	case "coindesk", "the block", "decrypt":
		return TierB
	default:
		return TierC
	}
}
