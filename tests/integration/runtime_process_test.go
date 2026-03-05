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
	"polymarket-signal/internal/ingestion"
	"polymarket-signal/internal/runtime"
)

func TestAPIServerUsesRealClients(t *testing.T) {
	gammaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/markets":
			_, _ = w.Write([]byte(`[{"id":"m1","event_id":"e1","title":"Election","active":true}]`))
		case "/events":
			_, _ = w.Write([]byte(`[{"id":"e1","title":"US Election"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gammaServer.Close()

	clobServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/book":
			_, _ = w.Write([]byte(`{"token_id":"tok1","bids":[{"price":"0.45","size":"100"}],"asks":[{"price":"0.55","size":"120"}]}`))
		case "/trades":
			_, _ = w.Write([]byte(`[{"id":"t1","token_id":"tok1","price":"0.5","size":"10","timestamp":"2026-03-05T00:00:00Z"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer clobServer.Close()

	api := runtime.NewAPIServer(
		polymarket.NewGammaRESTClient(gammaServer.URL, gammaServer.Client()),
		polymarket.NewCLOBRESTClient(clobServer.URL, clobServer.Client()),
	)
	httpServer := httptest.NewServer(api.Handler())
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for /health, got %d", resp.StatusCode)
	}

	resp, err = http.Get(httpServer.URL + "/markets?active=true")
	if err != nil {
		t.Fatalf("markets request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for /markets, got %d", resp.StatusCode)
	}
	var markets []polymarket.Market
	if err := json.NewDecoder(resp.Body).Decode(&markets); err != nil {
		t.Fatalf("decode markets failed: %v", err)
	}
	if len(markets) != 1 || markets[0].ID != "m1" {
		t.Fatalf("unexpected markets payload: %+v", markets)
	}

	resp, err = http.Get(httpServer.URL + "/orderbook?token_id=tok1")
	if err != nil {
		t.Fatalf("orderbook request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for /orderbook, got %d", resp.StatusCode)
	}
}

func TestWorkerConsumesRealMarketWS(t *testing.T) {
	upgrader := websocket.Upgrader{}
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
		defer conn.Close()

		_, subscribe, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read subscribe failed: %v", err)
		}
		if !strings.Contains(string(subscribe), "assets_ids") {
			t.Fatalf("unexpected subscribe payload: %s", string(subscribe))
		}

		err = conn.WriteMessage(websocket.TextMessage, []byte(`{"asset_id":"tok1","type":"book","payload":{"bid":0.44}}`))
		if err != nil {
			t.Fatalf("write update failed: %v", err)
		}
	}))
	defer wsServer.Close()

	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")
	stream := polymarket.NewWSMarketClient(wsURL)
	sink := ingestion.NewMemoryRawWriter()
	worker := runtime.NewWorker(stream, sink)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Run(ctx, []string{"tok1"})
	}()

	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(sink.List()) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(sink.List()) == 0 {
		t.Fatal("expected worker to ingest at least one ws market update")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("worker returned unexpected error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("worker did not exit after context cancellation")
	}
}
