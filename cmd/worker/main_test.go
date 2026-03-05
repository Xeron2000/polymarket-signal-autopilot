package main

import (
	"context"
	"fmt"
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
