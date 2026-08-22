package channels

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"synapse/internal/logging"
	"synapse/internal/notify"
)

// Ntfy delivers messages to an ntfy topic via HTTP POST.
type Ntfy struct {
	cfg  Config
	http *http.Client
}

// NewNtfy builds an ntfy adapter from the given config.
func NewNtfy(cfg Config) *Ntfy {
	return &Ntfy{cfg: cfg, http: newHTTPClient()}
}

// Name returns the channel name.
func (n *Ntfy) Name() string { return TypeNtfy }

// Enabled reports whether a topic URL is configured and the channel is on.
func (n *Ntfy) Enabled() bool { return n.cfg.Enabled && strings.TrimSpace(n.cfg.URL) != "" }

// Send posts the message body with Title/Priority/Tags headers, as expected
// by ntfy's HTTP API.
func (n *Ntfy) Send(ctx context.Context, cat notify.Category, title, message string) error {
	if !n.Enabled() {
		return fmt.Errorf("ntfy not configured (url empty or disabled)")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(n.cfg.URL, "/"), strings.NewReader(message))
	if err != nil {
		return err
	}
	if title != "" {
		req.Header.Set("Title", title)
	}
	prio := n.cfg.Priority
	if prio == 0 {
		prio = priorityForCategory(cat)
	}
	if prio > 0 {
		req.Header.Set("Priority", strconv.Itoa(prio))
	}
	req.Header.Set("Tags", string(cat))
	req.Header.Set("Markdown", "no")

	start := time.Now()
	resp, err := n.http.Do(req)
	if err != nil {
		logging.LogError("notify", "Failed to send ntfy message",
			slog.String("error", err.Error()),
			slog.String("error_kind", string(logging.ErrorKindNetwork)),
			slog.Duration("duration", time.Since(start)),
		)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		logging.LogError("notify", "ntfy returned non-OK status",
			slog.Int("status", resp.StatusCode),
			slog.String("error_kind", string(logging.ErrorKindServer)),
			slog.String("response_body_snippet", strings.TrimSpace(string(body))),
			slog.Duration("duration", time.Since(start)),
		)
		return fmt.Errorf("ntfy send failed: status %d", resp.StatusCode)
	}
	logging.LogInfo("notify", "ntfy message sent",
		slog.String("title", title),
		slog.Duration("duration", time.Since(start)),
	)
	return nil
}

// priorityForCategory maps notification categories to ntfy's 1–5 priority
// scale; unknown categories default to default (3).
func priorityForCategory(cat notify.Category) int {
	switch cat {
	case notify.CatDockerDie:
		return 4 // high: unexpected death
	case notify.CatDockerHealth:
		return 4
	default:
		return 3
	}
}
