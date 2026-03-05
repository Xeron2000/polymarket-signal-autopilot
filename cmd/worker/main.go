package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"polymarket-signal/internal/connectors/polymarket"
	"polymarket-signal/internal/execution"
	"polymarket-signal/internal/notify"
	"polymarket-signal/internal/persistence"
	"polymarket-signal/internal/runtime"
	"polymarket-signal/internal/strategy"
)

type executionDependencies struct {
	trader  runtime.OrderTrader
	builder runtime.OrderBuilder
}

type runtimeStateStore interface {
	AllocateRuntimeNonce(ctx context.Context, key string, seed int64) (int64, error)
}

func main() {
	assetIDs := parseAssetIDs(os.Getenv("POLY_ASSET_IDS"))
	if len(assetIDs) == 0 {
		log.Fatalf("POLY_ASSET_IDS is required (comma-separated token ids)")
	}

	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		log.Fatalf("DATABASE_URL is required for persistent storage")
	}
	db, err := persistence.OpenPostgres(dsn)
	if err != nil {
		log.Fatalf("open postgres failed: %v", err)
	}
	defer db.Close()

	store := persistence.NewStore(db)
	stream := polymarket.NewWSMarketClient(os.Getenv("POLY_WS_URL"))

	strategyEngine := buildStrategy(assetIDs)
	notifier := buildNotifier()
	telemetry := runtime.NewAutoPilotTelemetry()
	opsAlerts := runtime.NewOpsAlertManager(notifier, parseDuration("OPS_ALERT_COOLDOWN", 2*time.Minute))

	autoExecute := parseBool("AUTO_EXECUTE", false)
	country := getenv("EXEC_COUNTRY", "US")
	liveRouter := execution.NewLiveRouter(execution.LiveConfig{
		WhitelistMarkets:     asSet(assetIDs),
		PerMarketExposureCap: parseFloat("EXEC_MARKET_CAP", 1000),
		StaleCutoff:          parseDuration("EXEC_STALE_SECONDS", 5*time.Second),
		DailyLossCap:         parseFloat("EXEC_DAILY_LOSS_CAP", 500),
		AllowedCountries:     asSet([]string{country}),
	})
	anomalySeverityMedium := parseFloat("ANOMALY_SEVERITY_MEDIUM", 0.08)
	anomalySeverityHigh := parseFloat("ANOMALY_SEVERITY_HIGH", 0.15)
	anomalyAlertMinSeverity := runtime.ParseAnomalySeverity(os.Getenv("ANOMALY_ALERT_MIN_SEVERITY"), runtime.AnomalySeverityMedium)

	trader, orderBuilder := buildExecutionDependencies(autoExecute, country, store, telemetry, opsAlerts)

	autopilot := runtime.NewAutoPilot(runtime.AutoPilotConfig{
		Stream:                  stream,
		Store:                   store,
		Strategy:                strategyEngine,
		Notifier:                notifier,
		Telemetry:               telemetry,
		OpsAlerts:               opsAlerts,
		LiveRouter:              liveRouter,
		Trader:                  trader,
		AutoExecute:             autoExecute,
		Country:                 country,
		OrderBuilder:            orderBuilder,
		ReconcileMaxAttempts:    int(parseInt("ORDER_RECONCILE_MAX_ATTEMPTS", 3)),
		ReconcileBaseDelay:      parseDuration("ORDER_RECONCILE_BASE_DELAY", 300*time.Millisecond),
		ReconcileMaxDelay:       parseDuration("ORDER_RECONCILE_MAX_DELAY", 2*time.Second),
		ReconcileSweepInterval:  parseDuration("ORDER_RECONCILE_SWEEP_INTERVAL", 15*time.Second),
		ReconcileSweepLimit:     int(parseInt("ORDER_RECONCILE_SWEEP_LIMIT", 50)),
		AnomalySeverityMedium:   anomalySeverityMedium,
		AnomalySeverityHigh:     anomalySeverityHigh,
		AnomalyAlertMinSeverity: anomalyAlertMinSeverity,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opsAddr := strings.TrimSpace(getenv("WORKER_OPS_ADDR", ":9091"))
	var opsServer *http.Server
	if opsAddr != "" {
		opsServer = &http.Server{
			Addr:              opsAddr,
			Handler:           runtime.NewWorkerOpsHandler(telemetry),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			if err := opsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("worker ops server failed: %v", err)
			}
		}()
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = opsServer.Shutdown(shutdownCtx)
		}()
		log.Printf("worker ops listening on %s", opsAddr)
	}

	log.Printf("worker starting (assets=%v auto_execute=%v ws=%s)", assetIDs, autoExecute, effectiveWSURL(os.Getenv("POLY_WS_URL")))
	if err := autopilot.Run(ctx, assetIDs); err != nil {
		log.Fatalf("autopilot run failed: %v", err)
	}
	log.Printf("worker stopped")
}

func buildStrategy(assetIDs []string) *strategy.RankShiftStrategy {
	assetA := getenv("ARB_ASSET_A", assetIDs[0])
	assetBDefault := ""
	if len(assetIDs) > 1 {
		assetBDefault = assetIDs[1]
	}
	assetB := getenv("ARB_ASSET_B", assetBDefault)
	if strings.TrimSpace(assetB) == "" {
		log.Fatalf("ARB_ASSET_B is required (or pass at least two POLY_ASSET_IDS)")
	}

	flipSpread := parseFloat("ARB_FLIP_SPREAD", 0.05)
	cooldown := parseDuration("ARB_COOLDOWN_SECONDS", 5*time.Minute)
	targetLabel := getenv("ARB_TARGET_LABEL", strings.ToUpper(assetA))

	return strategy.NewRankShiftStrategy(strategy.RankShiftConfig{
		AssetA:      assetA,
		AssetB:      assetB,
		FlipSpread:  flipSpread,
		Cooldown:    cooldown,
		TargetLabel: targetLabel,
	})
}

func buildNotifier() runtime.AlertNotifier {
	bToken := strings.TrimSpace(os.Getenv("TG_BOT_TOKEN"))
	chatID := strings.TrimSpace(os.Getenv("TG_CHAT_ID"))
	if bToken == "" || chatID == "" {
		return nil
	}
	tg := notify.NewTelegramNotifier(bToken, os.Getenv("TG_BASE_URL"), nil)
	return telegramAdapter{chatID: chatID, notifier: tg}
}

func buildExecutionDependencies(
	autoExecute bool,
	country string,
	stateStore runtimeStateStore,
	telemetry *runtime.AutoPilotTelemetry,
	opsAlerts *runtime.OpsAlertManager,
) (runtime.OrderTrader, runtime.OrderBuilder) {
	if !autoExecute {
		return nil, nil
	}

	deps, err := loadExecutionDependenciesFromEnv(country, stateStore, telemetry, opsAlerts)
	if err != nil {
		log.Fatalf("configure execution dependencies failed: %v", err)
	}
	return deps.trader, deps.builder
}

func loadExecutionDependenciesFromEnv(
	country string,
	stateStore runtimeStateStore,
	telemetry *runtime.AutoPilotTelemetry,
	opsAlerts *runtime.OpsAlertManager,
) (executionDependencies, error) {
	deps := executionDependencies{}

	creds := polymarket.PrivateAPICredentials{
		Address:    strings.TrimSpace(os.Getenv("POLY_TRADER_ADDRESS")),
		APIKey:     strings.TrimSpace(os.Getenv("POLY_API_KEY")),
		Passphrase: strings.TrimSpace(os.Getenv("POLY_PASSPHRASE")),
		Secret:     strings.TrimSpace(os.Getenv("POLY_API_SECRET")),
	}
	if creds.Address == "" || creds.APIKey == "" || creds.Passphrase == "" || creds.Secret == "" {
		return deps, fmt.Errorf("AUTO_EXECUTE=true requires POLY_TRADER_ADDRESS/POLY_API_KEY/POLY_PASSPHRASE/POLY_API_SECRET")
	}

	notional, err := parseFloatStrict("EXEC_NOTIONAL", 10)
	if err != nil {
		return deps, err
	}
	client := polymarket.NewPrivateCLOBClient(os.Getenv("POLY_CLOB_URL"), creds, nil)
	deps.trader = client

	privateKey := strings.TrimSpace(os.Getenv("POLY_PRIVATE_KEY"))
	if privateKey != "" {
		recordPricing := func(mode string) {
			if telemetry != nil {
				telemetry.RecordPricingMode(mode)
			}
		}
		recordNonce := func() {
			if telemetry != nil {
				telemetry.RecordNonceAllocation()
			}
		}

		price, err := parseFloatStrict("POLY_ORDER_PRICE", 0.5)
		if err != nil {
			return deps, err
		}
		dynamicPricingEnabled, err := parseBoolStrict("POLY_DYNAMIC_PRICING", true)
		if err != nil {
			return deps, err
		}
		dynamicPriceOffsetBps, err := parseFloatStrict("POLY_DYNAMIC_PRICE_OFFSET_BPS", 25)
		if err != nil {
			return deps, err
		}
		dynamicPriceMin, err := parseFloatStrict("POLY_DYNAMIC_PRICE_MIN", 0.01)
		if err != nil {
			return deps, err
		}
		dynamicPriceMax, err := parseFloatStrict("POLY_DYNAMIC_PRICE_MAX", 0.99)
		if err != nil {
			return deps, err
		}
		dynamicPriceTimeoutMS, err := parseIntStrict("POLY_DYNAMIC_PRICE_TIMEOUT_MS", 1500)
		if err != nil {
			return deps, err
		}
		feeRateBps, err := parseIntStrict("POLY_ORDER_FEE_BPS", 0)
		if err != nil {
			return deps, err
		}
		nonceStart, err := parseIntStrict("POLY_ORDER_NONCE", 0)
		if err != nil {
			return deps, err
		}
		nonceStateKey := fmt.Sprintf("nonce:%s", strings.ToLower(creds.Address))
		nonceSeed := nonceStart
		var nonceProvider polymarket.NonceProvider
		if stateStore != nil {
			dbProvider, providerErr := newDBAtomicNonceProvider(stateStore, nonceStateKey, nonceSeed)
			if providerErr != nil {
				return deps, providerErr
			}
			nonceProvider = dbProvider
		} else {
			localProvider, providerErr := newMonotonicNonceProvider(nonceSeed, nil)
			if providerErr != nil {
				return deps, providerErr
			}
			nonceProvider = localProvider
		}
		signatureType, err := parseIntStrict("POLY_ORDER_SIGNATURE_TYPE", 0)
		if err != nil {
			return deps, err
		}
		expirationSeconds, err := parseIntStrict("POLY_ORDER_EXPIRATION_SECONDS", 600)
		if err != nil {
			return deps, err
		}
		chainID, err := parseIntStrict("POLY_CHAIN_ID", 137)
		if err != nil {
			return deps, err
		}
		negRisk, err := parseBoolStrict("POLY_ORDER_NEG_RISK", false)
		if err != nil {
			return deps, err
		}
		deferExec, err := parseBoolStrict("POLY_ORDER_DEFER_EXEC", false)
		if err != nil {
			return deps, err
		}

		orderSide := getenv("POLY_ORDER_SIDE", "BUY")
		bookClient := polymarket.NewCLOBRESTClient(os.Getenv("POLY_CLOB_URL"), nil)

		dynamicBuilder, err := polymarket.NewDynamicOrderBuilder(polymarket.DynamicOrderConfig{
			PrivateKeyHex:     privateKey,
			Owner:             getenv("POLY_ORDER_OWNER", creds.APIKey),
			MakerAddress:      getenv("POLY_ORDER_MAKER", creds.Address),
			SignerAddress:     strings.TrimSpace(os.Getenv("POLY_ORDER_SIGNER")),
			TakerAddress:      strings.TrimSpace(os.Getenv("POLY_ORDER_TAKER")),
			Price:             price,
			Side:              orderSide,
			OrderType:         getenv("POLY_ORDER_TYPE", "GTC"),
			DeferExec:         deferExec,
			FeeRateBps:        feeRateBps,
			Nonce:             nonceSeed,
			NonceProvider:     nonceProvider,
			SignatureType:     signatureType,
			ExpirationSeconds: expirationSeconds,
			ChainID:           chainID,
			NegRisk:           negRisk,
		})
		if err != nil {
			return deps, err
		}

		deps.builder = func(signal strategy.Signal) (execution.LiveOrder, polymarket.PostOrderRequest, error) {
			request := polymarket.PostOrderRequest{}
			mode := "static"
			selectedPrice := price
			if dynamicPricingEnabled {
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(dynamicPriceTimeoutMS)*time.Millisecond)
				book, bookErr := bookClient.GetOrderBook(ctx, signal.AssetID)
				cancel()
				pickedPrice, pickedMode, pickErr := chooseLimitPrice(
					dynamicPricingEnabled,
					book,
					bookErr,
					orderSide,
					price,
					dynamicPriceOffsetBps,
					dynamicPriceMin,
					dynamicPriceMax,
				)
				if pickErr != nil {
					return execution.LiveOrder{}, polymarket.PostOrderRequest{}, pickErr
				}
				selectedPrice = pickedPrice
				mode = pickedMode
				dynamicRequest, buildErr := dynamicBuilder.BuildRequestWithPrice(signal.AssetID, notional, selectedPrice)
				if buildErr != nil {
					return execution.LiveOrder{}, polymarket.PostOrderRequest{}, buildErr
				}
				request = dynamicRequest
				if mode == "fallback" && opsAlerts != nil {
					opsAlerts.Emit(context.Background(), "dynamic_pricing_fallback", fmt.Sprintf("⚠️ dynamic pricing fallback\nasset=%s", signal.AssetID))
				}
			}
			if request.Order == nil {
				fallbackRequest, buildErr := dynamicBuilder.BuildRequest(signal.AssetID, notional)
				if buildErr != nil {
					return execution.LiveOrder{}, polymarket.PostOrderRequest{}, buildErr
				}
				request = fallbackRequest
			}
			recordPricing(mode)
			recordNonce()
			order := execution.LiveOrder{
				Key:      buildLiveOrderKey(signal.ID, signal.AssetID),
				Market:   signal.AssetID,
				Country:  country,
				Notional: notional,
			}
			return order, request, nil
		}
		return deps, nil
	}

	template, err := parseOrderTemplate(os.Getenv("POLY_SIGNED_ORDER_JSON"))
	if err != nil {
		return deps, fmt.Errorf("invalid POLY_SIGNED_ORDER_JSON: %w", err)
	}

	deps.builder = func(signal strategy.Signal) (execution.LiveOrder, polymarket.PostOrderRequest, error) {
		request := clonePostOrderRequest(template)
		market := signal.AssetID
		if fromTemplate := readTokenID(request.Order); fromTemplate != "" {
			market = fromTemplate
		}

		order := execution.LiveOrder{
			Key:      buildLiveOrderKey(signal.ID, market),
			Market:   market,
			Country:  country,
			Notional: notional,
		}
		return order, request, nil
	}

	return deps, nil
}

type telegramAdapter struct {
	chatID   string
	notifier *notify.TelegramNotifier
}

func (t telegramAdapter) Send(ctx context.Context, message runtime.AlertMessage) (int64, error) {
	return t.notifier.Send(ctx, notify.TelegramMessage{ChatID: t.chatID, Text: message.Text})
}

func parseOrderTemplate(raw string) (polymarket.PostOrderRequest, error) {
	if strings.TrimSpace(raw) == "" {
		return polymarket.PostOrderRequest{}, fmt.Errorf("POLY_SIGNED_ORDER_JSON is required")
	}
	var request polymarket.PostOrderRequest
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		return polymarket.PostOrderRequest{}, fmt.Errorf("decode signed order json: %w", err)
	}
	if request.Order == nil {
		return polymarket.PostOrderRequest{}, fmt.Errorf("signed order template missing 'order' object")
	}
	if strings.TrimSpace(request.Owner) == "" {
		return polymarket.PostOrderRequest{}, fmt.Errorf("signed order template missing 'owner'")
	}
	return request, nil
}

func clonePostOrderRequest(source polymarket.PostOrderRequest) polymarket.PostOrderRequest {
	raw, err := json.Marshal(source)
	if err != nil {
		return source
	}
	var out polymarket.PostOrderRequest
	if err := json.Unmarshal(raw, &out); err != nil {
		return source
	}
	return out
}

func readTokenID(order any) string {
	object, ok := order.(map[string]any)
	if !ok {
		return ""
	}
	value, ok := object["tokenId"]
	if !ok {
		return ""
	}
	token, ok := value.(string)
	if !ok {
		return ""
	}
	return token
}

func buildLiveOrderKey(signalID string, market string) string {
	trimmedSignalID := strings.TrimSpace(signalID)
	trimmedMarket := strings.TrimSpace(market)
	if trimmedSignalID == "" {
		trimmedSignalID = "unknown"
	}
	if trimmedMarket == "" {
		trimmedMarket = "unknown"
	}
	return fmt.Sprintf("sig-%s-%s", trimmedSignalID, trimmedMarket)
}

func asSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	return set
}

func parseAssetIDs(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseInt(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseFloatStrict(key string, fallback float64) (float64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a float, got %q", key, value)
	}
	return parsed, nil
}

func parseIntStrict(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", key, value)
	}
	return parsed, nil
}

func parseBoolStrict(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean, got %q", key, value)
	}
	return parsed, nil
}

type monotonicNonceProvider struct {
	mu      sync.Mutex
	next    int64
	persist func(next int64) error
}

type dbAtomicNonceProvider struct {
	store runtimeStateStore
	key   string
	seed  int64
}

func newDBAtomicNonceProvider(store runtimeStateStore, key string, seed int64) (*dbAtomicNonceProvider, error) {
	if store == nil {
		return nil, fmt.Errorf("runtime state store is required for db atomic nonce provider")
	}
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return nil, fmt.Errorf("runtime nonce state key is required")
	}
	if seed < 0 {
		return nil, fmt.Errorf("POLY_ORDER_NONCE must be >= 0")
	}
	return &dbAtomicNonceProvider{store: store, key: trimmedKey, seed: seed}, nil
}

func (p *dbAtomicNonceProvider) NextNonce() (int64, error) {
	nonce, err := p.store.AllocateRuntimeNonce(context.Background(), p.key, p.seed)
	if err != nil {
		return 0, fmt.Errorf("allocate db atomic nonce: %w", err)
	}
	return nonce, nil
}

func newMonotonicNonceProvider(initial int64, persist func(next int64) error) (*monotonicNonceProvider, error) {
	if initial < 0 {
		return nil, fmt.Errorf("POLY_ORDER_NONCE must be >= 0")
	}
	return &monotonicNonceProvider{next: initial, persist: persist}, nil
}

func (p *monotonicNonceProvider) NextNonce() (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	nonce := p.next
	next := p.next + 1
	if p.persist != nil {
		if err := p.persist(next); err != nil {
			return 0, fmt.Errorf("persist nonce state: %w", err)
		}
	}
	p.next = next
	return nonce, nil
}

func parseDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if duration, err := time.ParseDuration(value); err == nil {
		return duration
	}
	return fallback
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func effectiveWSURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return polymarket.DefaultMarketWSURL
	}
	return raw
}

var _ runtime.OrderTrader = (*polymarket.PrivateCLOBClient)(nil)
var _ runtime.AlertNotifier = telegramAdapter{}
