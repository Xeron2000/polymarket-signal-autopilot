package unit

import (
	"testing"

	"polymarket-signal/internal/assetpicker"
)

func TestSelectCandidatesAppliesTierThresholds(t *testing.T) {
	markets := []assetpicker.MarketSnapshot{
		{ID: "sports-10", Category: "Sports", Volume: 10, Liquidity: 10, TokenIDs: []string{"s10"}},
		{ID: "sports-9", Category: "Sports", Volume: 9, Liquidity: 9, TokenIDs: []string{"s9"}},
		{ID: "sports-8", Category: "Sports", Volume: 8, Liquidity: 8, TokenIDs: []string{"s8"}},
		{ID: "sports-7", Category: "Sports", Volume: 7, Liquidity: 7, TokenIDs: []string{"s7"}},
		{ID: "crypto-6", Category: "Crypto", Volume: 6, Liquidity: 6, TokenIDs: []string{"c6"}},
		{ID: "crypto-5", Category: "Crypto", Volume: 5, Liquidity: 5, TokenIDs: []string{"c5"}},
		{ID: "crypto-4", Category: "Crypto", Volume: 4, Liquidity: 4, TokenIDs: []string{"c4"}},
		{ID: "culture-3", Category: "Culture", Volume: 3, Liquidity: 3, TokenIDs: []string{"u3"}},
		{ID: "culture-2", Category: "Culture", Volume: 2, Liquidity: 2, TokenIDs: []string{"u2"}},
		{ID: "culture-1", Category: "Culture", Volume: 1, Liquidity: 1, TokenIDs: []string{"u1"}},
	}

	selected := assetpicker.SelectCandidates(markets, assetpicker.SelectionConfig{
		CoreCategories:     []string{"Sports", "Soccer", "Politics"},
		CoreTopPercentile:  0.30,
		OtherTopPercentile: 0.10,
	})

	got := make(map[string]struct{}, len(selected))
	for _, row := range selected {
		got[row.ID] = struct{}{}
	}

	expected := []string{"sports-10", "sports-9", "sports-8"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d selected markets, got %d (%v)", len(expected), len(got), selected)
	}
	for _, id := range expected {
		if _, ok := got[id]; !ok {
			t.Fatalf("expected selected id %s not found (got=%v)", id, selected)
		}
	}
}

func TestBuildPOLYAssetIDsAndSuggestPair(t *testing.T) {
	candidates := []assetpicker.Candidate{
		{ID: "m1", Category: "Sports", Score: 0.9, TokenIDs: []string{"a1", "a2"}},
		{ID: "m2", Category: "Sports", Score: 0.8, TokenIDs: []string{"b1", "b2"}},
		{ID: "m3", Category: "Politics", Score: 0.7, TokenIDs: []string{"c1", "c2"}},
	}

	assetIDs := assetpicker.BuildPOLYAssetIDs(candidates, 4)
	expectedAssetIDs := []string{"a1", "a2", "b1", "b2"}
	if len(assetIDs) != len(expectedAssetIDs) {
		t.Fatalf("expected %d asset ids, got %d (%v)", len(expectedAssetIDs), len(assetIDs), assetIDs)
	}
	for i := range expectedAssetIDs {
		if assetIDs[i] != expectedAssetIDs[i] {
			t.Fatalf("unexpected asset ids order: want %v got %v", expectedAssetIDs, assetIDs)
		}
	}

	assetA, assetB := assetpicker.SuggestPair(candidates)
	if assetA != "a1" || assetB != "b1" {
		t.Fatalf("expected same-category pair a1/b1, got %s/%s", assetA, assetB)
	}
}
