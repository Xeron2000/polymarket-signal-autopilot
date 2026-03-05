package unit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"polymarket-signal/internal/notify"
)

func TestTelegramNotifierSendSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/sendMessage" {
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		if req["chat_id"] != "chat-1" {
			t.Fatalf("unexpected chat_id payload: %+v", req)
		}
		if req["parse_mode"] != "HTML" {
			t.Fatalf("expected parse_mode HTML, got %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 101}})
	}))
	defer server.Close()

	notifier := notify.NewTelegramNotifier("TOKEN", server.URL, server.Client())
	msgID, err := notifier.Send(context.Background(), notify.TelegramMessage{
		ChatID: "chat-1",
		Text:   "<b>signal</b>",
	})
	if err != nil {
		t.Fatalf("expected send success, got %v", err)
	}
	if msgID != 101 {
		t.Fatalf("unexpected telegram message id: %d", msgID)
	}
}

func TestTelegramNotifierRetriesOn429(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":          false,
				"error_code":  429,
				"description": "Too Many Requests: retry after 0",
				"parameters":  map[string]any{"retry_after": 0},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 202}})
	}))
	defer server.Close()

	notifier := notify.NewTelegramNotifier("TOKEN", server.URL, server.Client())
	notifier.MaxRetries = 2

	msgID, err := notifier.Send(context.Background(), notify.TelegramMessage{ChatID: "chat-1", Text: "hi"})
	if err != nil {
		t.Fatalf("expected retry send success, got %v", err)
	}
	if msgID != 202 {
		t.Fatalf("unexpected message id after retry: %d", msgID)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Fatalf("expected exactly 2 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}
