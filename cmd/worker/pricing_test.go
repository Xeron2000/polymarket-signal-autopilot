package main

import (
	"math"
	"testing"

	"polymarket-signal/internal/connectors/polymarket"
)

func TestSelectLimitPriceUsesOrderBookMidWithOffset(t *testing.T) {
	price, mode, err := selectLimitPrice(polymarket.OrderBook{
		Bids: []polymarket.PriceLevel{{Price: 0.44, Size: 100}},
		Asks: []polymarket.PriceLevel{{Price: 0.46, Size: 120}},
	}, "BUY", 0.50, 25, 0.01, 0.99)
	if err != nil {
		t.Fatalf("expected dynamic pricing success, got %v", err)
	}
	if mode != "dynamic" {
		t.Fatalf("expected dynamic mode, got %s", mode)
	}
	want := 0.451125
	if math.Abs(price-want) > 0.000001 {
		t.Fatalf("unexpected dynamic price, got %.6f want %.6f", price, want)
	}
}

func TestSelectLimitPriceFallsBackWhenOrderBookUnavailable(t *testing.T) {
	price, mode, err := selectLimitPrice(polymarket.OrderBook{}, "SELL", 0.37, 50, 0.01, 0.99)
	if err != nil {
		t.Fatalf("expected fallback pricing success, got %v", err)
	}
	if mode != "fallback" {
		t.Fatalf("expected fallback mode, got %s", mode)
	}
	if price != 0.37 {
		t.Fatalf("expected fallback price 0.37, got %.4f", price)
	}
}

func TestChooseLimitPriceMarksFallbackWhenDynamicBookUnavailable(t *testing.T) {
	price, mode, err := chooseLimitPrice(true, polymarket.OrderBook{}, nil, "BUY", 0.42, 25, 0.01, 0.99)
	if err != nil {
		t.Fatalf("expected chooseLimitPrice success, got %v", err)
	}
	if mode != "fallback" {
		t.Fatalf("expected fallback mode, got %s", mode)
	}
	if price != 0.42 {
		t.Fatalf("expected fallback price 0.42, got %.4f", price)
	}
}

func TestChooseLimitPriceReturnsStaticWhenDynamicDisabled(t *testing.T) {
	price, mode, err := chooseLimitPrice(false, polymarket.OrderBook{}, nil, "BUY", 0.58, 25, 0.01, 0.99)
	if err != nil {
		t.Fatalf("expected chooseLimitPrice success, got %v", err)
	}
	if mode != "static" {
		t.Fatalf("expected static mode, got %s", mode)
	}
	if price != 0.58 {
		t.Fatalf("expected static fallback price 0.58, got %.4f", price)
	}
}
