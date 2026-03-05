package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
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

	SignalNotifyCooldown time.Duration
}

type AutoPilot struct {
	cfg AutoPilotConfig

	notifyMu         sync.Mutex
	lastSignalNotify map[string]time.Time
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
	if cfg.SignalNotifyCooldown <= 0 {
		cfg.SignalNotifyCooldown = 2 * time.Minute
	}
	return &AutoPilot{
		cfg:              cfg,
		lastSignalNotify: make(map[string]time.Time),
	}
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
				a.cfg.OpsAlerts.Emit(ctx, "market_stream_error", formatMarketStreamErrorAlert(err))
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
				formatAnomalyAlertText(signal, anomaly),
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
	if a.shouldSuppressSignalNotify(stage, signal) {
		return
	}
	text := formatSignalNotificationText(stage, signal)
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

func (a *AutoPilot) shouldSuppressSignalNotify(stage string, signal strategy.Signal) bool {
	if strings.ToLower(strings.TrimSpace(stage)) != "signal" {
		return false
	}
	if a.cfg.SignalNotifyCooldown <= 0 {
		return false
	}

	severity := signalSeverityFromMetadata(signal)
	key := fmt.Sprintf(
		"%s|%s|%s|%s|%s",
		"signal",
		strings.TrimSpace(signal.AssetID),
		strings.ToUpper(strings.TrimSpace(signal.Action)),
		strings.TrimSpace(signal.Reason),
		string(severity),
	)

	now := time.Now().UTC()
	a.notifyMu.Lock()
	defer a.notifyMu.Unlock()

	if last, ok := a.lastSignalNotify[key]; ok && now.Sub(last) < a.cfg.SignalNotifyCooldown {
		return true
	}
	a.lastSignalNotify[key] = now
	return false
}

func formatSignalNotificationText(stage string, signal strategy.Signal) string {
	severity := signalSeverityFromMetadata(signal)
	spread := readSignalSpread(signal.Metadata)
	confidencePercent := clampConfidencePercent(signal.Confidence)
	marketURL := readSignalMarketURL(signal.Metadata)

	lines := []string{
		fmt.Sprintf("📈 信号提醒（%s）", humanizeStage(stage)),
		fmt.Sprintf("方向：%s", humanizeAction(signal.Action)),
		fmt.Sprintf("标的：%s", formatAssetDisplay(signal.AssetLabel, signal.AssetID)),
		fmt.Sprintf("触发原因：%s", humanizeReason(signal.Reason)),
		fmt.Sprintf("风险等级：%s", humanizeSeverity(severity)),
		fmt.Sprintf("置信度：%.2f%%", confidencePercent),
		fmt.Sprintf("价差幅度：%.4f", spread),
	}
	if marketURL != "" {
		lines = append(lines, fmt.Sprintf("查看市场：%s", marketURL))
	}
	lines = append(lines, "建议：该信号仅供参考，请结合仓位与风控决策。")

	return strings.Join(lines, "\n")
}

func formatAnomalyAlertText(signal strategy.Signal, anomaly AnomalyEvent) string {
	confidencePercent := clampConfidencePercent(anomaly.Confidence)
	assetLabel := formatAssetDisplay(anomaly.AssetLabel, anomaly.AssetID)
	if assetLabel == "未知标的" {
		assetLabel = formatAssetDisplay(signal.AssetLabel, signal.AssetID)
	}
	marketURL := readSignalMarketURL(signal.Metadata)

	lines := []string{
		"⚠️ 异常波动预警",
		fmt.Sprintf("等级：%s", humanizeSeverity(anomaly.Severity)),
		fmt.Sprintf("标的：%s", assetLabel),
		fmt.Sprintf("触发原因：%s", humanizeReason(anomaly.Reason)),
		fmt.Sprintf("当前价差：%.4f", anomaly.Spread),
		fmt.Sprintf("置信度：%.2f%%", confidencePercent),
	}
	if marketURL != "" {
		lines = append(lines, fmt.Sprintf("查看市场：%s", marketURL))
	}
	lines = append(lines, "建议：波动放大，注意仓位与止损。")

	return strings.Join(lines, "\n")
}

func formatMarketStreamErrorAlert(err error) string {
	if err == nil {
		return "⚠️ 行情数据流出现异常，系统正在自动重连。"
	}

	raw := strings.TrimSpace(err.Error())
	lower := strings.ToLower(raw)

	summary := "行情数据流出现异常，系统正在自动重连。"
	detail := "连接恢复后会继续推送信号，请稍后再看一眼。"

	switch {
	case strings.Contains(lower, "decode market ws update"):
		summary = "行情数据格式出现变化，系统已自动跳过异常片段并继续运行。"
		detail = "无需手动处理；如果频繁出现，请升级到最新版本。"
	case strings.Contains(lower, "dial market ws"):
		summary = "行情连接暂时中断，系统正在自动重连。"
		detail = "通常是网络抖动导致，恢复后会继续接收行情。"
	case strings.Contains(lower, "read market ws"):
		summary = "行情连接读取中断，系统正在自动重连。"
		detail = "短暂断线后通常会自动恢复。"
	}

	return fmt.Sprintf("⚠️ 行情提醒\n%s\n说明：%s", summary, detail)
}

func clampConfidencePercent(confidence float64) float64 {
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	return confidence * 100
}

func humanizeStage(stage string) string {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "signal":
		return "新信号"
	case "blocked":
		return "风控拦截"
	case "failed":
		return "执行失败"
	case "submitted", "live", "matched", "filled":
		return "执行跟踪"
	case "reconcile_pending":
		return "待对账确认"
	default:
		trimmed := strings.TrimSpace(stage)
		if trimmed == "" {
			return "状态更新"
		}
		return trimmed
	}
}

func humanizeAction(action string) string {
	switch strings.ToUpper(strings.TrimSpace(action)) {
	case "LONG", "BUY":
		return "看多"
	case "SHORT", "SELL":
		return "看空"
	default:
		trimmed := strings.TrimSpace(action)
		if trimmed == "" {
			return "未知"
		}
		return trimmed
	}
}

func humanizeReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "rank_flip_polymarket_leads_news_lag":
		return "盘口领先关系发生反转（Polymarket 领先）"
	case "":
		return "未知原因"
	default:
		return strings.ReplaceAll(strings.TrimSpace(reason), "_", " ")
	}
}

func humanizeSeverity(severity AnomalySeverity) string {
	switch normalizeAnomalySeverity(string(severity)) {
	case AnomalySeverityHigh:
		return "高"
	case AnomalySeverityMedium:
		return "中"
	case AnomalySeverityLow:
		return "低"
	default:
		return "未知"
	}
}

func readSignalMarketURL(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, key := range []string{"market_url", "marketUrl", "event_url", "eventUrl", "url"} {
		raw, ok := metadata[key]
		if !ok {
			continue
		}
		value, ok := raw.(string)
		if !ok {
			continue
		}
		link := strings.TrimSpace(value)
		if strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "http://") {
			return link
		}
	}
	return ""
}

func formatAssetDisplay(label string, assetID string) string {
	trimmedLabel := strings.TrimSpace(label)
	trimmedAssetID := strings.TrimSpace(assetID)

	if trimmedLabel == "" && trimmedAssetID == "" {
		return "未知标的"
	}
	if trimmedLabel == "" || trimmedLabel == trimmedAssetID {
		return shortenAssetID(trimmedAssetID)
	}
	if trimmedAssetID == "" {
		return trimmedLabel
	}
	return fmt.Sprintf("%s (%s)", trimmedLabel, shortenAssetID(trimmedAssetID))
}

func shortenAssetID(assetID string) string {
	trimmed := strings.TrimSpace(assetID)
	if trimmed == "" {
		return "未知标的"
	}
	if len(trimmed) <= 18 {
		return trimmed
	}
	return fmt.Sprintf("%s...%s", trimmed[:10], trimmed[len(trimmed)-8:])
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
