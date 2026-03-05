package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultCLOBBaseURL = "https://clob.polymarket.com"

type PriceLevel struct {
	Price float64
	Size  float64
}

type OrderBook struct {
	TokenID string
	Bids    []PriceLevel
	Asks    []PriceLevel
}

type Trade struct {
	ID         string
	TokenID    string
	Price      float64
	Size       float64
	OccurredAt time.Time
}

type CLOBClient interface {
	GetOrderBook(ctx context.Context, tokenID string) (OrderBook, error)
	GetTradesSince(ctx context.Context, tokenID string, since time.Time) ([]Trade, error)
}

type CLOBRESTClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewDefaultCLOBRESTClient() *CLOBRESTClient {
	return NewCLOBRESTClient(DefaultCLOBBaseURL, nil)
}

func NewCLOBRESTClient(baseURL string, httpClient *http.Client) *CLOBRESTClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultCLOBBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &CLOBRESTClient{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}
}

func (c *CLOBRESTClient) GetOrderBook(ctx context.Context, tokenID string) (OrderBook, error) {
	if strings.TrimSpace(tokenID) == "" {
		return OrderBook{}, fmt.Errorf("tokenID is required")
	}

	query := url.Values{}
	query.Set("token_id", tokenID)
	body, err := c.get(ctx, "/book", query)
	if err != nil {
		return OrderBook{}, err
	}

	var raw struct {
		AssetID string `json:"asset_id"`
		TokenID string `json:"token_id"`
		Bids    []struct {
			Price string `json:"price"`
			Size  string `json:"size"`
		} `json:"bids"`
		Asks []struct {
			Price string `json:"price"`
			Size  string `json:"size"`
		} `json:"asks"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return OrderBook{}, fmt.Errorf("decode clob orderbook: %w", err)
	}

	book := OrderBook{TokenID: raw.TokenID}
	if book.TokenID == "" {
		book.TokenID = raw.AssetID
	}
	for _, entry := range raw.Bids {
		price, err := strconv.ParseFloat(entry.Price, 64)
		if err != nil {
			return OrderBook{}, fmt.Errorf("parse bid price: %w", err)
		}
		size, err := strconv.ParseFloat(entry.Size, 64)
		if err != nil {
			return OrderBook{}, fmt.Errorf("parse bid size: %w", err)
		}
		book.Bids = append(book.Bids, PriceLevel{Price: price, Size: size})
	}
	for _, entry := range raw.Asks {
		price, err := strconv.ParseFloat(entry.Price, 64)
		if err != nil {
			return OrderBook{}, fmt.Errorf("parse ask price: %w", err)
		}
		size, err := strconv.ParseFloat(entry.Size, 64)
		if err != nil {
			return OrderBook{}, fmt.Errorf("parse ask size: %w", err)
		}
		book.Asks = append(book.Asks, PriceLevel{Price: price, Size: size})
	}
	return book, nil
}

func (c *CLOBRESTClient) GetTradesSince(ctx context.Context, tokenID string, since time.Time) ([]Trade, error) {
	if strings.TrimSpace(tokenID) == "" {
		return nil, fmt.Errorf("tokenID is required")
	}
	if since.IsZero() {
		since = time.Now().Add(-5 * time.Minute).UTC()
	}

	query := url.Values{}
	query.Set("token_id", tokenID)
	query.Set("since", since.UTC().Format(time.RFC3339))
	body, err := c.get(ctx, "/trades", query)
	if err != nil {
		return nil, err
	}

	var raw []struct {
		ID        string `json:"id"`
		TokenID   string `json:"token_id"`
		AssetID   string `json:"asset_id"`
		Price     string `json:"price"`
		Size      string `json:"size"`
		Timestamp string `json:"timestamp"`
		MatchTime string `json:"match_time"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode clob trades: %w", err)
	}

	trades := make([]Trade, 0, len(raw))
	for _, entry := range raw {
		price, err := strconv.ParseFloat(entry.Price, 64)
		if err != nil {
			return nil, fmt.Errorf("parse trade price: %w", err)
		}
		size, err := strconv.ParseFloat(entry.Size, 64)
		if err != nil {
			return nil, fmt.Errorf("parse trade size: %w", err)
		}
		token := entry.TokenID
		if token == "" {
			token = entry.AssetID
		}
		occurredAt, err := parseTradeTime(entry.Timestamp, entry.MatchTime)
		if err != nil {
			return nil, err
		}
		trades = append(trades, Trade{ID: entry.ID, TokenID: token, Price: price, Size: size, OccurredAt: occurredAt})
	}
	return trades, nil
}

func (c *CLOBRESTClient) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("create request %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("clob %s returned status %d", path, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read clob %s response: %w", path, err)
	}
	return body, nil
}

func parseTradeTime(timestamp string, matchTime string) (time.Time, error) {
	if timestamp != "" {
		t, err := time.Parse(time.RFC3339, timestamp)
		if err == nil {
			return t, nil
		}
	}
	if matchTime != "" {
		if seconds, err := strconv.ParseInt(matchTime, 10, 64); err == nil {
			return time.Unix(seconds, 0).UTC(), nil
		}
		if t, err := time.Parse(time.RFC3339, matchTime); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("missing or invalid trade timestamp")
}
