// Package notify delivers alerts: always to the log, and to a webhook when one
// is configured.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Amadeus-22/permwatch/internal/app"
)

// Log writes every alert to the structured log.
type Log struct{ Logger *slog.Logger }

// Notify logs one line per change.
func (l Log) Notify(_ context.Context, alert app.Alert) error {
	for _, c := range alert.Changes {
		l.Logger.Warn("permissions changed", "address", string(alert.Address),
			"kind", c.Kind, "permission_id", c.PermissionID, "detail", c.Message)
	}
	return nil
}

// Webhook POSTs the alert as JSON.
type Webhook struct {
	URL  string
	HTTP *http.Client
}

// NewWebhook returns a webhook notifier with a request timeout.
func NewWebhook(url string, timeout time.Duration) *Webhook {
	return &Webhook{URL: url, HTTP: &http.Client{Timeout: timeout}}
}

// Notify sends the alert; any status outside 2xx is an error, so the watcher
// tries again on its next cycle.
func (w *Webhook) Notify(ctx context.Context, alert app.Alert) error {
	body, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("encode alert: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("post webhook: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("post webhook: status %d", resp.StatusCode)
	}
	return nil
}

// Multi fans an alert out to several notifiers and reports every failure.
type Multi []app.Notifier

// Notify calls each notifier; one failing does not stop the others.
func (m Multi) Notify(ctx context.Context, alert app.Alert) error {
	var errs []error
	for _, n := range m {
		if err := n.Notify(ctx, alert); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
