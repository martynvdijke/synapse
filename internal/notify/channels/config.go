// Package channels provides notification channel adapters (ntfy, Telegram,
// Discord, generic webhook) implementing notify.Notifier, plus configuration
// parsing for the notify_channels settings document.
package channels

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"synapse/internal/notify"
)

// Channel types supported by the adapters.
const (
	TypeGotify   = "gotify"
	TypeNtfy     = "ntfy"
	TypeTelegram = "telegram"
	TypeDiscord  = "discord"
	TypeWebhook  = "webhook"
)

// Config is one entry of the notify_channels settings document.
type Config struct {
	Type     string `json:"type"`
	Enabled  bool   `json:"enabled"`
	URL      string `json:"url"`
	Token    string `json:"token,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

// sendTimeout bounds every adapter's HTTP call.
const sendTimeout = 10 * time.Second

// ValidTypes lists the supported channel type identifiers.
func ValidTypes() []string {
	return []string{TypeGotify, TypeNtfy, TypeTelegram, TypeDiscord, TypeWebhook}
}

// ParseChannels decodes the notify_channels JSON document. An empty document
// yields an empty slice (legacy gotify_* keys then apply).
func ParseChannels(doc string) ([]Config, error) {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return nil, nil
	}
	var cfgs []Config
	if err := json.Unmarshal([]byte(doc), &cfgs); err != nil {
		return nil, fmt.Errorf("parse notify_channels: %w", err)
	}
	for _, c := range cfgs {
		switch c.Type {
		case TypeGotify, TypeNtfy, TypeTelegram, TypeDiscord, TypeWebhook:
		default:
			return nil, fmt.Errorf("parse notify_channels: unknown channel type %q", c.Type)
		}
	}
	return cfgs, nil
}

// Build constructs the Notifier for one config entry.
func Build(cfg Config) (notify.Notifier, error) {
	switch cfg.Type {
	case TypeGotify:
		return notify.NewClient(cfg.URL, cfg.Token, cfg.Priority), nil
	case TypeNtfy:
		return NewNtfy(cfg), nil
	case TypeTelegram:
		return NewTelegram(cfg), nil
	case TypeDiscord:
		return NewDiscord(cfg), nil
	case TypeWebhook:
		return NewWebhook(cfg), nil
	default:
		return nil, fmt.Errorf("unknown channel type %q", cfg.Type)
	}
}

// BuildAll constructs notifiers for every enabled config entry, skipping
// entries that fail to build (with an error reported per index).
func BuildAll(cfgs []Config) ([]notify.Notifier, []error) {
	var (
		out   []notify.Notifier
		errs  []error
	)
	for i, cfg := range cfgs {
		n, err := Build(cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("channel %d (%s): %w", i, cfg.Type, err))
			continue
		}
		if !cfg.Enabled {
			continue
		}
		out = append(out, n)
	}
	return out, errs
}

// newHTTPClient returns the shared adapter HTTP client.
func newHTTPClient() *http.Client {
	return &http.Client{Timeout: sendTimeout}
}
