package npm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"synapse/internal/logging"
)

type ProxyHost struct {
	ID          int              `json:"id"`
	DomainNames []string         `json:"domain_names"`
	Forwarding  ForwardingConfig `json:"forwarding"`
}

type ForwardingConfig struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Container string `json:"container"`
	Protocol  string `json:"protocol"`
}

type ProxyEntry struct {
	CNAME     string `json:"cname"`
	Container string `json:"container"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`
	SourceInstanceID int `json:"source_instance_id,omitempty"`
}

var ErrNPM2FARequired = errors.New("NPM account has 2FA enabled. Create a dedicated API account without 2FA for Synapse")

type Client struct {
	url         string
	user        string
	pass        string
	token       string
	tokenExpiry time.Time
	client      *http.Client
	tracer      trace.Tracer
}

func NewClient(url, user, pass string) *Client {
	return &Client{
		url: url, user: user, pass: pass,
		client: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   30 * time.Second,
		},
		tracer: otel.Tracer("npm"),
	}
}

func (c *Client) Login() error {
	start := time.Now()
	logging.LogDebug("npm", "Logging into NPM via /api/tokens",
		slog.String("npm_url", c.url),
	)

	body, _ := json.Marshal(map[string]string{
		"identity": c.user,
		"secret":   c.pass,
	})

	req, err := http.NewRequest("POST", c.url+"/api/tokens", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		errKind := logging.ErrorKindNetwork
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host") || strings.Contains(err.Error(), "timeout") {
			errKind = logging.ErrorKindNetwork
		}
		logging.LogError("npm", "NPM login failed",
			slog.String("error", err.Error()),
			slog.String("error_kind", string(errKind)),
			slog.Duration("duration", time.Since(start)),
		)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid NPM credentials")
	}
	if resp.StatusCode != http.StatusOK {
		bodySnippet := ""
		if bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 200)); readErr == nil {
			bodySnippet = strings.TrimSpace(string(bodyBytes))
		}
		err := fmt.Errorf("login failed: status %d", resp.StatusCode)
		logging.LogError("npm", "NPM login returned non-OK status",
			slog.Int("status", resp.StatusCode),
			slog.String("response_body_snippet", bodySnippet),
			slog.Duration("duration", time.Since(start)),
		)
		return err
	}

	var result struct {
		Token        string `json:"token"`
		Expires      string `json:"expires"`
		Requires2FA  bool   `json:"requires_2fa"`
		ChallengeTok string `json:"challenge_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logging.LogError("npm", "NPM login response parse failed",
			slog.String("error", err.Error()),
			slog.String("error_kind", string(logging.ErrorKindParse)),
			slog.Duration("duration", time.Since(start)),
		)
		return err
	}

	if result.Requires2FA {
		return ErrNPM2FARequired
	}

	c.token = result.Token
	// Default 1 day expiry; parse if server provides RFC3339
	c.tokenExpiry = time.Now().Add(24 * time.Hour)
	if result.Expires != "" {
		if t, err := time.Parse(time.RFC3339, result.Expires); err == nil {
			c.tokenExpiry = t
		}
	}

	logging.LogInfo("npm", "NPM login successful",
		slog.Duration("duration", time.Since(start)),
	)
	return nil
}

func (c *Client) ensureLoggedIn() error {
	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		return nil
	}
	return c.Login()
}

func (c *Client) GetProxyHosts() ([]ProxyEntry, error) {
	_, span := c.tracer.Start(context.Background(), "GetProxyHosts",
		trace.WithAttributes(attribute.String("npm_url", c.url)),
	)
	defer span.End()

	start := time.Now()
	logging.LogDebug("npm", "Fetching proxy hosts from NPM",
		slog.String("npm_url", c.url),
	)

	url := fmt.Sprintf("%s/api/nginx/proxy-hosts", c.url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		logging.LogError("npm", "Failed to create NPM request",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}
	if err := c.ensureLoggedIn(); err != nil {
		logging.LogError("npm", "Failed to authenticate to NPM",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		errKind := logging.ErrorKindNetwork
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host") || strings.Contains(err.Error(), "timeout") {
			errKind = logging.ErrorKindNetwork
		}
		logging.LogError("npm", "Failed to fetch proxy hosts from NPM",
			slog.String("error", err.Error()),
			slog.String("error_kind", string(errKind)),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errKind := logging.ErrorKindServer
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			errKind = logging.ErrorKindAuth
		}
		bodySnippet := ""
		if bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 200)); readErr == nil {
			bodySnippet = strings.TrimSpace(string(bodyBytes))
		}
		err := fmt.Errorf("failed to get proxy hosts: status %d", resp.StatusCode)
		logging.LogError("npm", "NPM request returned non-OK status",
			slog.Int("status", resp.StatusCode),
			slog.String("error_kind", string(errKind)),
			slog.String("response_body_snippet", bodySnippet),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}

	var hosts []ProxyHost
	if err := json.NewDecoder(resp.Body).Decode(&hosts); err != nil {
		logging.LogError("npm", "Failed to decode NPM proxy hosts response",
			slog.String("error", err.Error()),
			slog.String("error_kind", string(logging.ErrorKindParse)),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}

	var entries []ProxyEntry
	for _, host := range hosts {
		forwarding := host.Forwarding
		if len(host.DomainNames) == 0 || forwarding.Container == "" {
			continue
		}

		for _, domain := range host.DomainNames {
			entries = append(entries, ProxyEntry{
				CNAME:     domain,
				Container: forwarding.Container,
				Host:      forwarding.Host,
				Port:      forwarding.Port,
				Protocol:  forwarding.Protocol,
			})
		}
	}

	logging.LogInfo("npm", "Fetched proxy hosts from NPM",
		slog.Int("host_count", len(hosts)),
		slog.Int("entry_count", len(entries)),
		slog.Duration("duration", time.Since(start)),
	)

	return entries, nil
}

var npmTracer = otel.Tracer("npm")

// GetProxyHosts is the legacy free function wrapper for backward compat.
func GetProxyHosts(npmHost, npmUser, npmPass string) ([]ProxyEntry, error) {
	c := NewClient(npmHost, npmUser, npmPass)
	if err := c.Login(); err != nil {
		return nil, err
	}
	return c.GetProxyHosts()
}
