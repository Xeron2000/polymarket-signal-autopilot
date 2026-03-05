package integration

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"polymarket-signal/internal/connectors/polymarket"
	"polymarket-signal/internal/execution"
	"polymarket-signal/internal/ingestion"
	"polymarket-signal/internal/persistence"
	"polymarket-signal/internal/runtime"
	"polymarket-signal/internal/strategy"
)

type testStream struct {
	update polymarket.WSMarketUpdate
}

func (s *testStream) Stream(ctx context.Context, _ []string) (<-chan polymarket.WSMarketUpdate, <-chan error) {
	updates := make(chan polymarket.WSMarketUpdate, 1)
	errs := make(chan error, 1)
	updates <- s.update
	close(updates)
	close(errs)
	return updates, errs
}

type testStore struct {
	mu            sync.Mutex
	rawCount      int
	signalCount   int
	orderCount    int
	notifyCount   int
	lastOrderStat string
	orderStatuses []string
	pendingOrders []persistence.PendingOrderRecord
}

func (s *testStore) Write(_ ingestion.RawEvent) (bool, error) {
	s.mu.Lock()
	s.rawCount++
	s.mu.Unlock()
	return true, nil
}
func (s *testStore) SaveSignal(_ persistence.SignalRecord) error {
	s.mu.Lock()
	s.signalCount++
	s.mu.Unlock()
	return nil
}
func (s *testStore) SaveOrder(record persistence.OrderRecord) error {
	s.mu.Lock()
	s.orderCount++
	s.lastOrderStat = record.Status
	s.orderStatuses = append(s.orderStatuses, record.Status)
	s.mu.Unlock()
	return nil
}
func (s *testStore) SaveNotification(_ persistence.NotificationRecord) error {
	s.mu.Lock()
	s.notifyCount++
	s.mu.Unlock()
	return nil
}

func (s *testStore) hasOrderStatus(status string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.orderStatuses {
		if existing == status {
			return true
		}
	}
	return false
}

func (s *testStore) ListPendingOrderReconciliations(limit int) ([]persistence.PendingOrderRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > len(s.pendingOrders) {
		limit = len(s.pendingOrders)
	}
	out := make([]persistence.PendingOrderRecord, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, s.pendingOrders[i])
	}
	return out, nil
}

type fixedStrategy struct{}

func (fixedStrategy) OnUpdate(_ polymarket.WSMarketUpdate) []strategy.Signal {
	return []strategy.Signal{{
		ID:         "sig-1",
		AssetID:    "tok1",
		AssetLabel: "MET",
		Action:     "LONG",
		Reason:     "rank_flip_polymarket_leads_news_lag",
		Confidence: 0.8,
		CreatedAt:  time.Now().UTC(),
	}}
}

type fixedNotifier struct{}

func (fixedNotifier) Send(_ context.Context, _ runtime.AlertMessage) (int64, error) {
	return 301, nil
}

type fixedTrader struct{}

func (fixedTrader) PostOrder(_ context.Context, _ polymarket.PostOrderRequest) (polymarket.PostOrderResponse, error) {
	return polymarket.PostOrderResponse{Success: true, OrderID: "0xorder-1", Status: "live"}, nil
}

type rejectingTrader struct{}

func (rejectingTrader) PostOrder(_ context.Context, _ polymarket.PostOrderRequest) (polymarket.PostOrderResponse, error) {
	return polymarket.PostOrderResponse{Success: false, ErrorMsg: "rejected_by_matching_engine"}, nil
}

type reconcilingTrader struct {
	reconcileStatus string
}

func (t *reconcilingTrader) PostOrder(_ context.Context, _ polymarket.PostOrderRequest) (polymarket.PostOrderResponse, error) {
	return polymarket.PostOrderResponse{Success: true, OrderID: "0xorder-reconcile", Status: "live"}, nil
}

func (t *reconcilingTrader) GetOrder(_ context.Context, orderID string) (polymarket.OpenOrder, error) {
	return polymarket.OpenOrder{ID: orderID, Status: t.reconcileStatus}, nil
}

type idleStream struct{}

func (idleStream) Stream(ctx context.Context, _ []string) (<-chan polymarket.WSMarketUpdate, <-chan error) {
	updates := make(chan polymarket.WSMarketUpdate)
	errs := make(chan error)
	go func() {
		<-ctx.Done()
		close(updates)
		close(errs)
	}()
	return updates, errs
}

type collectingNotifier struct {
	mu       sync.Mutex
	messages []string
}

func (n *collectingNotifier) Send(_ context.Context, message runtime.AlertMessage) (int64, error) {
	n.mu.Lock()
	n.messages = append(n.messages, message.Text)
	id := int64(len(n.messages))
	n.mu.Unlock()
	return id, nil
}

func (n *collectingNotifier) contains(sub string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, message := range n.messages {
		if strings.Contains(message, sub) {
			return true
		}
	}
	return false
}

type pendingReconcileTrader struct{}

func (pendingReconcileTrader) PostOrder(_ context.Context, _ polymarket.PostOrderRequest) (polymarket.PostOrderResponse, error) {
	return polymarket.PostOrderResponse{Success: true, OrderID: "0xorder-pending", Status: "live"}, nil
}

func (pendingReconcileTrader) GetOrder(_ context.Context, orderID string) (polymarket.OpenOrder, error) {
	return polymarket.OpenOrder{ID: orderID, Status: "live"}, nil
}

type errorOnlyStream struct {
	err error
}

func (s errorOnlyStream) Stream(_ context.Context, _ []string) (<-chan polymarket.WSMarketUpdate, <-chan error) {
	updates := make(chan polymarket.WSMarketUpdate)
	errs := make(chan error, 1)
	errs <- s.err
	close(updates)
	close(errs)
	return updates, errs
}

type severityStrategy struct {
	spread float64
}

func (s severityStrategy) OnUpdate(_ polymarket.WSMarketUpdate) []strategy.Signal {
	return []strategy.Signal{{
		ID:         "sig-severity-1",
		AssetID:    "tok1",
		AssetLabel: "TOK1",
		Action:     "LONG",
		Reason:     "rank_flip_polymarket_leads_news_lag",
		Confidence: s.spread,
		CreatedAt:  time.Now().UTC(),
		Metadata: map[string]any{
			"spread": s.spread,
		},
	}}
}

func TestAutoPilotPipelineSignalNotifyExecute(t *testing.T) {
	store := &testStore{}
	router := execution.NewLiveRouter(execution.LiveConfig{
		WhitelistMarkets: map[string]struct{}{"tok1": {}},
		AllowedCountries: map[string]struct{}{"US": {}},
	})

	autopilot := runtime.NewAutoPilot(runtime.AutoPilotConfig{
		Stream:      &testStream{update: polymarket.WSMarketUpdate{AssetID: "tok1", Type: "book", Payload: []byte(`{"price":0.55}`)}},
		Store:       store,
		Strategy:    fixedStrategy{},
		Notifier:    fixedNotifier{},
		LiveRouter:  router,
		Trader:      fixedTrader{},
		AutoExecute: true,
		Country:     "US",
		OrderBuilder: func(signal strategy.Signal) (execution.LiveOrder, polymarket.PostOrderRequest, error) {
			return execution.LiveOrder{Key: "k-1", Market: signal.AssetID, Country: "US", Notional: 10}, polymarket.PostOrderRequest{
				Order: map[string]any{"tokenId": signal.AssetID, "side": "BUY"},
				Owner: "owner-1",
			}, nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := autopilot.Run(ctx, []string{"tok1"}); err != nil {
		t.Fatalf("expected autopilot run success, got %v", err)
	}

	if store.rawCount == 0 {
		t.Fatal("expected raw event persisted")
	}
	if store.signalCount == 0 {
		t.Fatal("expected signal persisted")
	}
	if store.notifyCount == 0 {
		t.Fatal("expected notification persisted")
	}
	if store.orderCount == 0 || store.lastOrderStat == "" {
		t.Fatal("expected order record persisted")
	}
}

func TestAutoPilotPipelineOrderRejectedPersistsFailedAndContinues(t *testing.T) {
	store := &testStore{}
	router := execution.NewLiveRouter(execution.LiveConfig{
		WhitelistMarkets: map[string]struct{}{"tok1": {}},
		AllowedCountries: map[string]struct{}{"US": {}},
	})

	autopilot := runtime.NewAutoPilot(runtime.AutoPilotConfig{
		Stream:      &testStream{update: polymarket.WSMarketUpdate{AssetID: "tok1", Type: "book", Payload: []byte(`{"price":0.55}`)}},
		Store:       store,
		Strategy:    fixedStrategy{},
		Notifier:    fixedNotifier{},
		LiveRouter:  router,
		Trader:      rejectingTrader{},
		AutoExecute: true,
		Country:     "US",
		OrderBuilder: func(signal strategy.Signal) (execution.LiveOrder, polymarket.PostOrderRequest, error) {
			return execution.LiveOrder{Key: "k-1", Market: signal.AssetID, Country: "US", Notional: 10}, polymarket.PostOrderRequest{
				Order: map[string]any{"tokenId": signal.AssetID, "side": "BUY"},
				Owner: "owner-1",
			}, nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := autopilot.Run(ctx, []string{"tok1"}); err != nil {
		t.Fatalf("expected autopilot to continue on order rejection, got %v", err)
	}

	if store.orderCount == 0 {
		t.Fatal("expected rejected order to be persisted")
	}
	if store.lastOrderStat != "failed" {
		t.Fatalf("expected failed order status on rejection, got %q", store.lastOrderStat)
	}
}

func TestAutoPilotPipelineReconcilesOrderStatusAfterSubmission(t *testing.T) {
	store := &testStore{}
	router := execution.NewLiveRouter(execution.LiveConfig{
		WhitelistMarkets: map[string]struct{}{"tok1": {}},
		AllowedCountries: map[string]struct{}{"US": {}},
	})

	trader := &reconcilingTrader{reconcileStatus: "filled"}
	autopilot := runtime.NewAutoPilot(runtime.AutoPilotConfig{
		Stream:      &testStream{update: polymarket.WSMarketUpdate{AssetID: "tok1", Type: "book", Payload: []byte(`{"price":0.55}`)}},
		Store:       store,
		Strategy:    fixedStrategy{},
		Notifier:    fixedNotifier{},
		LiveRouter:  router,
		Trader:      trader,
		AutoExecute: true,
		Country:     "US",
		OrderBuilder: func(signal strategy.Signal) (execution.LiveOrder, polymarket.PostOrderRequest, error) {
			return execution.LiveOrder{Key: "k-1", Market: signal.AssetID, Country: "US", Notional: 10}, polymarket.PostOrderRequest{
				Order: map[string]any{"tokenId": signal.AssetID, "side": "BUY"},
				Owner: "owner-1",
			}, nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := autopilot.Run(ctx, []string{"tok1"}); err != nil {
		t.Fatalf("expected autopilot run success, got %v", err)
	}

	if !store.hasOrderStatus("live") {
		t.Fatal("expected initial submitted/live order status persisted")
	}
	if !store.hasOrderStatus("filled") {
		t.Fatal("expected reconciled terminal status persisted")
	}
}

func TestAutoPilotPeriodicReconcileSweepPersistsTerminalStatus(t *testing.T) {
	store := &testStore{
		pendingOrders: []persistence.PendingOrderRecord{{
			SignalID: "sig-legacy-1",
			OrderID:  "0xorder-stale-1",
			Status:   "live",
		}},
	}
	trader := &reconcilingTrader{reconcileStatus: "filled"}
	autopilot := runtime.NewAutoPilot(runtime.AutoPilotConfig{
		Stream:                 idleStream{},
		Store:                  store,
		Strategy:               fixedStrategy{},
		Notifier:               fixedNotifier{},
		Trader:                 trader,
		AutoExecute:            false,
		ReconcileMaxAttempts:   1,
		ReconcileSweepInterval: 10 * time.Millisecond,
		ReconcileSweepLimit:    10,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	if err := autopilot.Run(ctx, []string{"tok1"}); err != nil {
		t.Fatalf("expected autopilot run success, got %v", err)
	}

	if !store.hasOrderStatus("filled") {
		t.Fatal("expected periodic reconcile sweep to persist filled status")
	}
}

func TestAutoPilotReconcilePendingEmitsOpsAlert(t *testing.T) {
	store := &testStore{}
	notifier := &collectingNotifier{}
	router := execution.NewLiveRouter(execution.LiveConfig{
		WhitelistMarkets: map[string]struct{}{"tok1": {}},
		AllowedCountries: map[string]struct{}{"US": {}},
	})

	autopilot := runtime.NewAutoPilot(runtime.AutoPilotConfig{
		Stream:      &testStream{update: polymarket.WSMarketUpdate{AssetID: "tok1", Type: "book", Payload: []byte(`{"price":0.55}`)}},
		Store:       store,
		Strategy:    fixedStrategy{},
		Notifier:    notifier,
		OpsAlerts:   runtime.NewOpsAlertManager(notifier, time.Minute),
		LiveRouter:  router,
		Trader:      pendingReconcileTrader{},
		AutoExecute: true,
		Country:     "US",
		OrderBuilder: func(signal strategy.Signal) (execution.LiveOrder, polymarket.PostOrderRequest, error) {
			return execution.LiveOrder{Key: "k-1", Market: signal.AssetID, Country: "US", Notional: 10}, polymarket.PostOrderRequest{
				Order: map[string]any{"tokenId": signal.AssetID, "side": "BUY"},
				Owner: "owner-1",
			}, nil
		},
		ReconcileMaxAttempts: 1,
		ReconcileBaseDelay:   5 * time.Millisecond,
		ReconcileMaxDelay:    5 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := autopilot.Run(ctx, []string{"tok1"}); err != nil {
		t.Fatalf("expected autopilot run success, got %v", err)
	}

	if !store.hasOrderStatus("reconcile_pending") {
		t.Fatal("expected reconcile_pending status persisted")
	}
	if !notifier.contains("order reconcile pending") {
		t.Fatal("expected reconcile_pending ops alert message")
	}
}

func TestAutoPilotStreamErrorEmitsOpsAlert(t *testing.T) {
	store := &testStore{}
	notifier := &collectingNotifier{}
	autopilot := runtime.NewAutoPilot(runtime.AutoPilotConfig{
		Stream:    errorOnlyStream{err: fmt.Errorf("ws temporary disconnect")},
		Store:     store,
		Strategy:  fixedStrategy{},
		Notifier:  notifier,
		OpsAlerts: runtime.NewOpsAlertManager(notifier, time.Minute),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	if err := autopilot.Run(ctx, []string{"tok1"}); err != nil {
		t.Fatalf("expected autopilot run success, got %v", err)
	}

	if !notifier.contains("market stream error") {
		t.Fatal("expected ops alert for market stream error")
	}
}

func TestAutoPilotAnomalySeverityHighRecordedAndAlerted(t *testing.T) {
	store := &testStore{}
	notifier := &collectingNotifier{}
	telemetry := runtime.NewAutoPilotTelemetry()
	autopilot := runtime.NewAutoPilot(runtime.AutoPilotConfig{
		Stream:                  &testStream{update: polymarket.WSMarketUpdate{AssetID: "tok1", Type: "book", Payload: []byte(`{"price":0.55}`)}},
		Store:                   store,
		Strategy:                severityStrategy{spread: 0.21},
		Notifier:                notifier,
		Telemetry:               telemetry,
		OpsAlerts:               runtime.NewOpsAlertManager(notifier, time.Minute),
		AnomalySeverityMedium:   0.08,
		AnomalySeverityHigh:     0.15,
		AnomalyAlertMinSeverity: runtime.AnomalySeverityMedium,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := autopilot.Run(ctx, []string{"tok1"}); err != nil {
		t.Fatalf("expected autopilot run success, got %v", err)
	}

	metrics := telemetry.RenderPrometheus()
	if !strings.Contains(metrics, `autopilot_anomaly_signals_total{severity="high"} 1`) {
		t.Fatalf("expected high anomaly metric recorded, got metrics:\n%s", metrics)
	}
	if !notifier.contains("anomaly severity alert") {
		t.Fatal("expected graded anomaly ops alert message")
	}
}

func TestAutoPilotAnomalySeverityLowDoesNotAlertWhenMinIsMedium(t *testing.T) {
	store := &testStore{}
	notifier := &collectingNotifier{}
	autopilot := runtime.NewAutoPilot(runtime.AutoPilotConfig{
		Stream:                  &testStream{update: polymarket.WSMarketUpdate{AssetID: "tok1", Type: "book", Payload: []byte(`{"price":0.55}`)}},
		Store:                   store,
		Strategy:                severityStrategy{spread: 0.06},
		Notifier:                notifier,
		Telemetry:               runtime.NewAutoPilotTelemetry(),
		OpsAlerts:               runtime.NewOpsAlertManager(notifier, time.Minute),
		AnomalySeverityMedium:   0.08,
		AnomalySeverityHigh:     0.15,
		AnomalyAlertMinSeverity: runtime.AnomalySeverityMedium,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := autopilot.Run(ctx, []string{"tok1"}); err != nil {
		t.Fatalf("expected autopilot run success, got %v", err)
	}

	if notifier.contains("anomaly severity alert") {
		t.Fatal("expected low anomaly not to trigger graded ops alert when min severity is medium")
	}
}
