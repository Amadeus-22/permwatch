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

	"github.com/Amadeus-22/permwatch/internal/app"
)

// TelegramAPI is the Bot API host. Tests point the notifier at an httptest server.
const TelegramAPI = "https://api.telegram.org"

// Telegram sends alerts as bot messages to one chat. The Bot API has no
// idempotency key, so a crash between sending and recording can repeat a message.
type Telegram struct {
	apiURL string // <base>/bot<token>/sendMessage
	chatID string
	client *http.Client
}

// NewTelegram returns a Telegram notifier with a request timeout.
func NewTelegram(baseURL, botToken, chatID string, timeout time.Duration) *Telegram {
	return &Telegram{
		apiURL: strings.TrimRight(baseURL, "/") + "/bot" + botToken + "/sendMessage",
		chatID: chatID,
		client: &http.Client{Timeout: timeout},
	}
}

// Notify sends the alert as a plain-text message.
func (t *Telegram) Notify(ctx context.Context, alert app.Alert) error {
	body, err := json.Marshal(map[string]any{"chat_id": t.chatID, "text": Text(alert), "disable_web_page_preview": true})
	if err != nil {
		return fmt.Errorf("encode telegram message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.apiURL, bytes.NewReader(body))
	if err != nil {
		return t.scrub(fmt.Errorf("build telegram request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return t.scrub(fmt.Errorf("send telegram message: %w", err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("send telegram message: status %d", resp.StatusCode)
	}
	return nil
}

// scrub removes the request URL from an error: it embeds the bot token, which
// must never reach the logs.
func (t *Telegram) scrub(err error) error {
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), t.apiURL, "<telegram-api>"))
}

// Text renders an alert for a human reader.
func Text(alert app.Alert) string {
	var b strings.Builder
	name := string(alert.Address)
	if alert.Label != "" {
		name = alert.Label + " (" + string(alert.Address) + ")"
	}

	switch alert.Kind {
	case app.AlertVaultNearLimit:
		fmt.Fprintf(&b, "Vault near its limit: %s\n", name)
		if alert.Vault != nil {
			for _, w := range alert.Vault.Warnings {
				fmt.Fprintf(&b, "[%s] %s\n", strings.ToUpper(string(w.Severity)), w.Message)
			}
		}
	default:
		fmt.Fprintf(&b, "Permissions changed: %s\n", name)
		for _, c := range alert.Changes {
			fmt.Fprintf(&b, "- %s\n", c.Message)
		}
		if len(alert.Findings) > 0 {
			b.WriteString("What the account allows now:\n")
			for _, f := range alert.Findings {
				fmt.Fprintf(&b, "[%s] %s\n", strings.ToUpper(string(f.Severity)), f.Message)
			}
		}
	}
	if alert.Owner != "" {
		fmt.Fprintf(&b, "Owner: %s\n", alert.Owner)
	}
	return strings.TrimRight(b.String(), "\n")
}
