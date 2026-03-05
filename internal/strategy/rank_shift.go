package strategy

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"polymarket-signal/internal/connectors/polymarket"
)

type Signal struct {
	ID         string
	AssetID    string
	AssetLabel string
	Action     string
	Reason     string
	Confidence float64
	CreatedAt  time.Time
	Metadata   map[string]any
}

type RankShiftConfig struct {
	AssetA      string
	AssetB      string
	FlipSpread  float64
	Cooldown    time.Duration
	Now         func() time.Time
	TargetLabel string
}

type RankShiftStrategy struct {
	cfg          RankShiftConfig
	Now          func() time.Time
	prices       map[string]float64
	leader       string
	lastSignalAt time.Time
}

func NewRankShiftStrategy(cfg RankShiftConfig) *RankShiftStrategy {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	if cfg.FlipSpread <= 0 {
		cfg.FlipSpread = 0.03
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 3 * time.Minute
	}

	return &RankShiftStrategy{
		cfg:    cfg,
		Now:    now,
		prices: make(map[string]float64),
	}
}

func (s *RankShiftStrategy) OnUpdate(update polymarket.WSMarketUpdate) []Signal {
	if update.AssetID == "" {
		return nil
	}
	if update.AssetID != s.cfg.AssetA && update.AssetID != s.cfg.AssetB {
		return nil
	}

	price, ok := extractPrice(update.Payload)
	if !ok {
		return nil
	}
	s.prices[update.AssetID] = price

	priceA, okA := s.prices[s.cfg.AssetA]
	priceB, okB := s.prices[s.cfg.AssetB]
	if !okA || !okB {
		return nil
	}

	newLeader := s.cfg.AssetA
	if priceB > priceA {
		newLeader = s.cfg.AssetB
	}
	diff := math.Abs(priceA - priceB)

	if s.leader == "" {
		s.leader = newLeader
		return nil
	}

	oldLeader := s.leader
	s.leader = newLeader

	if oldLeader == newLeader || diff < s.cfg.FlipSpread {
		return nil
	}
	now := s.Now().UTC()
	if !s.lastSignalAt.IsZero() && now.Sub(s.lastSignalAt) < s.cfg.Cooldown {
		return nil
	}
	s.lastSignalAt = now

	label := s.cfg.TargetLabel
	if strings.TrimSpace(label) == "" {
		label = oldLeader
	}
	confidence := diff
	if confidence > 1 {
		confidence = 1
	}

	return []Signal{{
		ID:         fmt.Sprintf("rankshift-%d", now.UnixNano()),
		AssetID:    oldLeader,
		AssetLabel: label,
		Action:     "LONG",
		Reason:     "rank_flip_polymarket_leads_news_lag",
		Confidence: confidence,
		CreatedAt:  now,
		Metadata: map[string]any{
			"old_leader": oldLeader,
			"new_leader": newLeader,
			"spread":     diff,
		},
	}}
}

func extractPrice(raw []byte) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, false
	}

	for _, key := range []string{"price", "mid", "best_bid", "bid", "last_trade_price"} {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return typed, true
		case string:
			parsed, err := strconv.ParseFloat(typed, 64)
			if err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}
