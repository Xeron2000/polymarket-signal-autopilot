package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"polymarket-signal/internal/connectors/polymarket"
)

func TestGammaRESTClientPublicEndpoints(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/markets":
			if r.URL.Query().Get("active") != "true" {
				t.Fatalf("expected active=true query, got %q", r.URL.Query().Get("active"))
			}
			_, _ = w.Write([]byte(`[{"id":"m1","event_id":"e1","title":"Election","active":true}]`))
		case "/events":
			_, _ = w.Write([]byte(`[{"id":"e1","title":"US Election"}]`))
		default:
			http.NotFound(w, r)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := polymarket.NewGammaRESTClient(server.URL, server.Client())

	markets, err := client.GetMarkets(context.Background(), true)
	if err != nil {
		t.Fatalf("expected GetMarkets success, got %v", err)
	}
	if len(markets) != 1 || markets[0].ID != "m1" || markets[0].EventID != "e1" {
		t.Fatalf("unexpected markets response: %+v", markets)
	}

	events, err := client.GetEvents(context.Background())
	if err != nil {
		t.Fatalf("expected GetEvents success, got %v", err)
	}
	if len(events) != 1 || events[0].ID != "e1" {
		t.Fatalf("unexpected events response: %+v", events)
	}
}

func TestCLOBRESTClientPublicEndpoints(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/book":
			if r.URL.Query().Get("token_id") != "tok1" {
				t.Fatalf("expected /book?token_id=tok1, got token_id=%q", r.URL.Query().Get("token_id"))
			}
			_, _ = w.Write([]byte(`{"token_id":"tok1","bids":[{"price":"0.45","size":"100"}],"asks":[{"price":"0.55","size":"120"}]}`))
		case r.URL.Path == "/trades":
			if r.URL.Query().Get("token_id") != "tok1" {
				t.Fatalf("expected token_id=tok1, got %q", r.URL.Query().Get("token_id"))
			}
			if r.URL.Query().Get("since") == "" {
				t.Fatal("expected since query parameter")
			}
			payload := []map[string]any{{
				"id":        "t1",
				"token_id":  "tok1",
				"price":     "0.5",
				"size":      "10",
				"timestamp": now.Format(time.RFC3339),
			}}
			_ = json.NewEncoder(w).Encode(payload)
		default:
			http.NotFound(w, r)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client := polymarket.NewCLOBRESTClient(server.URL, server.Client())

	orderBook, err := client.GetOrderBook(context.Background(), "tok1")
	if err != nil {
		t.Fatalf("expected GetOrderBook success, got %v", err)
	}
	if orderBook.TokenID != "tok1" || len(orderBook.Bids) != 1 || len(orderBook.Asks) != 1 {
		t.Fatalf("unexpected orderbook response: %+v", orderBook)
	}

	trades, err := client.GetTradesSince(context.Background(), "tok1", now.Add(-1*time.Minute))
	if err != nil {
		t.Fatalf("expected GetTradesSince success, got %v", err)
	}
	if len(trades) != 1 || trades[0].ID != "t1" || trades[0].TokenID != "tok1" {
		t.Fatalf("unexpected trades response: %+v", trades)
	}
}

func TestWSMarketClientSubscribeAndReceivePublicFeed(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		_, subscribeMsg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read subscribe message failed: %v", err)
		}
		if !strings.Contains(string(subscribeMsg), "assets_ids") || !strings.Contains(string(subscribeMsg), "market") {
			t.Fatalf("unexpected subscribe payload: %s", string(subscribeMsg))
		}

		err = conn.WriteMessage(websocket.TextMessage, []byte(`{"asset_id":"tok1","type":"book","payload":{"bid":0.44}}`))
		if err != nil {
			t.Fatalf("write market update failed: %v", err)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := polymarket.NewWSMarketClient(wsURL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	updates, errs := client.Stream(ctx, []string{"tok1"})

	select {
	case err := <-errs:
		t.Fatalf("unexpected stream error: %v", err)
	case msg := <-updates:
		if msg.AssetID != "tok1" || msg.Type != "book" {
			t.Fatalf("unexpected ws update: %+v", msg)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for websocket update")
	}
}

func TestWSMarketClientReceiveEventTypePriceChanges(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		_, subscribeMsg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read subscribe message failed: %v", err)
		}
		if !strings.Contains(string(subscribeMsg), "assets_ids") || !strings.Contains(string(subscribeMsg), "market") {
			t.Fatalf("unexpected subscribe payload: %s", string(subscribeMsg))
		}

		err = conn.WriteMessage(websocket.TextMessage, []byte(`{
			"event_type":"price_change",
			"market":"m1",
			"price_changes":[
				{"asset_id":"tok1","price":"0.49","best_bid":"0.49","best_ask":"0.51"},
				{"asset_id":"tok2","price":"0.51","best_bid":"0.51","best_ask":"0.53"}
			]
		}`))
		if err != nil {
			t.Fatalf("write market update failed: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := polymarket.NewWSMarketClient(wsURL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	updates, errs := client.Stream(ctx, []string{"tok1", "tok2"})
	seen := map[string]bool{}

	for len(seen) < 2 {
		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("unexpected stream error: %v", err)
			}
		case msg := <-updates:
			if msg.Type != "price_change" {
				t.Fatalf("unexpected ws update type: %+v", msg)
			}
			if msg.AssetID != "tok1" && msg.AssetID != "tok2" {
				t.Fatalf("unexpected ws asset id: %+v", msg)
			}

			var payload map[string]any
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("decode payload failed: %v", err)
			}
			if _, ok := payload["price"]; !ok {
				t.Fatalf("expected price in payload, got %+v", payload)
			}
			seen[msg.AssetID] = true
		case <-ctx.Done():
			t.Fatal("timeout waiting for price_change websocket updates")
		}
	}
}

func TestWSMarketClientHandlesArrayEnvelopeMessages(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		_, _, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("read subscribe message failed: %v", err)
		}

		err = conn.WriteMessage(websocket.TextMessage, []byte(`[
			{
				"event_type": "price_change",
				"market": "m1",
				"price_changes": [
					{"asset_id":"tok1","price":"0.49","best_bid":"0.49","best_ask":"0.51"}
				]
			}
		]`))
		if err != nil {
			t.Fatalf("write array market update failed: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := polymarket.NewWSMarketClient(wsURL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	updates, errs := client.Stream(ctx, []string{"tok1"})

	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("expected array envelope decode success, got stream error: %v", err)
		}
	case msg := <-updates:
		if msg.AssetID != "tok1" || msg.Type != "price_change" {
			t.Fatalf("unexpected ws update from array envelope: %+v", msg)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for array envelope update")
	}
}

func TestWSMarketClientReconnectsAfterCleanServerClose(t *testing.T) {
	upgrader := websocket.Upgrader{}
	var connections atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		_, _, err = conn.ReadMessage()
		if err != nil {
			t.Fatalf("read subscribe message failed: %v", err)
		}

		switch connections.Add(1) {
		case 1:
			return
		case 2:
			err = conn.WriteMessage(websocket.TextMessage, []byte(`{"asset_id":"tok1","type":"book","payload":{"bid":0.51}}`))
			if err != nil {
				t.Fatalf("write market update failed: %v", err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := polymarket.NewWSMarketClient(wsURL)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	updates, errs := client.Stream(ctx, []string{"tok1"})

	for {
		select {
		case <-errs:
			continue
		case msg := <-updates:
			if msg.AssetID != "tok1" || msg.Type != "book" {
				t.Fatalf("unexpected ws update after reconnect: %+v", msg)
			}
			goto done
		case <-ctx.Done():
			t.Fatal("timeout waiting for websocket update after reconnect")
		}
	}

done:

	if got := connections.Load(); got < 2 {
		t.Fatalf("expected reconnect with at least 2 connections, got %d", got)
	}
}
