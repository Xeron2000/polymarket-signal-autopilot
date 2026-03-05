package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"polymarket-signal/internal/assetpicker"
	"polymarket-signal/internal/connectors/polymarket"
)

const defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

type gammaTag struct {
	Label string `json:"label"`
}

type gammaMarket struct {
	ID                string          `json:"id"`
	Question          string          `json:"question"`
	EventID           json.RawMessage `json:"event_id"`
	Volume            json.RawMessage `json:"volume"`
	Liquidity         json.RawMessage `json:"liquidity"`
	Active            bool            `json:"active"`
	Closed            bool            `json:"closed"`
	ClobTokenIDs      json.RawMessage `json:"clobTokenIds"`
	ClobTokenIDsSnake json.RawMessage `json:"clob_token_ids"`
	Events            []gammaEventRef `json:"events"`
	Tags              []gammaTag      `json:"tags"`
}

type gammaEventRef struct {
	ID json.RawMessage `json:"id"`
}

type gammaEvent struct {
	ID   json.RawMessage `json:"id"`
	Tags []gammaTag      `json:"tags"`
}

func main() {
	var (
		gammaURL       = flag.String("gamma-url", polymarket.DefaultGammaBaseURL, "Gamma API base URL")
		pageSize       = flag.Int("page-size", 200, "Markets page size")
		maxFetch       = flag.Int("max-fetch", 800, "Maximum active markets to fetch")
		maxSelected    = flag.Int("max-selected-markets", 12, "Maximum markets after filtering")
		maxAssetIDs    = flag.Int("max-asset-ids", 24, "Maximum token IDs in POLY_ASSET_IDS output")
		tier1Only      = flag.Bool("tier1-only", false, "Only keep tier1 categories after filtering")
		coreCategories = flag.String("core-categories", "Sports,Soccer,Politics", "Tier1 categories")
		coreTopPct     = flag.Float64("core-top-pct", 0.30, "Top percentile for core categories (0~1)")
		otherTopPct    = flag.Float64("other-top-pct", 0.10, "Top percentile for non-core categories (0~1)")
		timeout        = flag.Duration("timeout", 20*time.Second, "HTTP timeout")
	)
	flag.Parse()

	httpClient := &http.Client{Timeout: *timeout}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	eventCategoryByID, err := fetchActiveEventCategories(ctx, httpClient, strings.TrimSpace(*gammaURL), *pageSize, *maxFetch)
	if err != nil {
		log.Printf("fetch active event categories failed, fallback to market tags only: %v", err)
		eventCategoryByID = map[string]string{}
	}

	markets, err := fetchActiveMarkets(ctx, httpClient, strings.TrimSpace(*gammaURL), *pageSize, *maxFetch, eventCategoryByID)
	if err != nil {
		log.Fatalf("fetch active markets failed: %v", err)
	}

	selection := assetpicker.SelectCandidates(markets, assetpicker.SelectionConfig{
		CoreCategories:     splitCSV(*coreCategories),
		CoreTopPercentile:  *coreTopPct,
		OtherTopPercentile: *otherTopPct,
		MaxMarkets:         0,
	})

	if *tier1Only {
		selection = keepTier(selection, "tier1")
	}
	if *maxSelected > 0 && len(selection) > *maxSelected {
		selection = selection[:*maxSelected]
	}

	if len(selection) == 0 {
		log.Printf("no markets matched rule: core-top=%.2f other-top=%.2f", *coreTopPct, *otherTopPct)
		os.Exit(1)
	}

	assetIDs := assetpicker.BuildPOLYAssetIDs(selection, *maxAssetIDs)
	assetA, assetB := assetpicker.SuggestPair(selection)

	printSummary(markets, selection)
	fmt.Println()
	fmt.Printf("POLY_ASSET_IDS=%s\n", strings.Join(assetIDs, ","))
	if assetA != "" && assetB != "" {
		fmt.Printf("ARB_ASSET_A=%s\n", assetA)
		fmt.Printf("ARB_ASSET_B=%s\n", assetB)
	}
	fmt.Println("# 建议: ARB_FLIP_SPREAD 取 max(当前值, 最近窗口 p80(|A-B|))")
}

func printSummary(allMarkets []assetpicker.MarketSnapshot, selected []assetpicker.Candidate) {
	fmt.Printf("fetched_markets=%d selected_markets=%d\n", len(allMarkets), len(selected))
	fmt.Println("selected (top by score):")
	fmt.Println("  tier  category      volume      liquidity   market_id")
	for _, row := range selected {
		fmt.Printf("  %-5s %-12s %-11s %-11s %s\n",
			row.Tier,
			truncate(row.Category, 12),
			compactNumber(row.Volume),
			compactNumber(row.Liquidity),
			truncate(row.ID, 20),
		)
	}
}

func fetchActiveMarkets(ctx context.Context, client *http.Client, gammaBaseURL string, pageSize int, maxFetch int, eventCategoryByID map[string]string) ([]assetpicker.MarketSnapshot, error) {
	if strings.TrimSpace(gammaBaseURL) == "" {
		gammaBaseURL = polymarket.DefaultGammaBaseURL
	}
	if pageSize <= 0 {
		pageSize = 200
	}
	if maxFetch <= 0 {
		maxFetch = 800
	}

	base := strings.TrimRight(gammaBaseURL, "/")
	if maxFetch < pageSize {
		pageSize = maxFetch
	}

	out := make([]assetpicker.MarketSnapshot, 0, maxFetch)
	for offset := 0; offset < maxFetch; offset += pageSize {
		batchLimit := pageSize
		remaining := maxFetch - offset
		if remaining < batchLimit {
			batchLimit = remaining
		}

		batch, err := fetchMarketsPage(ctx, client, base, batchLimit, offset, eventCategoryByID)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}

		out = append(out, batch...)
		if len(batch) < batchLimit {
			break
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Volume != out[j].Volume {
			return out[i].Volume > out[j].Volume
		}
		if out[i].Liquidity != out[j].Liquidity {
			return out[i].Liquidity > out[j].Liquidity
		}
		return out[i].ID < out[j].ID
	})

	return out, nil
}

func fetchMarketsPage(ctx context.Context, client *http.Client, base string, limit int, offset int, eventCategoryByID map[string]string) ([]assetpicker.MarketSnapshot, error) {
	params := url.Values{}
	params.Set("active", "true")
	params.Set("closed", "false")
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(offset))

	endpoint := fmt.Sprintf("%s/markets?%s", base, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request gamma markets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("gamma /markets status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gamma response: %w", err)
	}

	var raw []gammaMarket
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode gamma markets: %w", err)
	}

	out := make([]assetpicker.MarketSnapshot, 0, len(raw))
	for _, market := range raw {
		if market.ID == "" {
			continue
		}
		if !market.Active || market.Closed {
			continue
		}
		volume, okVolume := decodeNumber(market.Volume)
		liquidity, okLiquidity := decodeNumber(market.Liquidity)
		if !okVolume || !okLiquidity {
			continue
		}
		tokens := decodeTokenIDs(market.ClobTokenIDs)
		if len(tokens) == 0 {
			tokens = decodeTokenIDs(market.ClobTokenIDsSnake)
		}
		if len(tokens) == 0 {
			continue
		}

		question := strings.TrimSpace(market.Question)
		if question == "" {
			question = market.ID
		}
		category := "Unknown"
		if len(market.Tags) > 0 {
			if label := strings.TrimSpace(market.Tags[0].Label); label != "" {
				category = label
			}
		}
		if category == "Unknown" {
			eventID := extractEventIDFromMarket(market)
			if eventID != "" {
				if label, ok := eventCategoryByID[eventID]; ok && strings.TrimSpace(label) != "" {
					category = label
				}
			}
		}

		out = append(out, assetpicker.MarketSnapshot{
			ID:        market.ID,
			Question:  question,
			Category:  category,
			Volume:    volume,
			Liquidity: liquidity,
			TokenIDs:  tokens,
		})
	}

	return out, nil
}

func decodeNumber(raw json.RawMessage) (float64, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return 0, false
	}

	var numeric float64
	if err := json.Unmarshal(raw, &numeric); err == nil && !math.IsNaN(numeric) && !math.IsInf(numeric, 0) {
		return numeric, true
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		parsed, parseErr := strconv.ParseFloat(strings.TrimSpace(asString), 64)
		if parseErr == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0) {
			return parsed, true
		}
	}

	return 0, false
}

func decodeTokenIDs(raw json.RawMessage) []string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}

	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return normalizeTokenIDs(list)
	}

	var mixed []any
	if err := json.Unmarshal(raw, &mixed); err == nil {
		out := make([]string, 0, len(mixed))
		for _, entry := range mixed {
			value, ok := entry.(string)
			if ok {
				out = append(out, value)
			}
		}
		return normalizeTokenIDs(out)
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err != nil {
		return nil
	}

	asString = strings.TrimSpace(asString)
	if asString == "" {
		return nil
	}

	if strings.HasPrefix(asString, "[") {
		var embedded []string
		if err := json.Unmarshal([]byte(asString), &embedded); err == nil {
			return normalizeTokenIDs(embedded)
		}
		var embeddedMixed []any
		if err := json.Unmarshal([]byte(asString), &embeddedMixed); err == nil {
			out := make([]string, 0, len(embeddedMixed))
			for _, entry := range embeddedMixed {
				value, ok := entry.(string)
				if ok {
					out = append(out, value)
				}
			}
			return normalizeTokenIDs(out)
		}
	}

	if strings.Contains(asString, ",") {
		return normalizeTokenIDs(strings.Split(asString, ","))
	}
	return normalizeTokenIDs([]string{asString})
}

func fetchActiveEventCategories(ctx context.Context, client *http.Client, gammaBaseURL string, pageSize int, maxFetch int) (map[string]string, error) {
	if strings.TrimSpace(gammaBaseURL) == "" {
		gammaBaseURL = polymarket.DefaultGammaBaseURL
	}
	if pageSize <= 0 {
		pageSize = 200
	}
	if maxFetch <= 0 {
		maxFetch = 800
	}

	base := strings.TrimRight(gammaBaseURL, "/")
	if maxFetch < pageSize {
		pageSize = maxFetch
	}

	eventCategoryByID := make(map[string]string)
	for offset := 0; offset < maxFetch; offset += pageSize {
		batchLimit := pageSize
		remaining := maxFetch - offset
		if remaining < batchLimit {
			batchLimit = remaining
		}

		batch, err := fetchEventsPage(ctx, client, base, batchLimit, offset)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}

		for _, event := range batch {
			if len(event.Tags) == 0 {
				continue
			}
			label := strings.TrimSpace(event.Tags[0].Label)
			if label == "" {
				continue
			}
			eventID := decodeID(event.ID)
			if eventID == "" {
				continue
			}
			eventCategoryByID[eventID] = label
		}

		if len(batch) < batchLimit {
			break
		}
	}

	return eventCategoryByID, nil
}

func fetchEventsPage(ctx context.Context, client *http.Client, base string, limit int, offset int) ([]gammaEvent, error) {
	params := url.Values{}
	params.Set("active", "true")
	params.Set("closed", "false")
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(offset))

	endpoint := fmt.Sprintf("%s/events?%s", base, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", defaultUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request gamma events: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("gamma /events status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gamma events response: %w", err)
	}

	var out []gammaEvent
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode gamma events: %w", err)
	}
	return out, nil
}

func extractEventIDFromMarket(market gammaMarket) string {
	if id := decodeID(market.EventID); id != "" {
		return id
	}
	for _, event := range market.Events {
		if id := decodeID(event.ID); id != "" {
			return id
		}
	}
	return ""
}

func decodeID(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}

	var asNumber json.Number
	if err := json.Unmarshal(raw, &asNumber); err == nil {
		return strings.TrimSpace(asNumber.String())
	}

	var asInt int64
	if err := json.Unmarshal(raw, &asInt); err == nil {
		return strconv.FormatInt(asInt, 10)
	}

	var asFloat float64
	if err := json.Unmarshal(raw, &asFloat); err == nil {
		if math.IsNaN(asFloat) || math.IsInf(asFloat, 0) {
			return ""
		}
		return strconv.FormatInt(int64(asFloat), 10)
	}

	return ""
}

func normalizeTokenIDs(tokenIDs []string) []string {
	if len(tokenIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tokenIDs))
	out := make([]string, 0, len(tokenIDs))
	for _, token := range tokenIDs {
		trimmed := strings.TrimSpace(token)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func keepTier(candidates []assetpicker.Candidate, tier string) []assetpicker.Candidate {
	trimmedTier := strings.TrimSpace(tier)
	if trimmedTier == "" {
		return candidates
	}
	out := make([]assetpicker.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Tier) == trimmedTier {
			out = append(out, candidate)
		}
	}
	return out
}

func compactNumber(value float64) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	switch {
	case value >= 1_000_000_000:
		return fmt.Sprintf("%s%.2fB", sign, value/1_000_000_000)
	case value >= 1_000_000:
		return fmt.Sprintf("%s%.2fM", sign, value/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%s%.2fK", sign, value/1_000)
	default:
		return fmt.Sprintf("%s%.2f", sign, value)
	}
}

func truncate(value string, max int) string {
	trimmed := strings.TrimSpace(value)
	if max <= 0 || len(trimmed) <= max {
		return trimmed
	}
	if max <= 3 {
		return trimmed[:max]
	}
	return trimmed[:max-3] + "..."
}
