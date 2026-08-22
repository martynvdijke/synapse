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

// Discord delivers messages to an incoming webhook.
type Discord struct {
	cfg  Config
	http *http.Client
}

// NewDiscord builds a Discord adapter from the given config.
func NewDiscord(cfg Config) *Discord {
	return &Discord{cfg: cfg, http: newHTTPClient()}
}

// Name returns the channel name.
func (d *Discord) Name() string { return TypeDiscord }

// Enabled reports whether a webhook URL is configured and the channel is on.
func (d *Discord) Enabled() bool { return d.cfg.Enabled && strings.TrimSpace(d.cfg.URL) != "" }

// discordMessage is the execute-webhook payload (plain content, no embeds).
type discordMessage struct {
	Content string `json:"content"`
}

// Send posts title + message as webhook content.
func (d *Discord) Send(ctx context.Context, _ notify.Category, title, message string) error {
	if !d.Enabled() {
		return fmt.Errorf("discord not configured (url empty or disabled)")
	}
	content := message
	if title != "" {
		content = "**" + title + "**\n" + message
	}
	body, err := json.Marshal(discordMessage{Content: content})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(d.cfg.URL, "/"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := d.http.Do(req)
	if err != nil {
		logging.LogError("notify", "Failed to send Discord message",
			slog.String("error", err.Error()),
			slog.String("error_kind", string(logging.ErrorKindNetwork)),
			slog.Duration("duration", time.Since(start)),
		)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		logging.LogError("notify", "Discord returned non-OK status",
			slog.Int("status", resp.StatusCode),
			slog.String("error_kind", string(logging.ErrorKindAuth)),
			slog.String("response_body_snippet", strings.TrimSpace(string(body))),
			slog.Duration("duration", time.Since(start)),
		)
		return fmt.Errorf("discord send failed: status %d", resp.StatusCode)
	}
	logging.LogInfo("notify", "Discord message sent",
		slog.String("title", title),
		slog.Duration("duration", time.Since(start)),
	)
	return nil
}
