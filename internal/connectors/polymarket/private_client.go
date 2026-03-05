package polymarket

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type PrivateAPICredentials struct {
	Address    string
	APIKey     string
	Passphrase string
	Secret     string
}

type PostOrderRequest struct {
	Order     any    `json:"order"`
	Owner     string `json:"owner"`
	OrderType string `json:"orderType,omitempty"`
	DeferExec bool   `json:"deferExec,omitempty"`
}

type PostOrderResponse struct {
	Success      bool     `json:"success"`
	OrderID      string   `json:"orderID"`
	Status       string   `json:"status"`
	MakingAmount string   `json:"makingAmount,omitempty"`
	TakingAmount string   `json:"takingAmount,omitempty"`
	TradeIDs     []string `json:"tradeIDs,omitempty"`
	ErrorMsg     string   `json:"errorMsg,omitempty"`
}

type CancelOrderResponse struct {
	Canceled    []string       `json:"canceled"`
	NotCanceled map[string]any `json:"not_canceled"`
}

type OpenOrder struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Owner        string `json:"owner"`
	Market       string `json:"market"`
	AssetID      string `json:"asset_id"`
	Side         string `json:"side"`
	OriginalSize string `json:"original_size"`
	SizeMatched  string `json:"size_matched"`
	Price        string `json:"price"`
	Outcome      string `json:"outcome"`
	Expiration   string `json:"expiration"`
	OrderType    string `json:"order_type"`
	CreatedAt    int64  `json:"created_at"`
}

type PrivateCLOBClient struct {
	baseURL    string
	creds      PrivateAPICredentials
	httpClient *http.Client
	now        func() time.Time
}

func NewPrivateCLOBClient(baseURL string, creds PrivateAPICredentials, httpClient *http.Client) *PrivateCLOBClient {
	return NewPrivateCLOBClientWithClock(baseURL, creds, httpClient, time.Now)
}

func NewPrivateCLOBClientWithClock(
	baseURL string,
	creds PrivateAPICredentials,
	httpClient *http.Client,
	now func() time.Time,
) *PrivateCLOBClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultCLOBBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if now == nil {
		now = time.Now
	}

	return &PrivateCLOBClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		creds:      creds,
		httpClient: httpClient,
		now:        now,
	}
}

func (c *PrivateCLOBClient) PostOrder(ctx context.Context, request PostOrderRequest) (PostOrderResponse, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return PostOrderResponse{}, fmt.Errorf("marshal post order request: %w", err)
	}

	var out PostOrderResponse
	if err := c.doJSON(ctx, http.MethodPost, "/order", body, &out); err != nil {
		return PostOrderResponse{}, err
	}
	return out, nil
}

func (c *PrivateCLOBClient) CancelOrder(ctx context.Context, orderID string) (CancelOrderResponse, error) {
	body, err := json.Marshal(map[string]string{"orderID": orderID})
	if err != nil {
		return CancelOrderResponse{}, fmt.Errorf("marshal cancel order request: %w", err)
	}

	var out CancelOrderResponse
	if err := c.doJSON(ctx, http.MethodDelete, "/order", body, &out); err != nil {
		return CancelOrderResponse{}, err
	}
	return out, nil
}

func (c *PrivateCLOBClient) GetOrder(ctx context.Context, orderID string) (OpenOrder, error) {
	path := "/order/" + url.PathEscape(orderID)

	var out OpenOrder
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return OpenOrder{}, err
	}
	return out, nil
}

func (c *PrivateCLOBClient) doJSON(ctx context.Context, method string, path string, body []byte, out any) error {
	if err := c.validateCreds(); err != nil {
		return err
	}

	target := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, target, bytesReader(body))
	if err != nil {
		return fmt.Errorf("create private request %s %s: %w", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	headers, err := c.l2Headers(method, path, body)
	if err != nil {
		return err
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute private request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read private response %s %s: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("private request %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	if out == nil {
		return nil
	}
	if len(responseBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		return fmt.Errorf("decode private response %s %s: %w", method, path, err)
	}
	return nil
}

func (c *PrivateCLOBClient) l2Headers(method string, path string, body []byte) (http.Header, error) {
	timestamp := c.now().UTC().Unix()
	bodyString := string(body)
	sig, err := BuildPolyHMACSignature(c.creds.Secret, timestamp, method, path, bodyString)
	if err != nil {
		return nil, fmt.Errorf("build POLY_SIGNATURE: %w", err)
	}

	headers := make(http.Header)
	headers.Set("POLY_ADDRESS", c.creds.Address)
	headers.Set("POLY_SIGNATURE", sig)
	headers.Set("POLY_TIMESTAMP", strconv.FormatInt(timestamp, 10))
	headers.Set("POLY_API_KEY", c.creds.APIKey)
	headers.Set("POLY_PASSPHRASE", c.creds.Passphrase)
	return headers, nil
}

func (c *PrivateCLOBClient) validateCreds() error {
	if strings.TrimSpace(c.creds.Address) == "" {
		return fmt.Errorf("POLY_ADDRESS credential is required")
	}
	if strings.TrimSpace(c.creds.APIKey) == "" {
		return fmt.Errorf("POLY_API_KEY credential is required")
	}
	if strings.TrimSpace(c.creds.Passphrase) == "" {
		return fmt.Errorf("POLY_PASSPHRASE credential is required")
	}
	if strings.TrimSpace(c.creds.Secret) == "" {
		return fmt.Errorf("POLY_SECRET credential is required")
	}
	return nil
}

func BuildPolyHMACSignature(secret string, timestamp int64, method string, requestPath string, body string) (string, error) {
	keyBytes, err := decodePolySecret(secret)
	if err != nil {
		return "", err
	}

	message := strconv.FormatInt(timestamp, 10) + method + requestPath + body
	h := hmac.New(sha256.New, keyBytes)
	if _, err := h.Write([]byte(message)); err != nil {
		return "", fmt.Errorf("write hmac message: %w", err)
	}

	sig := base64.StdEncoding.EncodeToString(h.Sum(nil))
	sig = strings.ReplaceAll(sig, "+", "-")
	sig = strings.ReplaceAll(sig, "/", "_")
	return sig, nil
}

func decodePolySecret(secret string) ([]byte, error) {
	sanitized := strings.ReplaceAll(secret, "-", "+")
	sanitized = strings.ReplaceAll(sanitized, "_", "/")

	var builder strings.Builder
	for _, r := range sanitized {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
			builder.WriteRune(r)
		}
	}

	prepared := builder.String()
	if mod := len(prepared) % 4; mod != 0 {
		prepared += strings.Repeat("=", 4-mod)
	}

	decoded, err := base64.StdEncoding.DecodeString(prepared)
	if err != nil {
		return nil, fmt.Errorf("decode POLY secret: %w", err)
	}
	return decoded, nil
}

func bytesReader(body []byte) io.Reader {
	if len(body) == 0 {
		return nil
	}
	return strings.NewReader(string(body))
}
