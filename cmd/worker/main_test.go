package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"polymarket-signal/internal/strategy"
)

type fakeRuntimeStateStore struct {
	next        int64
	initialized bool
}

func (f *fakeRuntimeStateStore) AllocateRuntimeNonce(_ context.Context, _ string, seed int64) (int64, error) {
	if !f.initialized {
		if f.next < seed {
			f.next = seed
		}
		f.initialized = true
	}
	allocated := f.next
	f.next++
	return allocated, nil
}

func TestLoadExecutionDependenciesFromEnvDynamicBuilderWithoutTemplate(t *testing.T) {
	t.Setenv("POLY_TRADER_ADDRESS", "0x1111111111111111111111111111111111111111")
	t.Setenv("POLY_API_KEY", "api-key-1")
	t.Setenv("POLY_PASSPHRASE", "pass-1")
	t.Setenv("POLY_API_SECRET", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("POLY_PRIVATE_KEY", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	t.Setenv("POLY_ORDER_PRICE", "0.5")
	t.Setenv("POLY_ORDER_SIDE", "BUY")
	t.Setenv("POLY_ORDER_OWNER", "owner-1")
	t.Setenv("EXEC_NOTIONAL", "10")
	t.Setenv("POLY_SIGNED_ORDER_JSON", "")
	t.Setenv("POLY_DYNAMIC_PRICING", "false")

	deps, err := loadExecutionDependenciesFromEnv("US", nil, nil, nil)
	if err != nil {
		t.Fatalf("expected execution dependencies load success, got %v", err)
	}
	if deps.trader == nil {
		t.Fatal("expected trader to be configured")
	}
	if deps.builder == nil {
		t.Fatal("expected order builder to be configured")
	}

	_, request, err := deps.builder(strategy.Signal{ID: "sig-1", AssetID: "101"})
	if err != nil {
		t.Fatalf("expected dynamic builder order success, got %v", err)
	}
	if request.Owner != "owner-1" {
		t.Fatalf("unexpected order owner: %s", request.Owner)
	}
}

func TestLoadExecutionDependenciesFromEnvRejectsInvalidDynamicOrderPrice(t *testing.T) {
	t.Setenv("POLY_TRADER_ADDRESS", "0x1111111111111111111111111111111111111111")
	t.Setenv("POLY_API_KEY", "api-key-1")
	t.Setenv("POLY_PASSPHRASE", "pass-1")
	t.Setenv("POLY_API_SECRET", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("POLY_PRIVATE_KEY", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	t.Setenv("POLY_ORDER_PRICE", "abc")
	t.Setenv("POLY_ORDER_OWNER", "owner-1")
	t.Setenv("EXEC_NOTIONAL", "10")
	t.Setenv("POLY_DYNAMIC_PRICING", "false")

	_, err := loadExecutionDependenciesFromEnv("US", nil, nil, nil)
	if err == nil {
		t.Fatal("expected invalid POLY_ORDER_PRICE to return error")
	}
}

func TestLoadExecutionDependenciesFromEnvFallsBackToTemplateWithoutPrivateKey(t *testing.T) {
	t.Setenv("POLY_TRADER_ADDRESS", "0x1111111111111111111111111111111111111111")
	t.Setenv("POLY_API_KEY", "api-key-1")
	t.Setenv("POLY_PASSPHRASE", "pass-1")
	t.Setenv("POLY_API_SECRET", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("POLY_PRIVATE_KEY", "")
	t.Setenv("POLY_SIGNED_ORDER_JSON", `{"order":{"tokenId":"template-asset","side":"BUY"},"owner":"template-owner","orderType":"GTC"}`)
	t.Setenv("POLY_DYNAMIC_PRICING", "false")

	deps, err := loadExecutionDependenciesFromEnv("US", nil, nil, nil)
	if err != nil {
		t.Fatalf("expected template fallback success, got %v", err)
	}

	_, request, err := deps.builder(strategy.Signal{ID: "sig-1", AssetID: "101"})
	if err != nil {
		t.Fatalf("expected template fallback builder success, got %v", err)
	}
	if request.Owner != "template-owner" {
		t.Fatalf("expected template owner, got %q", request.Owner)
	}
}

func TestLoadExecutionDependenciesFromEnvPrefersDynamicOverTemplateWhenBothSet(t *testing.T) {
	t.Setenv("POLY_TRADER_ADDRESS", "0x1111111111111111111111111111111111111111")
	t.Setenv("POLY_API_KEY", "api-key-1")
	t.Setenv("POLY_PASSPHRASE", "pass-1")
	t.Setenv("POLY_API_SECRET", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("POLY_PRIVATE_KEY", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	t.Setenv("POLY_ORDER_OWNER", "dynamic-owner")
	t.Setenv("POLY_ORDER_PRICE", "0.5")
	t.Setenv("EXEC_NOTIONAL", "10")
	t.Setenv("POLY_SIGNED_ORDER_JSON", `{"order":{"tokenId":"202","side":"BUY"},"owner":"template-owner","orderType":"GTC"}`)
	t.Setenv("POLY_DYNAMIC_PRICING", "false")

	deps, err := loadExecutionDependenciesFromEnv("US", nil, nil, nil)
	if err != nil {
		t.Fatalf("expected dynamic precedence success, got %v", err)
	}

	_, request, err := deps.builder(strategy.Signal{ID: "sig-1", AssetID: "101"})
	if err != nil {
		t.Fatalf("expected dynamic builder success, got %v", err)
	}
	if request.Owner != "dynamic-owner" {
		t.Fatalf("expected dynamic builder to be chosen, got owner %q", request.Owner)
	}
}

func TestLoadExecutionDependenciesFromEnvUsesPersistedNonceState(t *testing.T) {
	t.Setenv("POLY_TRADER_ADDRESS", "0x1111111111111111111111111111111111111111")
	t.Setenv("POLY_API_KEY", "api-key-1")
	t.Setenv("POLY_PASSPHRASE", "pass-1")
	t.Setenv("POLY_API_SECRET", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("POLY_PRIVATE_KEY", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	t.Setenv("POLY_ORDER_PRICE", "0.5")
	t.Setenv("POLY_ORDER_OWNER", "owner-1")
	t.Setenv("POLY_DYNAMIC_PRICING", "false")
	t.Setenv("POLY_ORDER_NONCE", "7")
	t.Setenv("EXEC_NOTIONAL", "10")

	stateStore := &fakeRuntimeStateStore{next: 40, initialized: true}
	deps, err := loadExecutionDependenciesFromEnv("US", stateStore, nil, nil)
	if err != nil {
		t.Fatalf("expected execution dependencies load success, got %v", err)
	}

	_, request, err := deps.builder(strategy.Signal{ID: "sig-1", AssetID: "101"})
	if err != nil {
		t.Fatalf("expected builder success, got %v", err)
	}
	order, ok := request.Order.(map[string]any)
	if !ok {
		t.Fatalf("expected order map payload, got %T", request.Order)
	}
	if fmt.Sprint(order["nonce"]) != "40" {
		t.Fatalf("expected first nonce from persisted state 40, got %v", order["nonce"])
	}

	_, secondRequest, err := deps.builder(strategy.Signal{ID: "sig-2", AssetID: "101"})
	if err != nil {
		t.Fatalf("expected second builder success, got %v", err)
	}
	secondOrder, ok := secondRequest.Order.(map[string]any)
	if !ok {
		t.Fatalf("expected second order map payload, got %T", secondRequest.Order)
	}
	if fmt.Sprint(secondOrder["nonce"]) != "41" {
		t.Fatalf("expected second nonce from atomic allocator 41, got %v", secondOrder["nonce"])
	}

	if stateStore.next != 42 {
		t.Fatalf("expected persisted next nonce 42, got %d", stateStore.next)
	}
}

func TestBuildLiveOrderKeyDeterministicBySignalAndMarket(t *testing.T) {
	keyA := buildLiveOrderKey("sig-100", "tok-1")
	keyB := buildLiveOrderKey("sig-100", "tok-1")
	if keyA != keyB {
		t.Fatalf("expected deterministic order key, got %q vs %q", keyA, keyB)
	}

	otherKey := buildLiveOrderKey("sig-100", "tok-2")
	if otherKey == keyA {
		t.Fatalf("expected different market to produce different key, got %q", otherKey)
	}
}

func TestDefaultStrategyLinkUsesSearchQueryURL(t *testing.T) {
	assetID := "105267568073659068217311993901927962476298440625043565106676088842803600775810"
	want := "https://polymarket.com/search?query=" + url.QueryEscape(assetID)
	if got := defaultStrategyLink(assetID); got != want {
		t.Fatalf("expected search link %q, got %q", want, got)
	}
}

func TestResolveStrategyAssetLinksFallsBackToSearchLink(t *testing.T) {
	assetA := "asset-does-not-exist-a"
	assetB := "asset-does-not-exist-b"
	t.Setenv("ARB_ASSET_A_URL", "")
	t.Setenv("ARB_ASSET_B_URL", "")
	t.Setenv("POLY_GAMMA_URL", "https://gamma-api.polymarket.com")

	links := resolveStrategyAssetLinks(assetA, assetB)
	wantA := "https://polymarket.com/search?query=" + url.QueryEscape(assetA)
	wantB := "https://polymarket.com/search?query=" + url.QueryEscape(assetB)

	if links[assetA] != wantA {
		t.Fatalf("expected asset A fallback search link %q, got %q", wantA, links[assetA])
	}
	if links[assetB] != wantB {
		t.Fatalf("expected asset B fallback search link %q, got %q", wantB, links[assetB])
	}
}

func TestResolveStrategyAssetLinksKeepsConfiguredURLWithoutReachabilityCheck(t *testing.T) {
	assetA := "asset-a"
	assetB := "asset-b"
	configuredA := "https://example.invalid/market-a"
	t.Setenv("ARB_ASSET_A_URL", configuredA)
	t.Setenv("ARB_ASSET_B_URL", "")
	t.Setenv("POLY_GAMMA_URL", "https://gamma-api.polymarket.com")

	links := resolveStrategyAssetLinks(assetA, assetB)
	if links[assetA] != configuredA {
		t.Fatalf("expected configured URL to be kept, got %q", links[assetA])
	}
}

func TestChoosePolymarketEventSlugPrefersEventSlug(t *testing.T) {
	marketSlug := "will-bitcoin-hit-1m-before-gta-vi-872"
	eventSlug := "what-will-happen-before-gta-vi"

	chosen := choosePolymarketEventSlug(
		marketSlug,
		[]gammaMarketEvent{{Slug: eventSlug}},
	)

	if chosen != eventSlug {
		t.Fatalf("expected event slug %q, got %q", eventSlug, chosen)
	}
}

func TestChoosePolymarketEventSlugFallsBackToMarketSlugWhenEventMissing(t *testing.T) {
	marketSlug := "bitboy-convicted"
	chosen := choosePolymarketEventSlug(marketSlug, nil)
	if chosen != marketSlug {
		t.Fatalf("expected market slug fallback %q, got %q", marketSlug, chosen)
	}
}

func TestResolvePolymarketEventURLUsesBrowserHeadersAndMarketSlugFallback(t *testing.T) {
	assetID := "token-id-1"
	marketSlug := "will-bitcoin-hit-1m-before-gta-vi-872"

	gammaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/markets" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("clob_token_ids"); got != assetID {
			t.Fatalf("expected clob_token_ids query %q, got %q", assetID, got)
		}
		if got := r.Header.Get("User-Agent"); !strings.Contains(got, "polymarket-signal-autopilot") {
			http.Error(w, "missing user agent", http.StatusForbidden)
			return
		}
		if got := r.Header.Get("Origin"); got != "https://polymarket.com" {
			http.Error(w, "missing origin", http.StatusForbidden)
			return
		}
		if got := r.Header.Get("Referer"); got != "https://polymarket.com/" {
			http.Error(w, "missing referer", http.StatusForbidden)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`[{"slug":"%s","events":[]}]`, marketSlug)))
	}))
	defer gammaServer.Close()

	got := resolvePolymarketEventURL(gammaServer.URL, assetID)
	want := "https://polymarket.com/event/" + marketSlug
	if got != want {
		t.Fatalf("expected resolved market URL %q, got %q", want, got)
	}
}
