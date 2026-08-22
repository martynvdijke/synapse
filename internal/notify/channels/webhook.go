package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"synapse/internal/logging"
	"synapse/internal/notify"
)

// Webhook delivers messages to a generic HTTP endpoint as a JSON envelope.
type Webhook struct {
	cfg  Config
	http *http.Client
}

// NewWebhook builds a generic webhook adapter from the given config.
func NewWebhook(cfg Config) *Webhook {
	return &Webhook{cfg: cfg, http: newHTTPClient()}
}

// Name returns the channel name.
func (w *Webhook) Name() string { return TypeWebhook }

// Enabled reports whether an endpoint URL is configured and the channel is on.
func (w *Webhook) Enabled() bool { return w.cfg.Enabled && strings.TrimSpace(w.cfg.URL) != "" }

// webhookEnvelope is the JSON body posted to the endpoint.
type webhookEnvelope struct {
	Category  string `json:"category"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// Send posts the structured envelope to the configured endpoint.
func (w *Webhook) Send(ctx context.Context, cat notify.Category, title, message string) error {
	if !w.Enabled() {
		return fmt.Errorf("webhook not configured (url empty or disabled)")
	}
	body, err := json.Marshal(webhookEnvelope{
		Category:  string(cat),
		Title:     title,
		Message:   message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(w.cfg.URL, "/"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+w.cfg.Token)
	}

	start := time.Now()
	resp, err := w.http.Do(req)
	if err != nil {
		logging.LogError("notify", "Failed to send webhook message",
			slog.String("error", err.Error()),
			slog.String("error_kind", string(logging.ErrorKindNetwork)),
			slog.Duration("duration", time.Since(start)),
		)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		logging.LogError("notify", "Webhook returned non-OK status",
			slog.Int("status", resp.StatusCode),
			slog.String("error_kind", string(logging.ErrorKindServer)),
			slog.String("response_body_snippet", strings.TrimSpace(string(body))),
			slog.Duration("duration", time.Since(start)),
		)
		return fmt.Errorf("webhook send failed: status %d", resp.StatusCode)
	}
	logging.LogInfo("notify", "Webhook message sent",
		slog.String("title", title),
		slog.Duration("duration", time.Since(start)),
	)
	return nil
}
