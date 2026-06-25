package kuma

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

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"synapse/internal/logging"
)

type Client struct {
	url      string
	token    string
	username string // stored for Socket.IO re-use
	password string // stored for Socket.IO re-use
	client   *http.Client
	tracer   trace.Tracer
}

type Monitor struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	URL             string `json:"url,omitempty"`
	DockerContainer string `json:"docker_container,omitempty"`
	DockerHost      int    `json:"docker_host,omitempty"`
}

type DockerHost struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type LoginResult struct {
	Token string `json:"token"`
}

func NewClient(url string) *Client {
	return &Client{
		url: url,
		client: &http.Client{
			Transport: otelhttp.NewTransport(http.DefaultTransport),
			Timeout:   30 * time.Second,
		},
		tracer: otel.Tracer("kuma"),
	}
}

func (c *Client) Login(username, password string) error {
	c.username = username
	c.password = password
	_, span := c.tracer.Start(context.Background(), "Login",
		trace.WithAttributes(attribute.String("kuma_url", c.url)),
	)
	defer span.End()

	start := time.Now()
	logging.LogDebug("kuma", "Logging into Uptime Kuma",
		slog.String("kuma_url", c.url),
	)

	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})

	resp, err := c.client.Post(fmt.Sprintf("%s/api/login", c.url), "application/json", bytes.NewReader(body))
	if err != nil {
		errKind := logging.ErrorKindNetwork
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host") || strings.Contains(err.Error(), "timeout") {
			errKind = logging.ErrorKindNetwork
		}
		logging.LogError("kuma", "Kuma login failed",
			slog.String("kuma_url", c.url),
			slog.String("error", err.Error()),
			slog.String("error_kind", string(errKind)),
			slog.Duration("duration", time.Since(start)),
		)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errKind := logging.ErrorKindAuth
		if resp.StatusCode >= 500 {
			errKind = logging.ErrorKindServer
		}
		bodySnippet := ""
		if bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 200)); readErr == nil {
			bodySnippet = strings.TrimSpace(string(bodyBytes))
		}
		err := fmt.Errorf("login failed: status %d", resp.StatusCode)
		logging.LogError("kuma", "Kuma login failed",
			slog.String("kuma_url", c.url),
			slog.Int("status", resp.StatusCode),
			slog.String("error_kind", string(errKind)),
			slog.String("response_body_snippet", bodySnippet),
			slog.Duration("duration", time.Since(start)),
		)
		return err
	}

	var result LoginResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logging.LogError("kuma", "Kuma login response parse failed",
			slog.String("error", err.Error()),
			slog.String("error_kind", string(logging.ErrorKindParse)),
			slog.Duration("duration", time.Since(start)),
		)
		return err
	}
	if result.Token == "" {
		err := fmt.Errorf("no token received")
		logging.LogError("kuma", "Kuma login received empty token",
			slog.Duration("duration", time.Since(start)),
		)
		return err
	}
	c.token = result.Token
	logging.LogInfo("kuma", "Kuma login successful",
		slog.Duration("duration", time.Since(start)),
	)
	return nil
}

func (c *Client) doRequest(method, path string, body []byte) (*http.Response, error) {
	start := time.Now()
	req, err := http.NewRequest(method, fmt.Sprintf("%s%s", c.url, path), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		errKind := logging.ErrorKindNetwork
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host") || strings.Contains(err.Error(), "timeout") {
			errKind = logging.ErrorKindNetwork
		}
		logging.LogError("kuma", "HTTP request failed",
			slog.String("method", method),
			slog.String("path", path),
			slog.String("error", err.Error()),
			slog.String("error_kind", string(errKind)),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}

	if resp.StatusCode >= 400 {
		errKind := logging.ErrorKindServer
		if resp.StatusCode == 404 {
			errKind = logging.ErrorKindNotFound
		} else if resp.StatusCode == 401 || resp.StatusCode == 403 {
			errKind = logging.ErrorKindAuth
		}
		bodySnippet := ""
		if bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 200)); readErr == nil {
			bodySnippet = strings.TrimSpace(string(bodyBytes))
			// Re-create the body for the caller
			resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(bodyBytes), resp.Body))
		}
		logging.LogError("kuma", "HTTP request failed",
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status", resp.StatusCode),
			slog.String("error_kind", string(errKind)),
			slog.String("response_body_snippet", bodySnippet),
			slog.Duration("duration", time.Since(start)),
		)
		return resp, fmt.Errorf("%s %s: status %d", method, path, resp.StatusCode)
	}

	logging.LogDebug("kuma", "HTTP request completed",
		slog.String("method", method),
		slog.String("path", path),
		slog.Int("status", resp.StatusCode),
		slog.Duration("duration", time.Since(start)),
	)
	return resp, nil
}

func (c *Client) GetDockerHosts() ([]DockerHost, error) {
	_, span := c.tracer.Start(context.Background(), "GetDockerHosts")
	defer span.End()

	start := time.Now()
	logging.LogDebug("kuma", "Fetching Docker hosts")

	resp, err := c.doRequest("GET", "/api/docker-hosts", nil)
	if err != nil {
		logging.LogError("kuma", "Failed to fetch Docker hosts",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("get docker hosts: status %d", resp.StatusCode)
		logging.LogError("kuma", "Failed to fetch Docker hosts",
			slog.Int("status", resp.StatusCode),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}

	var hosts []DockerHost
	if err := json.NewDecoder(resp.Body).Decode(&hosts); err != nil {
		logging.LogError("kuma", "Failed to decode Docker hosts response",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}
	logging.LogInfo("kuma", "Fetched Docker hosts",
		slog.Int("count", len(hosts)),
		slog.Duration("duration", time.Since(start)),
	)
	return hosts, nil
}

func (c *Client) GetMonitors() ([]Monitor, error) {
	_, span := c.tracer.Start(context.Background(), "GetMonitors")
	defer span.End()

	start := time.Now()
	logging.LogDebug("kuma", "Fetching monitors")

	resp, err := c.doRequest("GET", "/api/monitors", nil)
	if err != nil {
		logging.LogError("kuma", "Failed to fetch monitors",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("get monitors: status %d", resp.StatusCode)
		logging.LogError("kuma", "Failed to fetch monitors",
			slog.Int("status", resp.StatusCode),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}

	var result struct {
		Monitors []Monitor `json:"monitors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logging.LogError("kuma", "Failed to decode monitors response",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}
	logging.LogInfo("kuma", "Fetched monitors",
		slog.Int("count", len(result.Monitors)),
		slog.Duration("duration", time.Since(start)),
	)
	return result.Monitors, nil
}

func (c *Client) AddMonitor(monitorType, name, url, dockerContainer string, dockerHostID int) (int, error) {
	_, span := c.tracer.Start(context.Background(), "AddMonitor",
		trace.WithAttributes(
			attribute.String("monitor_type", monitorType),
			attribute.String("name", name),
		),
	)
	defer span.End()

	start := time.Now()
	logging.LogDebug("kuma", "Adding monitor",
		slog.String("type", monitorType),
		slog.String("name", name),
		slog.String("url", url),
	)

	payload := map[string]any{
		"name":          name,
		"type":          monitorType,
		"interval":      60,
		"retryInterval": 60,
		"maxretries":    3,
		"conditions":    []any{},
	}

	switch monitorType {
	case "http":
		payload["url"] = url
		payload["method"] = "GET"
		payload["accepted_statuscodes"] = []int{200, 201, 204, 301, 302}
	case "docker":
		payload["docker_container"] = dockerContainer
		payload["docker_host"] = dockerHostID
	}

	body, _ := json.Marshal(payload)
	resp, err := c.doRequest("POST", "/api/monitors", body)
	if err != nil {
		logging.LogError("kuma", "Failed to add monitor",
			slog.String("name", name),
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("add monitor: status %d", resp.StatusCode)
		logging.LogError("kuma", "Failed to add monitor",
			slog.String("name", name),
			slog.Int("status", resp.StatusCode),
			slog.Duration("duration", time.Since(start)),
		)
		return 0, err
	}

	var m Monitor
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		logging.LogError("kuma", "Failed to decode add monitor response",
			slog.String("name", name),
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return 0, err
	}
	logging.LogInfo("kuma", "Monitor added successfully",
		slog.String("name", name),
		slog.Int("monitor_id", m.ID),
		slog.Duration("duration", time.Since(start)),
	)
	return m.ID, nil
}
