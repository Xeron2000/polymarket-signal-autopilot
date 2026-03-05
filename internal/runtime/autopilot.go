package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"polymarket-signal/internal/connectors/polymarket"
	"polymarket-signal/internal/execution"
	"polymarket-signal/internal/ingestion"
	"polymarket-signal/internal/persistence"
	"polymarket-signal/internal/strategy"
)

type AlertMessage struct {
	Target string
	Text   string
}

type AlertNotifier interface {
	Send(ctx context.Context, message AlertMessage) (int64, error)
}

type OrderTrader interface {
	PostOrder(ctx context.Context, request polymarket.PostOrderRequest) (polymarket.PostOrderResponse, error)
}

type OrderStatusReader interface {
	GetOrder(ctx context.Context, orderID string) (polymarket.OpenOrder, error)
}

type SignalStrategy interface {
	OnUpdate(update polymarket.WSMarketUpdate) []strategy.Signal
}

type AutoPilotStore interface {
	Write(event ingestion.RawEvent) (bool, error)
	SaveSignal(record persistence.SignalRecord) error
	SaveOrder(record persistence.OrderRecord) error
	SaveNotification(record persistence.NotificationRecord) error
}

type ReconcileScanStore interface {
	ListPendingOrderReconciliations(limit int) ([]persistence.PendingOrderRecord, error)
}

type OrderBuilder func(signal strategy.Signal) (execution.LiveOrder, polymarket.PostOrderRequest, error)

type AutoPilotConfig struct {
	Stream       MarketStream
	Store        AutoPilotStore
	Strategy     SignalStrategy
	Notifier     AlertNotifier
	Telemetry    *AutoPilotTelemetry
	OpsAlerts    *OpsAlertManager
	LiveRouter   *execution.LiveRouter
	Trader       OrderTrader
	AutoExecute  bool
	Country      string
	OrderBuilder OrderBuilder

	ReconcileMaxAttempts   int
	ReconcileBaseDelay     time.Duration
	ReconcileMaxDelay      time.Duration
	ReconcileSweepInterval time.Duration
	ReconcileSweepLimit    int

	AnomalySeverityMedium   float64
	AnomalySeverityHigh     float64
	AnomalyAlertMinSeverity AnomalySeverity
}

type AutoPilot struct {
	cfg AutoPilotConfig
}

func NewAutoPilot(cfg AutoPilotConfig) *AutoPilot {
	if cfg.Telemetry == nil {
		cfg.Telemetry = NewAutoPilotTelemetry()
	}
	if cfg.ReconcileMaxAttempts <= 0 {
		cfg.ReconcileMaxAttempts = 3
	}
	if cfg.ReconcileBaseDelay <= 0 {
		cfg.ReconcileBaseDelay = 300 * time.Millisecond
	}
	if cfg.ReconcileMaxDelay <= 0 {
		cfg.ReconcileMaxDelay = 2 * time.Second
	}
	if cfg.ReconcileSweepInterval <= 0 {
		cfg.ReconcileSweepInterval = 15 * time.Second
	}
	if cfg.ReconcileSweepLimit <= 0 {
		cfg.ReconcileSweepLimit = 50
	}
	if cfg.AnomalySeverityMedium <= 0 {
		cfg.AnomalySeverityMedium = 0.08
	}
	if cfg.AnomalySeverityHigh <= cfg.AnomalySeverityMedium {
		cfg.AnomalySeverityHigh = 0.15
		if cfg.AnomalySeverityHigh <= cfg.AnomalySeverityMedium {
			cfg.AnomalySeverityHigh = cfg.AnomalySeverityMedium + 0.01
		}
	}
	if normalizeAnomalySeverity(string(cfg.AnomalyAlertMinSeverity)) == "" {
		cfg.AnomalyAlertMinSeverity = AnomalySeverityMedium
	}
	return &AutoPilot{cfg: cfg}
}

func (a *AutoPilot) Run(ctx context.Context, assetIDs []string) error {
	if a.cfg.Stream == nil {
		return fmt.Errorf("autopilot stream is required")
	}
	if a.cfg.Store == nil {
		return fmt.Errorf("autopilot store is required")
	}
	if a.cfg.Strategy == nil {
		return fmt.Errorf("autopilot strategy is required")
	}

	updates, errs := a.cfg.Stream.Stream(ctx, assetIDs)
	reconcileTicker := time.NewTicker(a.cfg.ReconcileSweepInterval)
	defer reconcileTicker.Stop()

	for updates != nil || errs != nil {
		select {
		case <-ctx.Done():
			return nil
		case <-reconcileTicker.C:
			if err := a.reconcilePendingOrders(ctx); err != nil {
				return err
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil && ctx.Err() == nil {
				a.cfg.Telemetry.RecordStreamError()
				a.cfg.OpsAlerts.Emit(ctx, "market_stream_error", fmt.Sprintf("⚠️ market stream error\nerror=%s", err.Error()))
				continue
			}
		case update, ok := <-updates:
			if !ok {
				updates = nil
				continue
			}
			if err := a.handleUpdate(ctx, update); err != nil {
				return err
			}
		}
	}

	return nil
}

func (a *AutoPilot) reconcilePendingOrders(ctx context.Context) error {
	scanStore, ok := a.cfg.Store.(ReconcileScanStore)
	if !ok {
		return nil
	}
	reader, ok := a.cfg.Trader.(OrderStatusReader)
	if !ok || reader == nil {
		return nil
	}
	pending, err := scanStore.ListPendingOrderReconciliations(a.cfg.ReconcileSweepLimit)
	if err != nil {
		a.cfg.Telemetry.RecordReconcileStatus("scan_error")
		a.cfg.OpsAlerts.Emit(ctx, "order_reconcile_scan_error", fmt.Sprintf("⚠️ order reconcile scan failed\nerror=%s", err.Error()))
		return nil
	}
	for _, entry := range pending {
		signal := strategy.Signal{ID: entry.SignalID, AssetID: entry.OrderID}
		if reconcileErr := a.reconcileOrder(ctx, signal, entry.OrderID, entry.Status); reconcileErr != nil {
			return reconcileErr
		}
	}
	return nil
}

func (a *AutoPilot) handleUpdate(ctx context.Context, update polymarket.WSMarketUpdate) error {
	now := time.Now().UTC()
	raw := ingestion.RawEvent{
		ID:           fmt.Sprintf("%s-%s-%d", update.AssetID, update.Type, now.UnixNano()),
		Source:       "polymarket-ws",
		Payload:      append([]byte(nil), update.Payload...),
		SourceTS:     now,
		IngestTS:     now,
		NormalizedTS: now,
	}
	if _, err := a.cfg.Store.Write(raw); err != nil {
		return fmt.Errorf("persist raw update: %w", err)
	}
	a.cfg.Telemetry.RecordRawEvent()

	signals := a.cfg.Strategy.OnUpdate(update)
	for _, signal := range signals {
		anomaly := classifySignalAnomaly(signal, a.cfg.AnomalySeverityMedium, a.cfg.AnomalySeverityHigh)
		signal = withSignalSeverityMetadata(signal, anomaly)
		a.cfg.Telemetry.RecordAnomaly(anomaly)
		if allowsAnomalyAlert(a.cfg.AnomalyAlertMinSeverity, anomaly.Severity) {
			a.cfg.OpsAlerts.Emit(
				ctx,
				fmt.Sprintf("anomaly_%s", anomaly.Severity),
				fmt.Sprintf(
					"⚠️ anomaly severity alert\nseverity=%s\nasset=%s\nreason=%s\nspread=%.4f\nconfidence=%.4f",
					anomaly.Severity,
					signal.AssetID,
					signal.Reason,
					anomaly.Spread,
					signal.Confidence,
				),
			)
		}

		a.cfg.Telemetry.RecordSignal()
		record := persistence.SignalRecord{
			ID:         signal.ID,
			AssetID:    signal.AssetID,
			Direction:  signal.Action,
			Confidence: signal.Confidence,
			Reason:     signal.Reason,
			Payload:    mustJSON(signal.Metadata),
			CreatedAt:  signal.CreatedAt,
		}
		if record.CreatedAt.IsZero() {
			record.CreatedAt = now
		}
		if err := a.cfg.Store.SaveSignal(record); err != nil {
			return fmt.Errorf("persist signal: %w", err)
		}

		a.notify(ctx, signal, "signal")

		if a.cfg.AutoExecute {
			if err := a.executeSignal(ctx, signal); err != nil {
				return err
			}
		}
	}

	return nil
}

func (a *AutoPilot) executeSignal(ctx context.Context, signal strategy.Signal) error {
	if a.cfg.LiveRouter == nil || a.cfg.Trader == nil || a.cfg.OrderBuilder == nil {
		return nil
	}

	liveOrder, request, err := a.cfg.OrderBuilder(signal)
	if err != nil {
		return fmt.Errorf("build order for signal %s: %w", signal.ID, err)
	}

	accepted, reason := a.cfg.LiveRouter.Submit(liveOrder, 0)
	if !accepted {
		record := persistence.OrderRecord{
			ID:        fmt.Sprintf("ord-%s-%d", signal.ID, time.Now().UnixNano()),
			SignalID:  signal.ID,
			Status:    "blocked",
			OrderID:   "",
			ErrorMsg:  reason,
			CreatedAt: time.Now().UTC(),
		}
		if err := a.cfg.Store.SaveOrder(record); err != nil {
			return fmt.Errorf("persist blocked order: %w", err)
		}
		a.cfg.Telemetry.RecordOrderStatus(record.Status)
		a.cfg.OpsAlerts.Emit(ctx, "order_blocked", fmt.Sprintf("⚠️ order blocked\nsignal=%s\nreason=%s\nasset=%s", signal.ID, reason, signal.AssetID))
		a.notify(ctx, signal, "blocked")
		return nil
	}

	response, err := a.cfg.Trader.PostOrder(ctx, request)
	if err == nil && !response.Success {
		responseErr := strings.TrimSpace(response.ErrorMsg)
		if responseErr == "" {
			responseErr = strings.TrimSpace(response.Status)
		}
		if responseErr == "" {
			responseErr = "success=false"
		}
		err = fmt.Errorf("order rejected: %s", responseErr)
	}
	status := response.Status
	if status == "" {
		if err != nil {
			status = "failed"
		} else {
			status = "submitted"
		}
	}
	if err != nil {
		status = "failed"
	}

	record := persistence.OrderRecord{
		ID:        fmt.Sprintf("ord-%s-%d", signal.ID, time.Now().UnixNano()),
		SignalID:  signal.ID,
		Status:    status,
		OrderID:   response.OrderID,
		ErrorMsg:  response.ErrorMsg,
		CreatedAt: time.Now().UTC(),
	}
	if err != nil {
		record.ErrorMsg = err.Error()
	}
	if saveErr := a.cfg.Store.SaveOrder(record); saveErr != nil {
		return fmt.Errorf("persist order result: %w", saveErr)
	}
	a.cfg.Telemetry.RecordOrderStatus(record.Status)
	if err != nil {
		a.cfg.OpsAlerts.Emit(ctx, "order_failed", fmt.Sprintf("⚠️ order submit failed\nsignal=%s\nasset=%s\nerror=%s", signal.ID, signal.AssetID, record.ErrorMsg))
	}
	if reconcileErr := a.reconcileOrder(ctx, signal, response.OrderID, record.Status); reconcileErr != nil {
		return reconcileErr
	}

	a.notify(ctx, signal, status)
	if err != nil {
		return nil
	}
	return nil
}

var privateStatusCodePattern = regexp.MustCompile(`\sreturned\s(\d{3})`)

func (a *AutoPilot) reconcileOrder(ctx context.Context, signal strategy.Signal, orderID string, currentStatus string) error {
	if strings.TrimSpace(orderID) == "" {
		return nil
	}
	reader, ok := a.cfg.Trader.(OrderStatusReader)
	if !ok {
		return nil
	}
	status := normalizeOrderStatus(currentStatus)
	if isTerminalOrderStatus(status) {
		return nil
	}

	delay := a.cfg.ReconcileBaseDelay
	for attempt := 0; attempt < a.cfg.ReconcileMaxAttempts; attempt++ {
		openOrder, err := reader.GetOrder(ctx, orderID)
		if err != nil {
			a.cfg.Telemetry.RecordReconcileStatus("error")
			a.cfg.OpsAlerts.Emit(ctx, "order_reconcile_error", fmt.Sprintf("⚠️ order reconcile failed\norder_id=%s\nattempt=%d\nerror=%s", orderID, attempt+1, err.Error()))
			if !isRetryableOrderStatusError(err) || attempt == a.cfg.ReconcileMaxAttempts-1 {
				record := persistence.OrderRecord{
					ID:        fmt.Sprintf("ord-reconcile-%s-%d", signal.ID, time.Now().UnixNano()),
					SignalID:  signal.ID,
					Status:    "reconcile_error",
					OrderID:   orderID,
					ErrorMsg:  err.Error(),
					CreatedAt: time.Now().UTC(),
				}
				if saveErr := a.cfg.Store.SaveOrder(record); saveErr != nil {
					return fmt.Errorf("persist reconcile error: %w", saveErr)
				}
				a.cfg.Telemetry.RecordOrderStatus(record.Status)
				return nil
			}
			if !waitWithContext(ctx, delay) {
				return nil
			}
			delay = nextReconcileDelay(delay, a.cfg.ReconcileMaxDelay)
			continue
		}

		exchangeStatus := normalizeOrderStatus(openOrder.Status)
		if exchangeStatus == "" {
			exchangeStatus = "unknown"
		}

		a.cfg.Telemetry.RecordReconcileStatus(exchangeStatus)
		if exchangeStatus != status {
			record := persistence.OrderRecord{
				ID:        fmt.Sprintf("ord-reconcile-%s-%d", signal.ID, time.Now().UnixNano()),
				SignalID:  signal.ID,
				Status:    exchangeStatus,
				OrderID:   orderID,
				ErrorMsg:  "",
				CreatedAt: time.Now().UTC(),
			}
			if saveErr := a.cfg.Store.SaveOrder(record); saveErr != nil {
				return fmt.Errorf("persist reconcile status: %w", saveErr)
			}
			a.cfg.Telemetry.RecordOrderStatus(record.Status)
			status = exchangeStatus
		}
		if isTerminalOrderStatus(exchangeStatus) {
			return nil
		}
		if attempt == a.cfg.ReconcileMaxAttempts-1 {
			record := persistence.OrderRecord{
				ID:        fmt.Sprintf("ord-reconcile-%s-%d", signal.ID, time.Now().UnixNano()),
				SignalID:  signal.ID,
				Status:    "reconcile_pending",
				OrderID:   orderID,
				ErrorMsg:  "terminal status not reached",
				CreatedAt: time.Now().UTC(),
			}
			if saveErr := a.cfg.Store.SaveOrder(record); saveErr != nil {
				return fmt.Errorf("persist reconcile pending: %w", saveErr)
			}
			a.cfg.Telemetry.RecordOrderStatus(record.Status)
			a.cfg.OpsAlerts.Emit(ctx, "order_reconcile_pending", fmt.Sprintf("⚠️ order reconcile pending\norder_id=%s\nstatus=%s", orderID, status))
			return nil
		}
		if !waitWithContext(ctx, delay) {
			return nil
		}
		delay = nextReconcileDelay(delay, a.cfg.ReconcileMaxDelay)
	}
	return nil
}

func isRetryableOrderStatusError(err error) bool {
	if err == nil {
		return false
	}
	matches := privateStatusCodePattern.FindStringSubmatch(err.Error())
	if len(matches) != 2 {
		return false
	}
	statusCode, parseErr := strconv.Atoi(matches[1])
	if parseErr != nil {
		return false
	}
	return polymarket.ShouldRetryHTTPStatus(statusCode)
}

func normalizeOrderStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

func isTerminalOrderStatus(status string) bool {
	switch normalizeOrderStatus(status) {
	case "filled", "cancelled", "canceled", "expired", "failed", "matched":
		return true
	default:
		return false
	}
}

func waitWithContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextReconcileDelay(current time.Duration, maxDelay time.Duration) time.Duration {
	next := current * 2
	if next <= 0 {
		next = 200 * time.Millisecond
	}
	if maxDelay > 0 && next > maxDelay {
		return maxDelay
	}
	return next
}

func (a *AutoPilot) notify(ctx context.Context, signal strategy.Signal, stage string) {
	if a.cfg.Notifier == nil {
		return
	}
	severity := signalSeverityFromMetadata(signal)
	spread := readSignalSpread(signal.Metadata)
	text := fmt.Sprintf(
		"📈 %s %s\nasset=%s\nreason=%s\nseverity=%s\nconfidence=%.4f\nspread=%.4f",
		stage,
		signal.Action,
		signal.AssetLabel,
		signal.Reason,
		severity,
		signal.Confidence,
		spread,
	)
	messageID, err := a.cfg.Notifier.Send(ctx, AlertMessage{Target: signal.AssetID, Text: text})

	status := "sent"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
	}

	_ = a.cfg.Store.SaveNotification(persistence.NotificationRecord{
		ID:        fmt.Sprintf("notif-%s-%s-%d", signal.ID, stage, time.Now().UnixNano()),
		Channel:   "telegram",
		Target:    signal.AssetID,
		Message:   text,
		Status:    status,
		ErrorMsg:  errMsg,
		CreatedAt: time.Now().UTC(),
	})

	_ = messageID
}

func signalSeverityFromMetadata(signal strategy.Signal) AnomalySeverity {
	if len(signal.Metadata) == 0 {
		return AnomalySeverityLow
	}
	raw, ok := signal.Metadata["severity"]
	if !ok {
		return AnomalySeverityLow
	}
	value, ok := raw.(string)
	if !ok {
		return AnomalySeverityLow
	}
	severity := ParseAnomalySeverity(value, AnomalySeverityLow)
	if normalizeAnomalySeverity(string(severity)) == "" {
		return AnomalySeverityLow
	}
	return severity
}

func mustJSON(value any) []byte {
	if value == nil {
		return []byte(`{}`)
	}
	body, err := json.Marshal(value)
	if err != nil {
		return []byte(`{}`)
	}
	return body
}
