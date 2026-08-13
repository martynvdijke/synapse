package kuma

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var ErrRESTNotSupported = errors.New("REST API not supported by Uptime Kuma; use Socket.IO equivalents")

// ClientTestHooks allows overriding Socket.IO methods for testing.
// When a hook is set on a Client, it takes priority over the global
// function variable defaults.
type ClientTestHooks struct {
	QueryMonitors func() ([]KumaMonitor, error)
	AddMonitor    func(monitorType, name, url, dockerContainer string, dockerHostID int) (int, error)
	DeleteMonitor func(monitorID int) error
	EditMonitor   func(monitorID int, payload map[string]any) error
}

type Client struct {
	url       string
	token     string
	username  string // stored for Socket.IO re-use
	password  string // stored for Socket.IO re-use
	client    *http.Client
	tracer    trace.Tracer
	testHooks *ClientTestHooks // only set in tests

	mu            sync.Mutex
	monCache      []KumaMonitor // cached monitor query result
	monCacheAt    time.Time     // when monCache was populated
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

// Deprecated: Uptime Kuma has no POST /api/login endpoint.
// Use Socket.IO login via QueryMonitorsViaSocketIO or AddMonitorViaSocketIO.
func (c *Client) Login(username, password string) error {
	c.username = username
	c.password = password
	return ErrRESTNotSupported
}

func (c *Client) GetDockerHosts() ([]DockerHost, error) {
	return nil, ErrRESTNotSupported
}

// Deprecated: Uptime Kuma has no GET /api/monitors endpoint. Use QueryMonitorsViaSocketIO.
func (c *Client) GetMonitors() ([]Monitor, error) {
	return nil, ErrRESTNotSupported
}

// Deprecated: Uptime Kuma has no POST /api/monitors endpoint. Use AddMonitorViaSocketIO.
func (c *Client) AddMonitor(monitorType, name, url, dockerContainer string, dockerHostID int) (int, error) {
	return 0, ErrRESTNotSupported
}

// Function variables used by wrapper methods below. Overridden in tests to
// avoid needing a live Socket.IO server.
var queryMonitorsFn = QueryMonitorsViaSocketIO
var addMonitorFn = AddMonitorViaSocketIO

// Test hooks — exported for use by tests in external packages.
// Each returns a restore function; use with defer or t.Cleanup.
func SetQueryMonitorsTestHook(fn func(url, user, pass string) ([]KumaMonitor, error)) func() {
	orig := queryMonitorsFn
	queryMonitorsFn = fn
	return func() { queryMonitorsFn = orig }
}

func SetAddMonitorTestHook(fn func(url, user, pass string, monitorType, name, monURL, dockerContainer string, dockerHostID int) (int, error)) func() {
	orig := addMonitorFn
	addMonitorFn = fn
	return func() { addMonitorFn = orig }
}

// deleteMonitorFn / editMonitorFn are package-level hooks overridable in
// tests without a real Kuma Socket.IO server.
var (
	deleteMonitorFn = DeleteMonitorViaSocketIO
	editMonitorFn   = EditMonitorViaSocketIO
)

// SetDeleteMonitorTestHook installs a package-level override for
// DeleteMonitorViaSocketIO. Returns a restore func.
func SetDeleteMonitorTestHook(fn func(url, user, pass string, monitorID int) error) func() {
	orig := deleteMonitorFn
	deleteMonitorFn = fn
	return func() { deleteMonitorFn = orig }
}

// SetEditMonitorTestHook installs a package-level override for
// EditMonitorViaSocketIO. Returns a restore func.
func SetEditMonitorTestHook(fn func(url, user, pass string, monitorID int, payload map[string]any) error) func() {
	orig := editMonitorFn
	editMonitorFn = fn
	return func() { editMonitorFn = orig }
}

// SetTestHooks installs per-client test overrides for Socket.IO methods.
// Use in tests to avoid needing a real Kuma Socket.IO server.
// Pass nil to clear hooks.
func (c *Client) SetTestHooks(hooks *ClientTestHooks) {
	c.testHooks = hooks
}

// monitorCacheTTL bounds how long a cached Socket.IO monitor query result is
// reused. The dashboard fires several API calls in a burst (status, services,
// proxies, monitors); without caching each call would open a fresh Socket.IO
// connection and block for the full collection window. 15s keeps the burst
// instant while still letting sync operations see recent changes.
const monitorCacheTTL = 15 * time.Second

// QueryMonitorsViaSocketIO fetches monitors from Kuma via Socket.IO using
// the Client's stored credentials. Results are cached for monitorCacheTTL so
// the burst of dashboard requests shares a single Socket.IO collection.
func (c *Client) QueryMonitorsViaSocketIO() ([]KumaMonitor, error) {
	if c.testHooks != nil && c.testHooks.QueryMonitors != nil {
		return c.testHooks.QueryMonitors()
	}
	c.mu.Lock()
	if c.monCache != nil && time.Since(c.monCacheAt) < monitorCacheTTL {
		out := c.monCache
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	monitors, err := queryMonitorsFn(c.url, c.username, c.password)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.monCache = monitors
	c.monCacheAt = time.Now()
	c.mu.Unlock()
	return monitors, nil
}

// AddMonitorViaSocketIO creates a monitor in Kuma via Socket.IO using the
// Client's stored credentials. A successful add invalidates the monitor query
// cache so subsequent queries reflect the new monitor.
func (c *Client) AddMonitorViaSocketIO(monitorType, name, url, dockerContainer string, dockerHostID int) (int, error) {
	if c.testHooks != nil && c.testHooks.AddMonitor != nil {
		return c.testHooks.AddMonitor(monitorType, name, url, dockerContainer, dockerHostID)
	}
	id, err := addMonitorFn(c.url, c.username, c.password, monitorType, name, url, dockerContainer, dockerHostID)
	if err == nil {
		c.mu.Lock()
		c.monCache = nil
		c.mu.Unlock()
	}
	return id, err
}

// DeleteMonitorViaSocketIO removes a monitor from Kuma using the Client's
// stored credentials. A successful delete invalidates the monitor query cache.
func (c *Client) DeleteMonitorViaSocketIO(monitorID int) error {
	if c.testHooks != nil && c.testHooks.DeleteMonitor != nil {
		return c.testHooks.DeleteMonitor(monitorID)
	}
	err := deleteMonitorFn(c.url, c.username, c.password, monitorID)
	if err == nil {
		c.mu.Lock()
		c.monCache = nil
		c.mu.Unlock()
	}
	return err
}

// EditMonitorViaSocketIO updates a monitor in Kuma using the Client's stored
// credentials. A successful edit invalidates the monitor query cache.
func (c *Client) EditMonitorViaSocketIO(monitorID int, payload map[string]any) error {
	if c.testHooks != nil && c.testHooks.EditMonitor != nil {
		return c.testHooks.EditMonitor(monitorID, payload)
	}
	err := editMonitorFn(c.url, c.username, c.password, monitorID, payload)
	if err == nil {
		c.mu.Lock()
		c.monCache = nil
		c.mu.Unlock()
	}
	return err
}
