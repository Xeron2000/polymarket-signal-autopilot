package main

import (
	"fmt"
	"math"
	"strings"

	"polymarket-signal/internal/connectors/polymarket"
)

func chooseLimitPrice(
	dynamicEnabled bool,
	book polymarket.OrderBook,
	bookErr error,
	side string,
	fallback float64,
	offsetBps float64,
	minPrice float64,
	maxPrice float64,
) (float64, string, error) {
	if !dynamicEnabled {
		price, err := clampProbabilityPrice(fallback, minPrice, maxPrice)
		if err != nil {
			return 0, "", err
		}
		return price, "static", nil
	}
	if bookErr != nil {
		price, err := clampProbabilityPrice(fallback, minPrice, maxPrice)
		if err != nil {
			return 0, "", err
		}
		return price, "fallback", nil
	}
	price, mode, err := selectLimitPrice(book, side, fallback, offsetBps, minPrice, maxPrice)
	if err != nil {
		return 0, "", err
	}
	return price, mode, nil
}

func selectLimitPrice(
	book polymarket.OrderBook,
	side string,
	fallback float64,
	offsetBps float64,
	minPrice float64,
	maxPrice float64,
) (float64, string, error) {
	safeFallback, err := clampProbabilityPrice(fallback, minPrice, maxPrice)
	if err != nil {
		return 0, "", err
	}

	bestBid, hasBid := bestBidPrice(book.Bids)
	bestAsk, hasAsk := bestAskPrice(book.Asks)
	if !hasBid && !hasAsk {
		return safeFallback, "fallback", nil
	}

	mid := 0.0
	switch {
	case hasBid && hasAsk:
		mid = (bestBid + bestAsk) / 2
	case hasBid:
		mid = bestBid
	default:
		mid = bestAsk
	}

	adjustment := offsetBps / 10000
	computed := mid
	if strings.EqualFold(strings.TrimSpace(side), "SELL") {
		computed = mid * (1 - adjustment)
	} else {
		computed = mid * (1 + adjustment)
	}

	clamped, err := clampProbabilityPrice(computed, minPrice, maxPrice)
	if err != nil {
		return safeFallback, "fallback", nil
	}
	return clamped, "dynamic", nil
}

func bestBidPrice(levels []polymarket.PriceLevel) (float64, bool) {
	best := 0.0
	found := false
	for _, level := range levels {
		if !isFiniteProbability(level.Price) {
			continue
		}
		if !found || level.Price > best {
			best = level.Price
			found = true
		}
	}
	return best, found
}

func bestAskPrice(levels []polymarket.PriceLevel) (float64, bool) {
	best := 0.0
	found := false
	for _, level := range levels {
		if !isFiniteProbability(level.Price) {
			continue
		}
		if !found || level.Price < best {
			best = level.Price
			found = true
		}
	}
	return best, found
}

func clampProbabilityPrice(price float64, minPrice float64, maxPrice float64) (float64, error) {
	if !isFiniteProbability(price) {
		return 0, fmt.Errorf("price must be finite and within (0,1), got %.8f", price)
	}
	min := minPrice
	max := maxPrice
	if !isFiniteProbability(min) {
		min = 0.01
	}
	if !isFiniteProbability(max) {
		max = 0.99
	}
	if min >= max {
		return 0, fmt.Errorf("invalid price clamp range min=%.6f max=%.6f", min, max)
	}
	if price < min {
		return min, nil
	}
	if price > max {
		return max, nil
	}
	return price, nil
}

func isFiniteProbability(price float64) bool {
	return !math.IsNaN(price) && !math.IsInf(price, 0) && price > 0 && price < 1
}
