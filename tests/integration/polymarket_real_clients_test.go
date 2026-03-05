package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
