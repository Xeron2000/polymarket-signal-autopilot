package polymarket

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

type nonceSequence struct {
	next int64
}

func (s *nonceSequence) NextNonce() (int64, error) {
	if s.next < 0 {
		return 0, fmt.Errorf("negative nonce seed")
	}
	nonce := s.next
	s.next++
	return nonce, nil
}

func TestDynamicOrderBuilderBuildRequestBUY(t *testing.T) {
	builder, err := NewDynamicOrderBuilder(DynamicOrderConfig{
		PrivateKeyHex:     "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		Owner:             "owner-1",
		MakerAddress:      "0x1111111111111111111111111111111111111111",
		Price:             0.5,
		Side:              "BUY",
		OrderType:         "GTC",
		FeeRateBps:        0,
		Nonce:             2,
		SignatureType:     0,
		ExpirationSeconds: 0,
		ChainID:           137,
		Now:               func() time.Time { return time.Unix(1700000000, 0).UTC() },
		SaltGenerator:     func() int64 { return 123 },
	})
	if err != nil {
		t.Fatalf("expected dynamic builder create success, got %v", err)
	}

	request, err := builder.BuildRequest("101", 10)
	if err != nil {
		t.Fatalf("expected build request success, got %v", err)
	}

	if request.Owner != "owner-1" {
		t.Fatalf("unexpected owner: %s", request.Owner)
	}
	if request.OrderType != "GTC" {
		t.Fatalf("unexpected order type: %s", request.OrderType)
	}

	order, ok := request.Order.(map[string]any)
	if !ok {
		t.Fatalf("expected request.Order map payload, got %T", request.Order)
	}

	if order["tokenId"] != "101" {
		t.Fatalf("unexpected tokenId: %v", order["tokenId"])
	}
	if _, ok := order["salt"].(string); !ok {
		t.Fatalf("expected salt encoded as string, got %T", order["salt"])
	}
	if order["side"] != "BUY" {
		t.Fatalf("unexpected side: %v", order["side"])
	}
	if order["makerAmount"] != "10000000" {
		t.Fatalf("unexpected makerAmount: %v", order["makerAmount"])
	}
	if order["takerAmount"] != "20000000" {
		t.Fatalf("unexpected takerAmount: %v", order["takerAmount"])
	}
	sig, _ := order["signature"].(string)
	if !strings.HasPrefix(sig, "0x") || len(sig) < 10 {
		t.Fatalf("expected hex signature, got %q", sig)
	}
}

func TestDynamicOrderBuilderBuildRequestSELL(t *testing.T) {
	builder, err := NewDynamicOrderBuilder(DynamicOrderConfig{
		PrivateKeyHex:     "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		Owner:             "owner-2",
		MakerAddress:      "0x2222222222222222222222222222222222222222",
		Price:             0.5,
		Side:              "SELL",
		OrderType:         "GTC",
		FeeRateBps:        30,
		Nonce:             7,
		SignatureType:     1,
		ExpirationSeconds: 3600,
		ChainID:           137,
		Now:               func() time.Time { return time.Unix(1700000000, 0).UTC() },
		SaltGenerator:     func() int64 { return 999 },
	})
	if err != nil {
		t.Fatalf("expected dynamic builder create success, got %v", err)
	}

	request, err := builder.BuildRequest("202", 10)
	if err != nil {
		t.Fatalf("expected build request success, got %v", err)
	}

	order, ok := request.Order.(map[string]any)
	if !ok {
		t.Fatalf("expected request.Order map payload, got %T", request.Order)
	}
	if order["side"] != "SELL" {
		t.Fatalf("unexpected side: %v", order["side"])
	}
	if order["makerAmount"] != "20000000" {
		t.Fatalf("unexpected makerAmount for SELL: %v", order["makerAmount"])
	}
	if order["takerAmount"] != "10000000" {
		t.Fatalf("unexpected takerAmount for SELL: %v", order["takerAmount"])
	}
}

func TestDynamicOrderBuilderUsesNonceProviderAcrossRequests(t *testing.T) {
	nonceProvider := &nonceSequence{next: 77}
	builder, err := NewDynamicOrderBuilder(DynamicOrderConfig{
		PrivateKeyHex:     "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		Owner:             "owner-3",
		MakerAddress:      "0x3333333333333333333333333333333333333333",
		Price:             0.5,
		Side:              "BUY",
		OrderType:         "GTC",
		FeeRateBps:        0,
		NonceProvider:     nonceProvider,
		SignatureType:     0,
		ExpirationSeconds: 600,
		ChainID:           137,
		Now:               func() time.Time { return time.Unix(1700000000, 0).UTC() },
		SaltGenerator:     func() int64 { return 12345 },
	})
	if err != nil {
		t.Fatalf("expected dynamic builder create success, got %v", err)
	}

	first, err := builder.BuildRequest("404", 10)
	if err != nil {
		t.Fatalf("expected first request build success, got %v", err)
	}
	second, err := builder.BuildRequest("404", 10)
	if err != nil {
		t.Fatalf("expected second request build success, got %v", err)
	}

	firstOrder, ok := first.Order.(map[string]any)
	if !ok {
		t.Fatalf("expected first order map payload, got %T", first.Order)
	}
	secondOrder, ok := second.Order.(map[string]any)
	if !ok {
		t.Fatalf("expected second order map payload, got %T", second.Order)
	}

	if firstOrder["nonce"] != "77" {
		t.Fatalf("expected first nonce=77, got %v", firstOrder["nonce"])
	}
	if secondOrder["nonce"] != "78" {
		t.Fatalf("expected second nonce=78, got %v", secondOrder["nonce"])
	}
}

func TestDynamicOrderBuilderBuildRequestWithPriceOverride(t *testing.T) {
	builder, err := NewDynamicOrderBuilder(DynamicOrderConfig{
		PrivateKeyHex:     "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
		Owner:             "owner-4",
		MakerAddress:      "0x4444444444444444444444444444444444444444",
		Price:             0.5,
		Side:              "BUY",
		OrderType:         "GTC",
		FeeRateBps:        0,
		Nonce:             1,
		SignatureType:     0,
		ExpirationSeconds: 600,
		ChainID:           137,
		Now:               func() time.Time { return time.Unix(1700000000, 0).UTC() },
		SaltGenerator:     func() int64 { return 9999 },
	})
	if err != nil {
		t.Fatalf("expected dynamic builder create success, got %v", err)
	}

	request, err := builder.BuildRequestWithPrice("505", 10, 0.25)
	if err != nil {
		t.Fatalf("expected request build with price override success, got %v", err)
	}
	order, ok := request.Order.(map[string]any)
	if !ok {
		t.Fatalf("expected order map payload, got %T", request.Order)
	}

	if order["makerAmount"] != "10000000" {
		t.Fatalf("unexpected makerAmount with override: %v", order["makerAmount"])
	}
	if order["takerAmount"] != "40000000" {
		t.Fatalf("unexpected takerAmount with override: %v", order["takerAmount"])
	}
}
