package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultTelegramBaseURL = "https://api.telegram.org"

type TelegramMessage struct {
	ChatID              string
	Text                string
	DisableNotification bool
}

type TelegramNotifier struct {
	token      string
	baseURL    string
	httpClient *http.Client
	sleep      func(time.Duration)

	MaxRetries int
	ParseMode  string
}

func NewTelegramNotifier(token string, baseURL string, httpClient *http.Client) *TelegramNotifier {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultTelegramBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &TelegramNotifier{
		token:      token,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		sleep:      time.Sleep,
		MaxRetries: 3,
		ParseMode:  "HTML",
	}
}

func (n *TelegramNotifier) Send(ctx context.Context, message TelegramMessage) (int64, error) {
	if strings.TrimSpace(n.token) == "" {
		return 0, fmt.Errorf("telegram bot token is required")
	}
	if strings.TrimSpace(message.ChatID) == "" {
		return 0, fmt.Errorf("telegram chat id is required")
	}
	if strings.TrimSpace(message.Text) == "" {
		return 0, fmt.Errorf("telegram message text is required")
	}

	attempts := n.MaxRetries + 1
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		messageID, retryAfter, status, err := n.sendOnce(ctx, message)
		if err == nil {
			return messageID, nil
		}
		lastErr = err

		if attempt == attempts-1 {
			break
		}

		if status == http.StatusTooManyRequests {
			n.sleep(time.Duration(retryAfter) * time.Second)
			continue
		}
		if status >= 500 {
			n.sleep(time.Duration(attempt+1) * time.Second)
			continue
		}
		break
	}

	return 0, lastErr
}

func (n *TelegramNotifier) sendOnce(ctx context.Context, message TelegramMessage) (int64, int, int, error) {
	payload := map[string]any{
		"chat_id":              message.ChatID,
		"text":                 message.Text,
		"parse_mode":           n.ParseMode,
		"disable_notification": message.DisableNotification,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("marshal telegram request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", n.baseURL, n.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, 0, 0, fmt.Errorf("create telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("execute telegram request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, resp.StatusCode, fmt.Errorf("read telegram response: %w", err)
	}

	var envelope struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
		Result      struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return 0, 0, resp.StatusCode, fmt.Errorf("decode telegram response: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 && envelope.OK {
		return envelope.Result.MessageID, 0, resp.StatusCode, nil
	}
	if envelope.Parameters.RetryAfter < 0 {
		envelope.Parameters.RetryAfter = 0
	}
	return 0, envelope.Parameters.RetryAfter, resp.StatusCode, fmt.Errorf("telegram send failed: status=%d code=%d desc=%s", resp.StatusCode, envelope.ErrorCode, envelope.Description)
}
