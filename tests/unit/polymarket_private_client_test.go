package unit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"polymarket-signal/internal/connectors/polymarket"
)

func TestBuildPolyHMACSignatureParity(t *testing.T) {
	signature, err := polymarket.BuildPolyHMACSignature(
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		1000000,
		"test-sign",
		"/orders",
		"{\"hash\": \"0x123\"}",
	)
	if err != nil {
		t.Fatalf("expected signature build success, got %v", err)
	}
	if signature != "ZwAdJKvoYRlEKDkNMwd5BuwNNtg93kNaR_oU2HrfVvc=" {
		t.Fatalf("unexpected signature: %s", signature)
	}
}

func TestBuildPolyHMACSignatureAcceptsBase64URLSecret(t *testing.T) {
	base64Sig, err := polymarket.BuildPolyHMACSignature(
		"++/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		1000000,
		"test-sign",
		"/orders",
		"{\"hash\": \"0x123\"}",
	)
	if err != nil {
		t.Fatalf("expected base64 signature success, got %v", err)
	}
	base64URLSig, err := polymarket.BuildPolyHMACSignature(
		"--_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		1000000,
		"test-sign",
		"/orders",
		"{\"hash\": \"0x123\"}",
	)
	if err != nil {
		t.Fatalf("expected base64url signature success, got %v", err)
	}
	if base64URLSig != base64Sig {
		t.Fatalf("expected base64url and base64 signatures equal, got %s != %s", base64URLSig, base64Sig)
	}
}

func TestPrivateClientAddsL2HeadersAndPostsOrder(t *testing.T) {
	const ts = int64(1700000000)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/order" {
			t.Fatalf("expected /order path, got %s", r.URL.Path)
		}
		if r.Header.Get("POLY_ADDRESS") != "0xabc" {
			t.Fatalf("missing POLY_ADDRESS header: %q", r.Header.Get("POLY_ADDRESS"))
		}
		if r.Header.Get("POLY_API_KEY") != "key-1" {
			t.Fatalf("missing POLY_API_KEY header: %q", r.Header.Get("POLY_API_KEY"))
		}
		if r.Header.Get("POLY_PASSPHRASE") != "pass-1" {
			t.Fatalf("missing POLY_PASSPHRASE header: %q", r.Header.Get("POLY_PASSPHRASE"))
		}
		if r.Header.Get("POLY_TIMESTAMP") != "1700000000" {
			t.Fatalf("unexpected POLY_TIMESTAMP: %q", r.Header.Get("POLY_TIMESTAMP"))
		}
		if strings.TrimSpace(r.Header.Get("POLY_SIGNATURE")) == "" {
			t.Fatal("expected POLY_SIGNATURE header to be non-empty")
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		if payload["owner"] != "owner-1" {
			t.Fatalf("unexpected owner payload: %+v", payload)
		}

		_ = json.NewEncoder(w).Encode(polymarket.PostOrderResponse{
			Success: true,
			OrderID: "0xorder1",
			Status:  "live",
		})
	}))
	defer server.Close()

	client := polymarket.NewPrivateCLOBClientWithClock(
		server.URL,
		polymarket.PrivateAPICredentials{
			Address:    "0xabc",
			APIKey:     "key-1",
			Passphrase: "pass-1",
			Secret:     "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		},
		server.Client(),
		func() time.Time { return time.Unix(ts, 0).UTC() },
	)

	resp, err := client.PostOrder(context.Background(), polymarket.PostOrderRequest{
		Order: map[string]any{"tokenId": "tok1", "side": "BUY"},
		Owner: "owner-1",
	})
	if err != nil {
		t.Fatalf("expected post order success, got %v", err)
	}
	if !resp.Success || resp.OrderID != "0xorder1" {
		t.Fatalf("unexpected post order response: %+v", resp)
	}
}
