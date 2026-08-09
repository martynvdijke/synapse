package kuma

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestGetMonitorStats_WSDialFailure(t *testing.T) {
	// Invalid URL should fail immediately, no 20s wait.
	c := NewClient("http://127.0.0.1:1")
	c.username = "admin"
	c.password = "secret"

	stats, err := GetMonitorStats(c, 1)
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
	if stats != nil {
		t.Errorf("expected nil stats on error, got %+v", stats)
	}
}

func TestGetMonitorStats_ShortURL(t *testing.T) {
	// Very short URL that causes dial to fail fast.
	c := NewClient("http://127.0.0.1:1")
	c.username = "admin"
	c.password = "secret"

	_, err := GetMonitorStats(c, 42)
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}

func TestAddMonitorViaSocketIO_DialFailure(t *testing.T) {
	_, err := AddMonitorViaSocketIO("http://127.0.0.1:1", "admin", "secret", "http", "test", "http://ex.com", "", 0)
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}

func TestAddMonitorViaSocketIO_ShortURL(t *testing.T) {
	_, err := AddMonitorViaSocketIO("http://127.0.0.1:1", "admin", "secret", "http", "test", "http://ex.com", "", 0)
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}

func TestQueryMonitorsViaSocketIO_DialFailure(t *testing.T) {
	_, err := QueryMonitorsViaSocketIO("http://127.0.0.1:1", "admin", "secret")
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}

// mockKumaServer simulates Uptime Kuma's Engine.IO v4 / Socket.IO v5
// handshake and captures the raw login packet so tests can assert the
// exact wire format the client emits.
func mockKumaServer(t *testing.T, loginResponse string) (url string, loginPacket chan string) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	loginPacket = make(chan string, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()

		// Engine.IO OPEN
		c.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"mock","upgrades":[],"pingInterval":25000,"pingTimeout":20000,"maxPayload":1000000}`))
		// Expect SIO CONNECT "40", respond with SIO CONNECT ack
		_, msg, err := c.ReadMessage()
		if err != nil {
			return
		}
		if string(msg) != "40" {
			t.Errorf("expected client to send \"40\", got %q", string(msg))
			return
		}
		c.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"mock"}`))
		// Tell client login is required
		c.WriteMessage(websocket.TextMessage, []byte(`42["loginRequired"]`))

		// Wait for the login event with ack
		_, msg, err = c.ReadMessage()
		if err != nil {
			return
		}
		loginPacket <- string(msg)

		// Respond to the ack: protocol v5 "43<id>[<data>]"
		c.WriteMessage(websocket.TextMessage, []byte(loginResponse))

		// Hold the connection open briefly so the client can process
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	// dialSIO expects an http(s):// URL and converts the scheme itself.
	return srv.URL, loginPacket
}

func TestEmitWithAckUsesProtocolV5WireFormat(t *testing.T) {
	url, loginPacket := mockKumaServer(t, `431[{"ok":true,"token":"jwt"}]`)

	cli, err := dialSIO(url)
	if err != nil {
		t.Fatalf("dialSIO failed: %v", err)
	}
	defer cli.close()

	cli.onEvent = func(ev rawEvent) {}

	ackCh := cli.emitWithAck("login", map[string]string{"username": "admin", "password": "secret"})

	raw := <-loginPacket
	// Protocol v5: id in header, event name first in JSON array.
	if !strings.HasPrefix(raw, `421["login",`) {
		t.Errorf("login packet must be v5 format 421[\"login\",...], got %q", raw)
	}
	if strings.HasPrefix(raw, `42[1,"login"`) {
		t.Errorf("login packet must NOT use v4 format 42[1,\"login\",...], got %q", raw)
	}
	// Payload must still contain credentials
	if !strings.Contains(raw, `"username":"admin"`) || !strings.Contains(raw, `"password":"secret"`) {
		t.Errorf("login packet missing credentials: %q", raw)
	}

	// Ack must be routed to the pending channel with args only (no id inside)
	select {
	case resp := <-ackCh:
		if len(resp) != 1 {
			t.Fatalf("expected 1 ack arg, got %d", len(resp))
		}
		var r struct {
			Ok bool `json:"ok"`
		}
		if err := json.Unmarshal(resp[0], &r); err != nil {
			t.Fatalf("unmarshal ack: %v", err)
		}
		if !r.Ok {
			t.Errorf("expected ok=true in ack, got %s", string(resp[0]))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ack")
	}
}

func TestHandleAckParsesHeaderID(t *testing.T) {
	cli := &sioClient{pendingAcks: make(map[int]chan []json.RawMessage)}

	// Verify an ack with a multi-digit id in the header is parsed.
	ch := make(chan []json.RawMessage, 1)
	cli.pendingAcks[42] = ch

	cli.handleAck([]byte(`42[{"ok":true}]`))

	select {
	case resp := <-ch:
		if len(resp) != 1 {
			t.Fatalf("expected 1 arg, got %d", len(resp))
		}
	case <-time.After(time.Second):
		t.Fatal("ack for id 42 was not routed")
	}
}

// TestQueryMonitorsViaSocketIO_ParsesMonitorList verifies that the
// monitorList event (the authoritative name/url/type source, arriving
// ~130ms after login) is parsed and that uptime events populate status.
// A shortened collection window keeps the test fast.
func TestQueryMonitorsViaSocketIO_ParsesMonitorList(t *testing.T) {
	orig := dataCollectionWindow
	dataCollectionWindow = 300 * time.Millisecond
	defer func() { dataCollectionWindow = orig }()

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()

		// Engine.IO OPEN
		c.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"mock","upgrades":[],"pingInterval":25000,"pingTimeout":20000,"maxPayload":1000000}`))
		_, msg, err := c.ReadMessage()
		if err != nil {
			return
		}
		if string(msg) != "40" {
			t.Errorf("expected \"40\", got %q", string(msg))
			return
		}
		c.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"mock"}`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["loginRequired"]`))

		// Wait for login packet, respond with ack
		_, msg, err = c.ReadMessage()
		if err != nil {
			return
		}
		if !strings.HasPrefix(string(msg), `421["login",`) {
			t.Errorf("expected v5 login packet, got %q", string(msg))
			return
		}
		c.WriteMessage(websocket.TextMessage, []byte(`431[{"ok":true,"token":"jwt"}]`))

		// Stream monitorList (map keyed by id string) with docker info
		c.WriteMessage(websocket.TextMessage, []byte(`42["monitorList",{"1":{"id":1,"name":"vandijke.xyz","url":"http://vandijke.xyz","type":"docker","docker_container":"vandijke","docker_host":1,"active":true},"2":{"id":2,"name":"heat","url":"http://heat.local","type":"http","active":true}}]`))
		// uptime events: "24" => up for monitor 1
		c.WriteMessage(websocket.TextMessage, []byte(`42["uptime",1,"24",0.98]`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["uptime",1,"720",0.95]`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["uptime",2,"24",0]`))

		// Hold open for the collection window so the client can process
		time.Sleep(600 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	monitors, err := QueryMonitorsViaSocketIO(srv.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(monitors) != 2 {
		t.Fatalf("expected 2 monitors, got %d: %+v", len(monitors), monitors)
	}

	// Monitor 1: name from monitorList, docker info, status up
	m1 := monitors[0]
	if m1.ID != 1 {
		t.Fatalf("expected id 1 first, got %d", m1.ID)
	}
	if m1.Name != "vandijke.xyz" {
		t.Errorf("expected name from monitorList, got %q", m1.Name)
	}
	if m1.Type != "docker" {
		t.Errorf("expected type docker, got %q", m1.Type)
	}
	if m1.URL != "http://vandijke.xyz" {
		t.Errorf("expected url, got %q", m1.URL)
	}
	if m1.DockerContainer != "vandijke" {
		t.Errorf("expected docker_container vandijke, got %q", m1.DockerContainer)
	}
	if m1.DockerHost != 1 {
		t.Errorf("expected docker_host 1, got %d", m1.DockerHost)
	}
	if m1.Status != 1 {
		t.Errorf("expected status up (1) from uptime 24h, got %d", m1.Status)
	}
	if m1.Uptime24h != 0.98 {
		t.Errorf("expected uptime24h 0.98, got %f", m1.Uptime24h)
	}

	// Monitor 2: name from monitorList, status down (uptime 24h == 0)
	m2 := monitors[1]
	if m2.Name != "heat" {
		t.Errorf("expected name heat, got %q", m2.Name)
	}
	if m2.Status != 0 {
		t.Errorf("expected status down (0) from uptime 24h, got %d", m2.Status)
	}
}

// TestClient_QueryMonitorsViaSocketIO_Cache verifies the monitor query
// result is cached and reused within the TTL, and invalidated after it.
func TestClient_QueryMonitorsViaSocketIO_Cache(t *testing.T) {
	calls := 0
	restore := SetQueryMonitorsTestHook(func(url, user, pass string) ([]KumaMonitor, error) {
		calls++
		return []KumaMonitor{{ID: 1, Name: "m1"}}, nil
	})
	defer restore()

	c := NewClient("http://kuma:3001")
	c.username = "admin"
	c.password = "secret"

	// First call populates cache
	m1, err := c.QueryMonitorsViaSocketIO()
	if err != nil {
		t.Fatalf("first query: %v", err)
	}
	if len(m1) != 1 || m1[0].Name != "m1" {
		t.Fatalf("unexpected first result: %+v", m1)
	}

	// Second call within TTL must not re-query
	m2, err := c.QueryMonitorsViaSocketIO()
	if err != nil {
		t.Fatalf("second query: %v", err)
	}
	if len(m2) != 1 || m2[0].Name != "m1" {
		t.Fatalf("unexpected second result: %+v", m2)
	}
	if calls != 1 {
		t.Fatalf("expected 1 query call (cached), got %d", calls)
	}

	// Expire the cache: results must be re-fetched
	c.mu.Lock()
	c.monCacheAt = time.Now().Add(-monitorCacheTTL - time.Second)
	c.mu.Unlock()
	if _, err := c.QueryMonitorsViaSocketIO(); err != nil {
		t.Fatalf("post-expiry query: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 query calls after expiry, got %d", calls)
	}
}

// TestClient_AddMonitorViaSocketIO_InvalidatesCache verifies a successful
// add clears the monitor query cache.
func TestClient_AddMonitorViaSocketIO_InvalidatesCache(t *testing.T) {
	restoreQuery := SetQueryMonitorsTestHook(func(url, user, pass string) ([]KumaMonitor, error) {
		return []KumaMonitor{{ID: 1, Name: "old"}}, nil
	})
	defer restoreQuery()
	restoreAdd := SetAddMonitorTestHook(func(url, user, pass string, monitorType, name, monURL, dockerContainer string, dockerHostID int) (int, error) {
		return 99, nil
	})
	defer restoreAdd()

	c := NewClient("http://kuma:3001")
	c.username = "admin"
	c.password = "secret"

	if _, err := c.QueryMonitorsViaSocketIO(); err != nil {
		t.Fatalf("initial query: %v", err)
	}
	c.mu.Lock()
	cached := c.monCache != nil
	c.mu.Unlock()
	if !cached {
		t.Fatal("expected cache to be populated after query")
	}

	if _, err := c.AddMonitorViaSocketIO("http", "new", "http://new.local", "", 0); err != nil {
		t.Fatalf("add monitor: %v", err)
	}
	c.mu.Lock()
	cached = c.monCache != nil
	c.mu.Unlock()
	if cached {
		t.Fatal("expected cache to be invalidated after successful add")
	}
}
