package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultGammaBaseURL = "https://gamma-api.polymarket.com"

type Market struct {
	ID      string
	EventID string
	Title   string
	Active  bool
}

type ExternalEvent struct {
	ID    string
	Title string
}

type GammaClient interface {
	GetMarkets(ctx context.Context, activeOnly bool) ([]Market, error)
	GetEvents(ctx context.Context) ([]ExternalEvent, error)
}

type GammaRESTClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewDefaultGammaRESTClient() *GammaRESTClient {
	return NewGammaRESTClient(DefaultGammaBaseURL, nil)
}

func NewGammaRESTClient(baseURL string, httpClient *http.Client) *GammaRESTClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultGammaBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &GammaRESTClient{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}
}

func (c *GammaRESTClient) GetMarkets(ctx context.Context, activeOnly bool) ([]Market, error) {
	query := url.Values{}
	if activeOnly {
		query.Set("active", "true")
	}

	body, err := c.get(ctx, "/markets", query)
	if err != nil {
		return nil, err
	}

	var raw []struct {
		ID       string `json:"id"`
		EventID  string `json:"event_id"`
		Question string `json:"question"`
		Title    string `json:"title"`
		Active   bool   `json:"active"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode gamma markets: %w", err)
	}

	markets := make([]Market, 0, len(raw))
	for _, entry := range raw {
		title := entry.Title
		if title == "" {
			title = entry.Question
		}
		markets = append(markets, Market{ID: entry.ID, EventID: entry.EventID, Title: title, Active: entry.Active})
	}
	return markets, nil
}

func (c *GammaRESTClient) GetEvents(ctx context.Context) ([]ExternalEvent, error) {
	body, err := c.get(ctx, "/events", nil)
	if err != nil {
		return nil, err
	}

	var events []ExternalEvent
	if err := json.Unmarshal(body, &events); err != nil {
		return nil, fmt.Errorf("decode gamma events: %w", err)
	}
	return events, nil
}

func (c *GammaRESTClient) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
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
		return nil, fmt.Errorf("gamma %s returned status %d", path, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gamma %s response: %w", path, err)
	}
	return body, nil
}
