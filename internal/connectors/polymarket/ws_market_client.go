package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

const DefaultMarketWSURL = "wss://ws-subscriptions-clob.polymarket.com/ws/market"

type WSMarketUpdate struct {
	AssetID string
	Type    string
	Payload []byte
}

type WSReconnectPolicy struct {
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	MaxAttempts int
}

type WSMarketClient struct {
	url               string
	dialer            *websocket.Dialer
	reconnectPolicy   WSReconnectPolicy
	heartbeatInterval time.Duration
}

func DefaultReconnectPolicy() WSReconnectPolicy {
	return WSReconnectPolicy{
		BaseDelay:   250 * time.Millisecond,
		MaxDelay:    5 * time.Second,
		MaxAttempts: 8,
	}
}

func NewDefaultWSMarketClient() *WSMarketClient {
	return NewWSMarketClient(DefaultMarketWSURL)
}

func NewWSMarketClient(url string) *WSMarketClient {
	if url == "" {
		url = DefaultMarketWSURL
	}
	return &WSMarketClient{
		url:               url,
		dialer:            websocket.DefaultDialer,
		reconnectPolicy:   DefaultReconnectPolicy(),
		heartbeatInterval: 10 * time.Second,
	}
}

func (c *WSMarketClient) Stream(ctx context.Context, assetIDs []string) (<-chan WSMarketUpdate, <-chan error) {
	updates := make(chan WSMarketUpdate, 64)
	errs := make(chan error, 16)

	go func() {
		defer close(updates)
		defer close(errs)

		attempt := 0
		for {
			if ctx.Err() != nil {
				return
			}

			conn, _, err := c.dialer.DialContext(ctx, c.url, nil)
			if err != nil {
				if !emitErr(ctx, errs, fmt.Errorf("dial market ws: %w", err)) {
					return
				}
				if !sleepBackoff(ctx, attempt, c.reconnectPolicy) {
					return
				}
				attempt++
				if c.reconnectPolicy.MaxAttempts > 0 && attempt >= c.reconnectPolicy.MaxAttempts {
					emitErr(ctx, errs, fmt.Errorf("market ws reconnect exceeded max attempts: %d", c.reconnectPolicy.MaxAttempts))
					return
				}
				continue
			}

			if err := c.writeSubscribe(conn, assetIDs); err != nil {
				_ = conn.Close()
				if !emitErr(ctx, errs, fmt.Errorf("subscribe market ws: %w", err)) {
					return
				}
				if !sleepBackoff(ctx, attempt, c.reconnectPolicy) {
					return
				}
				attempt++
				continue
			}

			attempt = 0
			if err := c.readLoop(ctx, conn, updates, errs); err != nil {
				if !emitErr(ctx, errs, err) {
					_ = conn.Close()
					return
				}
				_ = conn.Close()
				if !sleepBackoff(ctx, attempt, c.reconnectPolicy) {
					return
				}
				attempt++
				continue
			}

			_ = conn.Close()
			return
		}
	}()

	return updates, errs
}

func (c *WSMarketClient) readLoop(ctx context.Context, conn *websocket.Conn, updates chan<- WSMarketUpdate, errs chan<- error) error {
	pingDone := make(chan struct{})
	if c.heartbeatInterval > 0 {
		go c.pingLoop(ctx, conn, pingDone)
	}

	for {
		if ctx.Err() != nil {
			close(pingDone)
			return nil
		}

		messageType, raw, err := conn.ReadMessage()
		if err != nil {
			close(pingDone)
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read market ws: %w", err)
		}
		if messageType != websocket.TextMessage {
			continue
		}

		update, ok, err := decodeMarketUpdate(raw)
		if err != nil {
			emitErr(ctx, errs, err)
			continue
		}
		if !ok {
			continue
		}

		select {
		case <-ctx.Done():
			close(pingDone)
			return nil
		case updates <- update:
		}
	}
}

func (c *WSMarketClient) pingLoop(ctx context.Context, conn *websocket.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = conn.WriteControl(websocket.PingMessage, []byte("PING"), time.Now().Add(2*time.Second))
		}
	}
}

func (c *WSMarketClient) writeSubscribe(conn *websocket.Conn, assetIDs []string) error {
	if len(assetIDs) == 0 {
		return fmt.Errorf("assetIDs required for market subscription")
	}
	payload := map[string]any{
		"assets_ids":             assetIDs,
		"type":                   "market",
		"initial_dump":           true,
		"custom_feature_enabled": true,
	}
	if err := conn.WriteJSON(payload); err != nil {
		return fmt.Errorf("write subscribe payload: %w", err)
	}
	return nil
}

func decodeMarketUpdate(raw []byte) (WSMarketUpdate, bool, error) {
	var envelope struct {
		AssetID  string          `json:"asset_id"`
		AssetIDs []string        `json:"asset_ids"`
		Type     string          `json:"type"`
		Payload  json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return WSMarketUpdate{}, false, fmt.Errorf("decode market ws update: %w", err)
	}

	assetID := envelope.AssetID
	if assetID == "" && len(envelope.AssetIDs) > 0 {
		assetID = envelope.AssetIDs[0]
	}
	if envelope.Type == "" || assetID == "" {
		return WSMarketUpdate{}, false, nil
	}

	payload := envelope.Payload
	if len(payload) == 0 {
		payload = raw
	}

	return WSMarketUpdate{AssetID: assetID, Type: envelope.Type, Payload: payload}, true, nil
}

func ShouldRetryHTTPStatus(statusCode int) bool {
	if statusCode == 425 || statusCode == 429 {
		return true
	}
	return statusCode >= 500 && statusCode <= 599
}

func NextBackoff(attempt int, policy WSReconnectPolicy) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	delay := policy.BaseDelay * time.Duration(1<<attempt)
	if delay > policy.MaxDelay {
		return policy.MaxDelay
	}
	return delay
}

func sleepBackoff(ctx context.Context, attempt int, policy WSReconnectPolicy) bool {
	delay := NextBackoff(attempt, policy)
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func emitErr(ctx context.Context, errs chan<- error, err error) bool {
	select {
	case <-ctx.Done():
		return false
	case errs <- err:
		return true
	default:
		return true
	}
}
