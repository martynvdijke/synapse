package notify

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
)

// gotifyTracer is a placeholder for future OpenTelemetry instrumentation of
// the Gotify client. Kept minimal to avoid importing otel in this package.
// Client sends application messages to a Gotify server.
type Client struct {
	url      string
	token    string
	priority int
	http     *http.Client
}

// NewClient creates a Gotify client. url is the Gotify server base URL
// (e.g. https://gotify.example.com), token the application token, and
// priority the message priority (0–10).
func NewClient(url, token string, priority int) *Client {
	if priority < 0 {
		priority = 0
	}
	if priority > 10 {
		priority = 10
	}
	return &Client{
		url:      strings.TrimRight(url, "/"),
		token:    token,
		priority: priority,
		http:     &http.Client{Timeout: 15 * time.Second},
	}
}

type gotifyMessage struct {
	Title    string `json:"title"`
	Message  string `json:"message"`
	Priority int    `json:"priority"`
}

// SendMessage posts an application message to the Gotify server.
// Errors are classified with logging.ErrorKind (network/auth/server/parse)
// following the connection-logging spec.
func (c *Client) SendMessage(ctx context.Context, title, message string) error {
	if c.url == "" || c.token == "" {
		return fmt.Errorf("gotify not configured (url or token empty)")
	}

	body, err := json.Marshal(gotifyMessage{Title: title, Message: message, Priority: c.priority})
	if err != nil {
		logging.LogError("notify", "Failed to encode Gotify message",
			slog.String("error", err.Error()),
			slog.String("error_kind", string(logging.ErrorKindParse)),
		)
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/message", bytes.NewReader(body))
	if err != nil {
		logging.LogError("notify", "Failed to build Gotify request",
			slog.String("error", err.Error()),
			slog.String("error_kind", string(logging.ErrorKindParse)),
		)
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", c.token)

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		logging.LogError("notify", "Failed to send Gotify message",
			slog.String("error", err.Error()),
			slog.String("error_kind", string(logging.ErrorKindNetwork)),
			slog.Duration("duration", time.Since(start)),
		)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		errKind := logging.ErrorKindAuth
		bodySnippet := ""
		if b, readErr := io.ReadAll(io.LimitReader(resp.Body, 200)); readErr == nil {
			bodySnippet = strings.TrimSpace(string(b))
		}
		logging.LogError("notify", "Gotify rejected the message (client error)",
			slog.Int("status", resp.StatusCode),
			slog.String("error_kind", string(errKind)),
			slog.String("response_body_snippet", bodySnippet),
			slog.Duration("duration", time.Since(start)),
		)
		return fmt.Errorf("gotify rejected message: status %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		errKind := logging.ErrorKindServer
		bodySnippet := ""
		if b, readErr := io.ReadAll(io.LimitReader(resp.Body, 200)); readErr == nil {
			bodySnippet = strings.TrimSpace(string(b))
		}
		logging.LogError("notify", "Gotify returned non-OK status",
			slog.Int("status", resp.StatusCode),
			slog.String("error_kind", string(errKind)),
			slog.String("response_body_snippet", bodySnippet),
			slog.Duration("duration", time.Since(start)),
		)
		return fmt.Errorf("gotify message send failed: status %d", resp.StatusCode)
	}

	logging.LogInfo("notify", "Gotify message sent",
		slog.String("title", title),
		slog.Int("priority", c.priority),
		slog.Duration("duration", time.Since(start)),
	)
	return nil
}
