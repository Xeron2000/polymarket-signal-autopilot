package assetpicker

import (
	"math"
	"sort"
	"strings"
)

type MarketSnapshot struct {
	ID        string
	Question  string
	Category  string
	Volume    float64
	Liquidity float64
	TokenIDs  []string
}

type SelectionConfig struct {
	CoreCategories     []string
	CoreTopPercentile  float64
	OtherTopPercentile float64
	MaxMarkets         int
}

type Candidate struct {
	ID             string
	Question       string
	Category       string
	Tier           string
	Volume         float64
	Liquidity      float64
	VolumeRank     float64
	LiquidityRank  float64
	Score          float64
	TokenIDs       []string
	MarketTopPct   float64
	VolumeFloor    float64
	LiquidityFloor float64
}

func SelectCandidates(markets []MarketSnapshot, cfg SelectionConfig) []Candidate {
	config := withSelectionDefaults(cfg)
	coreSet := make(map[string]struct{}, len(config.CoreCategories))
	for _, category := range config.CoreCategories {
		normalized := normalizeCategory(category)
		if normalized != "" {
			coreSet[normalized] = struct{}{}
		}
	}

	valid := make([]MarketSnapshot, 0, len(markets))
	volumes := make([]float64, 0, len(markets))
	liquidities := make([]float64, 0, len(markets))
	for _, market := range markets {
		if market.Volume <= 0 || market.Liquidity <= 0 {
			continue
		}
		tokens := normalizeTokenIDs(market.TokenIDs)
		if len(tokens) == 0 {
			continue
		}
		market.TokenIDs = tokens
		valid = append(valid, market)
		volumes = append(volumes, market.Volume)
		liquidities = append(liquidities, market.Liquidity)
	}

	if len(valid) == 0 {
		return nil
	}

	coreVolumeFloor := percentileFloor(volumes, 1-config.CoreTopPercentile)
	coreLiquidityFloor := percentileFloor(liquidities, 1-config.CoreTopPercentile)
	otherVolumeFloor := percentileFloor(volumes, 1-config.OtherTopPercentile)
	otherLiquidityFloor := percentileFloor(liquidities, 1-config.OtherTopPercentile)

	candidates := make([]Candidate, 0, len(valid))
	for _, market := range valid {
		normalizedCategory := normalizeCategory(market.Category)
		_, isCore := coreSet[normalizedCategory]

		requiredTopPct := config.OtherTopPercentile
		volFloor := otherVolumeFloor
		liqFloor := otherLiquidityFloor
		tier := "tier3"
		if isCore {
			requiredTopPct = config.CoreTopPercentile
			volFloor = coreVolumeFloor
			liqFloor = coreLiquidityFloor
			tier = "tier1"
		}

		if market.Volume < volFloor || market.Liquidity < liqFloor {
			continue
		}

		volumeRank := percentileRank(volumes, market.Volume)
		liquidityRank := percentileRank(liquidities, market.Liquidity)
		candidates = append(candidates, Candidate{
			ID:             market.ID,
			Question:       market.Question,
			Category:       market.Category,
			Tier:           tier,
			Volume:         market.Volume,
			Liquidity:      market.Liquidity,
			VolumeRank:     volumeRank,
			LiquidityRank:  liquidityRank,
			Score:          volumeRank + liquidityRank,
			TokenIDs:       market.TokenIDs,
			MarketTopPct:   requiredTopPct,
			VolumeFloor:    volFloor,
			LiquidityFloor: liqFloor,
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].Volume != candidates[j].Volume {
			return candidates[i].Volume > candidates[j].Volume
		}
		if candidates[i].Liquidity != candidates[j].Liquidity {
			return candidates[i].Liquidity > candidates[j].Liquidity
		}
		return candidates[i].ID < candidates[j].ID
	})

	if config.MaxMarkets > 0 && len(candidates) > config.MaxMarkets {
		candidates = candidates[:config.MaxMarkets]
	}

	return candidates
}

func BuildPOLYAssetIDs(candidates []Candidate, maxIDs int) []string {
	if maxIDs == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	assetIDs := make([]string, 0)
	for _, candidate := range candidates {
		for _, token := range candidate.TokenIDs {
			trimmed := strings.TrimSpace(token)
			if trimmed == "" {
				continue
			}
			if _, exists := seen[trimmed]; exists {
				continue
			}
			seen[trimmed] = struct{}{}
			assetIDs = append(assetIDs, trimmed)
			if maxIDs > 0 && len(assetIDs) >= maxIDs {
				return assetIDs
			}
		}
	}
	return assetIDs
}

func SuggestPair(candidates []Candidate) (string, string) {
	for i := 0; i < len(candidates); i++ {
		if len(candidates[i].TokenIDs) == 0 {
			continue
		}
		for j := i + 1; j < len(candidates); j++ {
			if len(candidates[j].TokenIDs) == 0 {
				continue
			}
			if normalizeCategory(candidates[i].Category) != normalizeCategory(candidates[j].Category) {
				continue
			}
			assetA := strings.TrimSpace(candidates[i].TokenIDs[0])
			assetB := strings.TrimSpace(candidates[j].TokenIDs[0])
			if assetA == "" || assetB == "" || assetA == assetB {
				continue
			}
			return assetA, assetB
		}
	}

	assetIDs := BuildPOLYAssetIDs(candidates, 2)
	if len(assetIDs) == 2 {
		return assetIDs[0], assetIDs[1]
	}
	return "", ""
}

func withSelectionDefaults(cfg SelectionConfig) SelectionConfig {
	if len(cfg.CoreCategories) == 0 {
		cfg.CoreCategories = []string{"Sports", "Soccer", "Politics"}
	}
	if cfg.CoreTopPercentile <= 0 || cfg.CoreTopPercentile > 1 {
		cfg.CoreTopPercentile = 0.30
	}
	if cfg.OtherTopPercentile <= 0 || cfg.OtherTopPercentile > 1 {
		cfg.OtherTopPercentile = 0.10
	}
	return cfg
}

func normalizeTokenIDs(tokenIDs []string) []string {
	if len(tokenIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tokenIDs))
	normalized := make([]string, 0, len(tokenIDs))
	for _, token := range tokenIDs {
		trimmed := strings.TrimSpace(token)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func normalizeCategory(category string) string {
	return strings.ToLower(strings.TrimSpace(category))
}

func percentileFloor(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if q <= 0 {
		q = 0
	}
	if q >= 1 {
		q = 1
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := int(math.Floor(q * float64(len(sorted))))
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func percentileRank(values []float64, value float64) float64 {
	if len(values) == 0 {
		return 0
	}
	count := 0
	for _, entry := range values {
		if entry <= value {
			count++
		}
	}
	return float64(count) / float64(len(values))
}
