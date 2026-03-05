package unit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"polymarket-signal/internal/runtime"
)

type counterNotifier struct {
	calls atomic.Int64
}

func (n *counterNotifier) Send(_ context.Context, _ runtime.AlertMessage) (int64, error) {
	n.calls.Add(1)
	return n.calls.Load(), nil
}

type flakyNotifier struct {
	calls      atomic.Int64
	failFirstN int64
}

func (n *flakyNotifier) Send(_ context.Context, _ runtime.AlertMessage) (int64, error) {
	attempt := n.calls.Add(1)
	if attempt <= n.failFirstN {
		return 0, context.DeadlineExceeded
	}
	return attempt, nil
}

func TestWorkerOpsHandlerExposesHealthAndMetrics(t *testing.T) {
	telemetry := runtime.NewAutoPilotTelemetry()
	telemetry.RecordRawEvent()
	telemetry.RecordSignal()
	telemetry.RecordStreamError()
	telemetry.RecordAnomaly(runtime.AnomalyEvent{
		Severity:   runtime.AnomalySeverityHigh,
		AssetID:    "tok1",
		Reason:     "rank_flip_polymarket_leads_news_lag",
		Confidence: 0.93,
		Spread:     0.21,
		Stage:      "signal",
		Action:     "LONG",
		CreatedAt:  time.Now().UTC(),
	})
	telemetry.RecordOrderStatus("failed")
	telemetry.RecordReconcileStatus("success")
	telemetry.RecordNonceAllocation()
	telemetry.RecordPricingMode("dynamic")

	handler := runtime.NewWorkerOpsHandler(telemetry)

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthResp := httptest.NewRecorder()
	handler.ServeHTTP(healthResp, healthReq)

	if healthResp.Code != http.StatusOK {
		t.Fatalf("expected /health 200, got %d", healthResp.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(healthResp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response failed: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", body["status"])
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsResp := httptest.NewRecorder()
	handler.ServeHTTP(metricsResp, metricsReq)

	if metricsResp.Code != http.StatusOK {
		t.Fatalf("expected /metrics 200, got %d", metricsResp.Code)
	}
	metrics := metricsResp.Body.String()
	for _, metricName := range []string{
		"autopilot_raw_events_total",
		"autopilot_signals_total",
		"autopilot_stream_errors_total",
		"autopilot_anomaly_signals_total",
		"autopilot_orders_total",
		"autopilot_reconcile_total",
		"autopilot_nonce_allocations_total",
		"autopilot_pricing_decisions_total",
	} {
		if !strings.Contains(metrics, metricName) {
			t.Fatalf("expected metrics payload to contain %s", metricName)
		}
	}

	panelReq := httptest.NewRequest(http.MethodGet, "/anomaly-panel?limit=5", nil)
	panelResp := httptest.NewRecorder()
	handler.ServeHTTP(panelResp, panelReq)

	if panelResp.Code != http.StatusOK {
		t.Fatalf("expected /anomaly-panel 200, got %d", panelResp.Code)
	}
	var panel map[string]any
	if err := json.Unmarshal(panelResp.Body.Bytes(), &panel); err != nil {
		t.Fatalf("decode panel response failed: %v", err)
	}
	counts, ok := panel["counts"].(map[string]any)
	if !ok {
		t.Fatalf("expected counts map in panel response, got %T", panel["counts"])
	}
	if counts["high"] == nil {
		t.Fatalf("expected high severity count in panel response, got %+v", counts)
	}

	htmlReq := httptest.NewRequest(http.MethodGet, "/anomaly-panel.html", nil)
	htmlResp := httptest.NewRecorder()
	handler.ServeHTTP(htmlResp, htmlReq)
	if htmlResp.Code != http.StatusOK {
		t.Fatalf("expected /anomaly-panel.html 200, got %d", htmlResp.Code)
	}
	if !strings.Contains(htmlResp.Body.String(), "Anomaly Severity Panel") {
		t.Fatal("expected anomaly panel HTML page title")
	}
}

func TestOpsAlertManagerRateLimitsByCooldown(t *testing.T) {
	notifier := &counterNotifier{}
	alerts := runtime.NewOpsAlertManager(notifier, 20*time.Millisecond)

	alerts.Emit(context.Background(), "order_failed", "first")
	alerts.Emit(context.Background(), "order_failed", "second")

	if notifier.calls.Load() != 1 {
		t.Fatalf("expected cooldown to suppress duplicate alert, got %d sends", notifier.calls.Load())
	}

	time.Sleep(25 * time.Millisecond)
	alerts.Emit(context.Background(), "order_failed", "third")

	if notifier.calls.Load() != 2 {
		t.Fatalf("expected alert to send after cooldown, got %d sends", notifier.calls.Load())
	}
}

func TestOpsAlertManagerDoesNotApplyCooldownOnFailedSend(t *testing.T) {
	notifier := &flakyNotifier{failFirstN: 1}
	alerts := runtime.NewOpsAlertManager(notifier, 5*time.Minute)

	alerts.Emit(context.Background(), "order_failed", "first")
	alerts.Emit(context.Background(), "order_failed", "second")

	if notifier.calls.Load() != 2 {
		t.Fatalf("expected second send attempt immediately after failed first send, got %d", notifier.calls.Load())
	}
}
