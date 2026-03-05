package runtime

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"polymarket-signal/internal/strategy"
)

type AnomalySeverity string

const (
	AnomalySeverityLow    AnomalySeverity = "low"
	AnomalySeverityMedium AnomalySeverity = "medium"
	AnomalySeverityHigh   AnomalySeverity = "high"
)

type AnomalyEvent struct {
	Severity   AnomalySeverity `json:"severity"`
	AssetID    string          `json:"asset_id"`
	AssetLabel string          `json:"asset_label"`
	Reason     string          `json:"reason"`
	Action     string          `json:"action"`
	Stage      string          `json:"stage"`
	Confidence float64         `json:"confidence"`
	Spread     float64         `json:"spread"`
	CreatedAt  time.Time       `json:"created_at"`
}

func ParseAnomalySeverity(raw string, fallback AnomalySeverity) AnomalySeverity {
	severity := normalizeAnomalySeverity(raw)
	if severity == "" {
		return normalizeAnomalySeverity(string(fallback))
	}
	return severity
}

func normalizeAnomalySeverity(raw string) AnomalySeverity {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch AnomalySeverity(value) {
	case AnomalySeverityLow, AnomalySeverityMedium, AnomalySeverityHigh:
		return AnomalySeverity(value)
	default:
		return ""
	}
}

func anomalySeverityRank(severity AnomalySeverity) int {
	switch normalizeAnomalySeverity(string(severity)) {
	case AnomalySeverityHigh:
		return 3
	case AnomalySeverityMedium:
		return 2
	case AnomalySeverityLow:
		return 1
	default:
		return 0
	}
}

func allowsAnomalyAlert(minSeverity AnomalySeverity, current AnomalySeverity) bool {
	return anomalySeverityRank(current) >= anomalySeverityRank(minSeverity)
}

func classifySignalAnomaly(signal strategy.Signal, mediumThreshold float64, highThreshold float64) AnomalyEvent {
	score := signal.Confidence
	spread := readSignalSpread(signal.Metadata)
	if isFinitePositive(spread) {
		score = spread
	}

	severity := AnomalySeverityLow
	if score >= highThreshold {
		severity = AnomalySeverityHigh
	} else if score >= mediumThreshold {
		severity = AnomalySeverityMedium
	}

	createdAt := signal.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	return AnomalyEvent{
		Severity:   severity,
		AssetID:    strings.TrimSpace(signal.AssetID),
		AssetLabel: strings.TrimSpace(signal.AssetLabel),
		Reason:     strings.TrimSpace(signal.Reason),
		Action:     strings.TrimSpace(signal.Action),
		Stage:      "signal",
		Confidence: signal.Confidence,
		Spread:     spread,
		CreatedAt:  createdAt.UTC(),
	}
}

func withSignalSeverityMetadata(signal strategy.Signal, anomaly AnomalyEvent) strategy.Signal {
	out := signal
	metadata := make(map[string]any, len(signal.Metadata)+2)
	for key, value := range signal.Metadata {
		metadata[key] = value
	}
	metadata["severity"] = string(anomaly.Severity)
	if isFinitePositive(anomaly.Spread) {
		metadata["spread"] = anomaly.Spread
	}
	out.Metadata = metadata
	if out.CreatedAt.IsZero() {
		out.CreatedAt = anomaly.CreatedAt
	}
	return out
}

func readSignalSpread(metadata map[string]any) float64 {
	if len(metadata) == 0 {
		return 0
	}
	for _, key := range []string{"spread", "price_spread", "flip_spread"} {
		if value, ok := metadata[key]; ok {
			if parsed, parsedOK := parseNumericAny(value); parsedOK && isFinitePositive(parsed) {
				return parsed
			}
		}
	}
	return 0
}

func parseNumericAny(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0, false
		}
		return typed, true
	case float32:
		parsed := float64(typed)
		if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case string:
		parsed, err := parseFloatString(typed)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func parseFloatString(raw string) (float64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("numeric string is empty")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("numeric string is not finite")
	}
	return parsed, nil
}

func isFinitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}
