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

// Telegram delivers messages via the Telegram Bot API sendMessage call.
// The configured URL carries the bot API base and chat id as path suffix:
// <base>/bot<token>/<chat_id> (token may also live in Token and is appended
// automatically when the URL lacks a bot segment).
type Telegram struct {
	cfg  Config
	http *http.Client
}

// NewTelegram builds a Telegram adapter from the given config.
func NewTelegram(cfg Config) *Telegram {
	return &Telegram{cfg: cfg, http: newHTTPClient()}
}

// Name returns the channel name.
func (t *Telegram) Name() string { return TypeTelegram }

// Enabled reports whether the channel is on and endpoint + chat id resolvable.
func (t *Telegram) Enabled() bool {
	return t.cfg.Enabled && strings.TrimSpace(t.cfg.URL) != "" && t.chatID() != ""
}

// telegramMessage is the sendMessage payload.
type telegramMessage struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

// Send posts to /bot<token>/sendMessage with plain text (no parse mode).
func (t *Telegram) Send(ctx context.Context, _ notify.Category, title, message string) error {
	if !t.Enabled() {
		return fmt.Errorf("telegram not configured (url/chat id empty or disabled)")
	}
	endpoint := t.endpoint()
	text := message
	if title != "" {
		text = title + "\n" + message
	}
	body, err := json.Marshal(telegramMessage{ChatID: t.chatID(), Text: text})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := t.http.Do(req)
	if err != nil {
		logging.LogError("notify", "Failed to send Telegram message",
			slog.String("error", err.Error()),
			slog.String("error_kind", string(logging.ErrorKindNetwork)),
			slog.Duration("duration", time.Since(start)),
		)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		logging.LogError("notify", "Telegram returned non-OK status",
			slog.Int("status", resp.StatusCode),
			slog.String("error_kind", string(logging.ErrorKindAuth)),
			slog.String("response_body_snippet", strings.TrimSpace(string(body))),
			slog.Duration("duration", time.Since(start)),
		)
		return fmt.Errorf("telegram send failed: status %d", resp.StatusCode)
	}
	logging.LogInfo("notify", "Telegram message sent",
		slog.String("title", title),
		slog.Duration("duration", time.Since(start)),
	)
	return nil
}

// chatID extracts the chat id from the URL path suffix.
func (t *Telegram) chatID() string {
	parts := strings.Split(strings.Trim(t.cfg.URL, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// endpoint assembles the sendMessage URL, injecting the bot token when the
// URL does not already contain a /bot<token> segment.
func (t *Telegram) endpoint() string {
	base := strings.TrimRight(t.cfg.URL, "/")
	if i := strings.LastIndex(base, "/"); i >= 0 && !strings.Contains(base[:i+1], "bot") {
		// base ends with the chat id; insert bot<token> before it.
		if t.cfg.Token != "" {
			return base[:i+1] + "bot" + t.cfg.Token + "/" + base[i+1:] + "/sendMessage"
		}
	}
	return base + "/sendMessage"
}
