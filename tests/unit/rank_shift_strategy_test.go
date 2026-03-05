package unit

import (
	"testing"
	"time"

	"polymarket-signal/internal/connectors/polymarket"
	"polymarket-signal/internal/strategy"
)

func TestRankShiftStrategyEmitsLeadLagSignalOnFlip(t *testing.T) {
	base := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	st := strategy.NewRankShiftStrategy(strategy.RankShiftConfig{
		AssetA:      "meteora",
		AssetB:      "axiom",
		FlipSpread:  0.05,
		Cooldown:    5 * time.Minute,
		Now:         func() time.Time { return base },
		TargetLabel: "MET",
	})

	if signals := st.OnUpdate(polymarket.WSMarketUpdate{AssetID: "meteora", Type: "book", Payload: []byte(`{"price":0.62}`)}); len(signals) != 0 {
		t.Fatalf("expected no signal before both assets observed, got %+v", signals)
	}
	if signals := st.OnUpdate(polymarket.WSMarketUpdate{AssetID: "axiom", Type: "book", Payload: []byte(`{"price":0.51}`)}); len(signals) != 0 {
		t.Fatalf("expected no signal on initial ranking, got %+v", signals)
	}

	st.Now = func() time.Time { return base.Add(1 * time.Minute) }
	signals := st.OnUpdate(polymarket.WSMarketUpdate{AssetID: "axiom", Type: "book", Payload: []byte(`{"price":0.70}`)})
	if len(signals) != 1 {
		t.Fatalf("expected one signal on rank flip, got %+v", signals)
	}
	if signals[0].Action != "LONG" || signals[0].AssetLabel != "MET" {
		t.Fatalf("unexpected signal payload: %+v", signals[0])
	}
}

func TestRankShiftStrategyCooldownSuppressesDuplicateSignals(t *testing.T) {
	base := time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)
	st := strategy.NewRankShiftStrategy(strategy.RankShiftConfig{
		AssetA:      "meteora",
		AssetB:      "axiom",
		FlipSpread:  0.05,
		Cooldown:    5 * time.Minute,
		Now:         func() time.Time { return base },
		TargetLabel: "MET",
	})

	_ = st.OnUpdate(polymarket.WSMarketUpdate{AssetID: "meteora", Type: "book", Payload: []byte(`{"price":0.62}`)})
	_ = st.OnUpdate(polymarket.WSMarketUpdate{AssetID: "axiom", Type: "book", Payload: []byte(`{"price":0.51}`)})

	st.Now = func() time.Time { return base.Add(1 * time.Minute) }
	first := st.OnUpdate(polymarket.WSMarketUpdate{AssetID: "axiom", Type: "book", Payload: []byte(`{"price":0.71}`)})
	if len(first) != 1 {
		t.Fatalf("expected first flip signal, got %+v", first)
	}

	st.Now = func() time.Time { return base.Add(2 * time.Minute) }
	second := st.OnUpdate(polymarket.WSMarketUpdate{AssetID: "meteora", Type: "book", Payload: []byte(`{"price":0.80}`)})
	if len(second) != 0 {
		t.Fatalf("expected cooldown to suppress second signal, got %+v", second)
	}
}
