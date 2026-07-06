package kuma

import (
	"errors"
	"net/http"
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
}

type Client struct {
	url       string
	token     string
	username  string // stored for Socket.IO re-use
	password  string // stored for Socket.IO re-use
	client    *http.Client
	tracer    trace.Tracer
	testHooks *ClientTestHooks // only set in tests
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

// SetTestHooks installs per-client test overrides for Socket.IO methods.
// Use in tests to avoid needing a real Kuma Socket.IO server.
// Pass nil to clear hooks.
func (c *Client) SetTestHooks(hooks *ClientTestHooks) {
	c.testHooks = hooks
}

// QueryMonitorsViaSocketIO fetches monitors from Kuma via Socket.IO using
// the Client's stored credentials.
func (c *Client) QueryMonitorsViaSocketIO() ([]KumaMonitor, error) {
	if c.testHooks != nil && c.testHooks.QueryMonitors != nil {
		return c.testHooks.QueryMonitors()
	}
	return queryMonitorsFn(c.url, c.username, c.password)
}

// AddMonitorViaSocketIO creates a monitor in Kuma via Socket.IO using the
// Client's stored credentials.
func (c *Client) AddMonitorViaSocketIO(monitorType, name, url, dockerContainer string, dockerHostID int) (int, error) {
	if c.testHooks != nil && c.testHooks.AddMonitor != nil {
		return c.testHooks.AddMonitor(monitorType, name, url, dockerContainer, dockerHostID)
	}
	return addMonitorFn(c.url, c.username, c.password, monitorType, name, url, dockerContainer, dockerHostID)
}
