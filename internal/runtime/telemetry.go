package runtime

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type AutoPilotTelemetry struct {
	startedAt        time.Time
	lastUpdateUnix   atomic.Int64
	rawEvents        atomic.Int64
	signals          atomic.Int64
	streamErrors     atomic.Int64
	nonceAllocations atomic.Int64

	mu            sync.Mutex
	orders        map[string]int64
	reconcile     map[string]int64
	pricing       map[string]int64
	anomalies     map[string]int64
	anomalyRecent []AnomalyEvent
}

func NewAutoPilotTelemetry() *AutoPilotTelemetry {
	now := time.Now().UTC()
	t := &AutoPilotTelemetry{
		startedAt: now,
		orders:    make(map[string]int64),
		reconcile: make(map[string]int64),
		pricing:   make(map[string]int64),
		anomalies: map[string]int64{
			string(AnomalySeverityLow):    0,
			string(AnomalySeverityMedium): 0,
			string(AnomalySeverityHigh):   0,
		},
		anomalyRecent: make([]AnomalyEvent, 0, 64),
	}
	t.lastUpdateUnix.Store(now.Unix())
	return t
}

func (t *AutoPilotTelemetry) RecordRawEvent() {
	t.rawEvents.Add(1)
	t.lastUpdateUnix.Store(time.Now().UTC().Unix())
}

func (t *AutoPilotTelemetry) RecordSignal() {
	t.signals.Add(1)
	t.lastUpdateUnix.Store(time.Now().UTC().Unix())
}

func (t *AutoPilotTelemetry) RecordStreamError() {
	t.streamErrors.Add(1)
	t.lastUpdateUnix.Store(time.Now().UTC().Unix())
}

func (t *AutoPilotTelemetry) RecordOrderStatus(status string) {
	t.incrementMapCounter(t.orders, status)
	t.lastUpdateUnix.Store(time.Now().UTC().Unix())
}

func (t *AutoPilotTelemetry) RecordReconcileStatus(status string) {
	t.incrementMapCounter(t.reconcile, status)
	t.lastUpdateUnix.Store(time.Now().UTC().Unix())
}

func (t *AutoPilotTelemetry) RecordNonceAllocation() {
	t.nonceAllocations.Add(1)
	t.lastUpdateUnix.Store(time.Now().UTC().Unix())
}

func (t *AutoPilotTelemetry) RecordPricingMode(mode string) {
	t.incrementMapCounter(t.pricing, mode)
	t.lastUpdateUnix.Store(time.Now().UTC().Unix())
}

func (t *AutoPilotTelemetry) RecordAnomaly(event AnomalyEvent) {
	if t == nil {
		return
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	severity := normalizeAnomalySeverity(string(event.Severity))
	if severity == "" {
		severity = AnomalySeverityLow
	}
	event.Severity = severity
	event.Stage = strings.TrimSpace(event.Stage)
	if event.Stage == "" {
		event.Stage = "signal"
	}

	t.mu.Lock()
	t.anomalies[string(severity)]++
	t.anomalyRecent = append(t.anomalyRecent, event)
	if len(t.anomalyRecent) > 200 {
		t.anomalyRecent = append([]AnomalyEvent(nil), t.anomalyRecent[len(t.anomalyRecent)-200:]...)
	}
	t.mu.Unlock()
	t.lastUpdateUnix.Store(time.Now().UTC().Unix())
}

func (t *AutoPilotTelemetry) incrementMapCounter(target map[string]int64, raw string) {
	if t == nil {
		return
	}
	status := normalizeMetricLabel(raw)
	t.mu.Lock()
	target[status]++
	t.mu.Unlock()
}

func (t *AutoPilotTelemetry) HealthSnapshot() map[string]any {
	if t == nil {
		return map[string]any{"status": "ok"}
	}
	now := time.Now().UTC()
	return map[string]any{
		"status":           "ok",
		"started_at":       t.startedAt.Format(time.RFC3339),
		"uptime_seconds":   int64(now.Sub(t.startedAt).Seconds()),
		"last_update_unix": t.lastUpdateUnix.Load(),
	}
}

func (t *AutoPilotTelemetry) RenderPrometheus() string {
	if t == nil {
		return ""
	}
	b := strings.Builder{}
	b.WriteString(fmt.Sprintf("autopilot_raw_events_total %d\n", t.rawEvents.Load()))
	b.WriteString(fmt.Sprintf("autopilot_signals_total %d\n", t.signals.Load()))
	b.WriteString(fmt.Sprintf("autopilot_stream_errors_total %d\n", t.streamErrors.Load()))
	b.WriteString(fmt.Sprintf("autopilot_nonce_allocations_total %d\n", t.nonceAllocations.Load()))

	t.mu.Lock()
	orders := cloneMetricMap(t.orders)
	reconcile := cloneMetricMap(t.reconcile)
	pricing := cloneMetricMap(t.pricing)
	anomalies := cloneMetricMap(t.anomalies)
	t.mu.Unlock()

	appendLabeledMetric(&b, "autopilot_orders_total", "status", orders)
	appendLabeledMetric(&b, "autopilot_reconcile_total", "status", reconcile)
	appendLabeledMetric(&b, "autopilot_pricing_decisions_total", "status", pricing)
	appendLabeledMetric(&b, "autopilot_anomaly_signals_total", "severity", anomalies)
	return b.String()
}

func (t *AutoPilotTelemetry) AnomalyPanelSnapshot(limit int) map[string]any {
	if t == nil {
		return map[string]any{
			"generated_at": time.Now().UTC().Format(time.RFC3339),
			"counts": map[string]int64{
				string(AnomalySeverityLow):    0,
				string(AnomalySeverityMedium): 0,
				string(AnomalySeverityHigh):   0,
			},
			"recent": []AnomalyEvent{},
		}
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	t.mu.Lock()
	counts := cloneMetricMap(t.anomalies)
	recentCopy := append([]AnomalyEvent(nil), t.anomalyRecent...)
	t.mu.Unlock()

	recent := make([]AnomalyEvent, 0, limit)
	for i := len(recentCopy) - 1; i >= 0 && len(recent) < limit; i-- {
		recent = append(recent, recentCopy[i])
	}

	return map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"counts": map[string]int64{
			string(AnomalySeverityLow):    counts[string(AnomalySeverityLow)],
			string(AnomalySeverityMedium): counts[string(AnomalySeverityMedium)],
			string(AnomalySeverityHigh):   counts[string(AnomalySeverityHigh)],
		},
		"recent": recent,
	}
}

func (t *AutoPilotTelemetry) RenderAnomalyPanelHTML(limit int) string {
	panel := t.AnomalyPanelSnapshot(limit)
	counts, _ := panel["counts"].(map[string]int64)
	recent, _ := panel["recent"].([]AnomalyEvent)

	b := strings.Builder{}
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Anomaly Severity Panel</title>")
	b.WriteString("<style>body{font-family:Arial,sans-serif;margin:24px;} .cards{display:flex;gap:12px;margin-bottom:16px;} .card{padding:12px 16px;border-radius:8px;color:#fff;min-width:120px;} .low{background:#2d8cf0;} .medium{background:#f0a020;} .high{background:#e74c3c;} table{border-collapse:collapse;width:100%;} th,td{border:1px solid #ddd;padding:8px;} th{background:#fafafa;text-align:left;}</style></head><body>")
	b.WriteString("<h1>Anomaly Severity Panel</h1>")
	b.WriteString("<div class=\"cards\">")
	b.WriteString(fmt.Sprintf("<div class=\"card low\"><div>LOW</div><div>%d</div></div>", counts[string(AnomalySeverityLow)]))
	b.WriteString(fmt.Sprintf("<div class=\"card medium\"><div>MEDIUM</div><div>%d</div></div>", counts[string(AnomalySeverityMedium)]))
	b.WriteString(fmt.Sprintf("<div class=\"card high\"><div>HIGH</div><div>%d</div></div>", counts[string(AnomalySeverityHigh)]))
	b.WriteString("</div>")
	b.WriteString("<table><thead><tr><th>Time</th><th>Severity</th><th>Asset</th><th>Reason</th><th>Spread</th><th>Confidence</th><th>Stage</th></tr></thead><tbody>")
	for _, event := range recent {
		b.WriteString("<tr>")
		b.WriteString(fmt.Sprintf("<td>%s</td>", event.CreatedAt.UTC().Format(time.RFC3339)))
		b.WriteString(fmt.Sprintf("<td>%s</td>", event.Severity))
		b.WriteString(fmt.Sprintf("<td>%s</td>", event.AssetID))
		b.WriteString(fmt.Sprintf("<td>%s</td>", event.Reason))
		b.WriteString(fmt.Sprintf("<td>%.4f</td>", event.Spread))
		b.WriteString(fmt.Sprintf("<td>%.4f</td>", event.Confidence))
		b.WriteString(fmt.Sprintf("<td>%s</td>", event.Stage))
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table></body></html>")
	return b.String()
}

func cloneMetricMap(source map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func appendLabeledMetric(builder *strings.Builder, name string, label string, values map[string]int64) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString(fmt.Sprintf("%s{%s=\"%s\"} %d\n", name, label, key, values[key]))
	}
}

func normalizeMetricLabel(raw string) string {
	label := strings.ToLower(strings.TrimSpace(raw))
	if label == "" {
		return "unknown"
	}
	label = strings.ReplaceAll(label, " ", "_")
	label = strings.ReplaceAll(label, "-", "_")
	return label
}

func NewWorkerOpsHandler(telemetry *AutoPilotTelemetry) http.Handler {
	if telemetry == nil {
		telemetry = NewAutoPilotTelemetry()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, telemetry.HealthSnapshot())
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(telemetry.RenderPrometheus()))
	})
	mux.HandleFunc("/anomaly-panel", func(w http.ResponseWriter, r *http.Request) {
		limit := parsePanelLimit(r.URL.Query().Get("limit"), 20)
		writeJSON(w, http.StatusOK, telemetry.AnomalyPanelSnapshot(limit))
	})
	mux.HandleFunc("/anomaly-panel.html", func(w http.ResponseWriter, r *http.Request) {
		limit := parsePanelLimit(r.URL.Query().Get("limit"), 20)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(telemetry.RenderAnomalyPanelHTML(limit)))
	})
	return mux
}

func parsePanelLimit(raw string, fallback int) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	if parsed > 200 {
		return 200
	}
	return parsed
}

type OpsAlertManager struct {
	notifier AlertNotifier
	cooldown time.Duration

	mu       sync.Mutex
	lastSent map[string]time.Time
}

func NewOpsAlertManager(notifier AlertNotifier, cooldown time.Duration) *OpsAlertManager {
	if notifier == nil {
		return nil
	}
	if cooldown <= 0 {
		cooldown = 2 * time.Minute
	}
	return &OpsAlertManager{
		notifier: notifier,
		cooldown: cooldown,
		lastSent: make(map[string]time.Time),
	}
}

func (m *OpsAlertManager) Emit(ctx context.Context, key string, text string) {
	if m == nil || m.notifier == nil {
		return
	}
	now := time.Now().UTC()
	rateKey := normalizeMetricLabel(key)

	m.mu.Lock()
	if last, ok := m.lastSent[rateKey]; ok && now.Sub(last) < m.cooldown {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	_, err := m.notifier.Send(ctx, AlertMessage{
		Target: rateKey,
		Text:   text,
	})
	if err != nil {
		return
	}

	m.mu.Lock()
	m.lastSent[rateKey] = now
	m.mu.Unlock()
}
