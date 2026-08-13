// Package docker is a minimal hand-rolled client for the Docker Engine API
// over a unix socket (or any base URL in tests). It intentionally avoids the
// official docker SDK: we only need events, container listing/inspection and
// image inspection for reconciliation and event tracking.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultSocket returns the docker socket to use: DOCKER_SOCKET env override
// (format unix:///path) or the default /var/run/docker.sock.
func DefaultSocket() string {
	if s := os.Getenv("DOCKER_SOCKET"); s != "" {
		return s
	}
	return "unix:///var/run/docker.sock"
}

// Client is a minimal Docker Engine API client.
type Client struct {
	base  string // HTTP base, e.g. "http://docker" when speaking to a unix socket
	httpc *http.Client
}

// New builds a Client for the given socket URL ("unix:///path" or a plain
// path). TCP endpoints (tcp://host:port, http://...) are also accepted for
// testing/remote setups.
func New(socket string) (*Client, error) {
	var dial func(ctx context.Context, network, addr string) (net.Conn, error)
	base := "http://docker"

	switch {
	case strings.HasPrefix(socket, "unix://"):
		path := strings.TrimPrefix(socket, "unix://")
		dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		}
	case strings.HasPrefix(socket, "tcp://"):
		base = "http://" + strings.TrimPrefix(socket, "tcp://")
	case strings.HasPrefix(socket, "http://") || strings.HasPrefix(socket, "https://"):
		base = strings.TrimSuffix(socket, "/")
	default:
		dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}
	}

	tr := &http.Transport{
		DialContext: dial,
	}
	// Keep the client lean: no keepalives needed for short API calls, and
	// the events stream manages its own connection.
	tr.DisableKeepAlives = true

	return &Client{
		base: base,
		httpc: &http.Client{
			Transport: tr,
			Timeout:   30 * time.Second,
		},
	}, nil
}

// NewWithClient is a test helper: builds a Client against an arbitrary base
// URL using a caller-provided HTTP client.
func NewWithClient(base string, httpc *http.Client) *Client {
	return &Client{base: strings.TrimSuffix(base, "/"), httpc: httpc}
}

// Ping checks whether the docker daemon is reachable (GET /_ping).
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/_ping", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker ping: status %d", resp.StatusCode)
	}
	return nil
}

// ContainerSummary is a row from GET /containers/json.
type ContainerSummary struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Labels  map[string]string `json:"Labels"`
}

// ListContainers lists all containers including stopped ones.
func (c *Client) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/containers/json?all=1", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker list containers: status %d", resp.StatusCode)
	}
	var out []ContainerSummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// ContainerInspect is the subset of GET /containers/{id}/json we care about.
type ContainerInspect struct {
	ID      string `json:"Id"`
	Name    string `json:"Name"`
	Config  *struct {
		Image        string            `json:"Image"`
		Labels       map[string]string `json:"Labels"`
		Healthcheck  *struct {
			Test []string `json:"Test"`
		} `json:"Healthcheck"`
		ExposedPorts map[string]struct{} `json:"ExposedPorts"`
	} `json:"Config"`
	State *struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		Restarting bool   `json:"Restarting"`
		Health     struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	HostConfig *struct {
		RestartPolicy struct {
			Name string `json:"Name"`
		} `json:"RestartPolicy"`
		PortBindings map[string][]struct {
			HostPort string `json:"HostPort"`
		} `json:"PortBindings"`
		NetworkMode string `json:"NetworkMode"`
	} `json:"HostConfig"`
	NetworkSettings *struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

// InspectContainer fetches full details for one container.
func (c *Client) InspectContainer(ctx context.Context, id string) (*ContainerInspect, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/containers/"+url.PathEscape(id)+"/json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker inspect container %s: status %d", id, resp.StatusCode)
	}
	var out ContainerInspect
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ImageInspect is the subset of GET /images/{name}/json we care about.
type ImageInspect struct {
	ID       string `json:"Id"`
	RepoTags []string `json:"RepoTags"`
	Created  string `json:"Created"`
}

// InspectImage fetches details for an image by name or id.
func (c *Client) InspectImage(ctx context.Context, name string) (*ImageInspect, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/images/"+url.PathEscape(name)+"/json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker inspect image %s: status %d", name, resp.StatusCode)
	}
	var out ImageInspect
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// discardBody drains a small body for connection reuse semantics.
func discardBody(r io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(r, 4096))
	_ = r.Close()
}
