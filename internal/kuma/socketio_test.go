package kuma

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// TestAddMonitorViaSocketIO_PayloadAndMonitorID reproduces the production
// failure "add monitor rejected: Cannot read properties of undefined
// (reading 'every')": Kuma's add handler unconditionally calls
// monitor.accepted_statuscodes.every(...), so the payload must always carry
// an array of strings, for every monitor type. It also verifies that the
// created monitor ID is taken from the ack's monitorID field (modern Kuma
// returns msg="successAdded" and the ID as a separate field).
func TestAddMonitorViaSocketIO_PayloadAndMonitorID(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	addPacket := make(chan string, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()

		// Engine.IO OPEN
		c.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"mock","upgrades":[],"pingInterval":25000,"pingTimeout":20000,"maxPayload":1000000}`))
		if _, _, err := c.ReadMessage(); err != nil { // SIO CONNECT "40"
			return
		}
		c.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"mock"}`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["loginRequired"]`))

		// Login event with ack; respond ok.
		if _, _, err := c.ReadMessage(); err != nil {
			return
		}
		c.WriteMessage(websocket.TextMessage, []byte(`431[{"ok":true}]`))

		// Add event: mimic Kuma's server-side validation.
		_, msg, err := c.ReadMessage()
		if err != nil {
			return
		}
		raw := string(msg)
		idx := strings.Index(raw, "[")
		if idx < 0 || !strings.HasPrefix(raw, "42") {
			t.Errorf("expected v5 event packet, got %q", raw)
			return
		}
		var args []json.RawMessage
		if err := json.Unmarshal([]byte(raw[idx:]), &args); err != nil || len(args) < 2 {
			t.Errorf("malformed add packet %q: %v", raw, err)
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(args[1], &payload); err != nil {
			t.Errorf("unmarshal add payload: %v", err)
			return
		}
		codes, ok := payload["accepted_statuscodes"].([]any)
		if !ok {
			t.Errorf("payload missing accepted_statuscodes array (Kuma would crash on .every): %s", args[1])
			c.WriteMessage(websocket.TextMessage, []byte(`432[{"ok":false,"msg":"Cannot read properties of undefined (reading 'every')"}]`))
			return
		}
		for _, code := range codes {
			if _, isStr := code.(string); !isStr {
				t.Errorf("accepted_statuscodes must all be strings (Kuma rejects ints): %s", args[1])
			}
		}
		addPacket <- raw
		c.WriteMessage(websocket.TextMessage, []byte(`432[{"ok":true,"msg":"successAdded","monitorID":42}]`))
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	id, err := AddMonitorViaSocketIO(srv.URL, "admin", "secret", "docker", "svc-a", "", "container-x", 3)
	if err != nil {
		t.Fatalf("AddMonitorViaSocketIO failed: %v", err)
	}
	if id != 42 {
		t.Errorf("expected monitorID 42 from ack field, got %d", id)
	}

	select {
	case raw := <-addPacket:
		if !strings.Contains(raw, `"docker_container":"container-x"`) {
			t.Errorf("add packet missing docker_container: %q", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for add packet")
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

		// Wait for login packet, respond with ack echoing the client's ack id
		// (v5 format: "42<ackId>[\"login\",...]" -> ack "43<ackId>[...]").
		_, msg, err = c.ReadMessage()
		if err != nil {
			return
		}
		if !strings.HasPrefix(string(msg), `421["login",`) {
			t.Errorf("expected v5 login packet, got %q", string(msg))
			return
		}
		ackID := strings.TrimPrefix(string(msg), "42")
		for i, r := range ackID {
			if r < '0' || r > '9' {
				ackID = ackID[:i]
				break
			}
		}
		c.WriteMessage(websocket.TextMessage, []byte("43"+ackID+`[{"ok":true,"token":"jwt"}]`))

		// Stream monitorList (map keyed by id string) with docker info
		c.WriteMessage(websocket.TextMessage, []byte(`42["monitorList",{"1":{"id":1,"name":"vandijke.xyz","url":"http://vandijke.xyz","type":"docker","docker_container":"vandijke","docker_host":1,"active":true},"2":{"id":2,"name":"heat","url":"http://heat.local","type":"http","active":true}}]`))
		// uptime events use the real Kuma wire format: id as string,
		// duration as JSON number, value as number.
		// "24" => up for monitor 1
		c.WriteMessage(websocket.TextMessage, []byte(`42["uptime","1",24,0.98]`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["uptime","1",720,0.95]`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["uptime","1",8760,0.93]`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["uptime","2",24,0]`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["avgPing","1",1.5]`))

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
	if m1.Uptime7d != 0.95 {
		t.Errorf("expected uptime7d 0.95, got %f", m1.Uptime7d)
	}
	if m1.Uptime1y != 0.93 {
		t.Errorf("expected uptime1y 0.93 (numeric 8760 duration), got %f", m1.Uptime1y)
	}
	if m1.Ping != 1.5 {
		t.Errorf("expected ping 1.5, got %f", m1.Ping)
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

// mockPollingKumaServer simulates an Uptime Kuma instance that only supports
// Engine.IO HTTP long-polling (no WebSocket upgrades), mirroring the reported
// production failure where nginx answers the WS upgrade with 400
// {"code":3,"message":"Bad request"}. It implements the full polling flow:
// GET handshake -> OPEN, POST "40" -> connect, GET long-poll -> buffered
// packets joined by the 0x1e record separator.
func mockPollingKumaServer(t *testing.T, loginResponse string) (url string, loginPacket chan string) {
	t.Helper()
	loginPacket = make(chan string, 1)

	var mu sync.Mutex
	var outbox []string
	wake := make(chan struct{}, 1)

	notify := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/socket.io/" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			sid := r.URL.Query().Get("sid")
			if sid == "" {
				// Engine.IO OPEN handshake
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, `0{"sid":"mock","upgrades":["websocket"],"pingInterval":25000,"pingTimeout":20000,"maxPayload":1000000}`)
				return
			}
			// Long-poll: return buffered packets or wait for a POST to wake us
			for {
				mu.Lock()
				if len(outbox) > 0 {
					pkts := strings.Join(outbox, "\x1e")
					outbox = nil
					mu.Unlock()
					w.Header().Set("Content-Type", "text/plain")
					fmt.Fprint(w, pkts)
					return
				}
				mu.Unlock()
				select {
				case <-wake:
				case <-time.After(700 * time.Millisecond):
					w.Header().Set("Content-Type", "text/plain")
					return
				}
			}
		case http.MethodPost:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "read error", http.StatusBadRequest)
				return
			}
			mu.Lock()
			switch string(body) {
			case "40":
				outbox = append(outbox, `40{"sid":"mock"}`, `42["loginRequired"]`)
			default:
				if strings.HasPrefix(string(body), `421["login",`) {
					loginPacket <- string(body)
					// echo the v5 ack id: "421<id>["login",...]" -> "43<id>[data]"
					ackID := strings.TrimPrefix(string(body), "42")
					for i, ch := range ackID {
						if ch < '0' || ch > '9' {
							ackID = ackID[:i]
							break
						}
					}
					// loginResponse is "43<id>[<data>]" — reuse its JSON args
					jsonPart := loginResponse[strings.Index(loginResponse, "["):]
					outbox = append(outbox, "43"+ackID+jsonPart)
					// stream monitor data like the real server
					outbox = append(outbox,
						`42["monitorList",{"1":{"id":1,"name":"vandijke.xyz","url":"http://vandijke.xyz","type":"http","active":true}}]`,
						`42["uptime","1",24,0.98]`,
						`42["uptime","1",720,0.95]`,
						`42["uptime","1",8760,0.93]`,
						`42["avgPing","1",1.5]`,
					)
				} else if string(body) == "2" {
					outbox = append(outbox, "3")
				}
			}
			mu.Unlock()
			notify()
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "ok")
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, loginPacket
}

// TestQueryMonitorsViaSocketIO_PollingFallback verifies the end-to-end flow
// against a server that rejects WebSocket upgrades (like the remote nginx
// fronted Kuma instances that previously failed to connect): dialSIO must
// fall back to Engine.IO HTTP long-polling and still complete login + data
// collection.
func TestQueryMonitorsViaSocketIO_PollingFallback(t *testing.T) {
	orig := dataCollectionWindow
	dataCollectionWindow = 400 * time.Millisecond
	defer func() { dataCollectionWindow = orig }()

	url, loginPacket := mockPollingKumaServer(t, `431[{"ok":true,"token":"jwt"}]`)

	monitors, err := QueryMonitorsViaSocketIO(url, "admin", "secret")
	if err != nil {
		t.Fatalf("query via polling fallback failed: %v", err)
	}
	if len(monitors) != 1 {
		t.Fatalf("expected 1 monitor, got %d: %+v", len(monitors), monitors)
	}
	m := monitors[0]
	if m.ID != 1 {
		t.Errorf("expected id 1, got %d", m.ID)
	}
	if m.Name != "vandijke.xyz" {
		t.Errorf("expected name vandijke.xyz, got %q", m.Name)
	}
	if m.Uptime24h != 0.98 || m.Uptime7d != 0.95 || m.Uptime1y != 0.93 {
		t.Errorf("unexpected uptimes: 24h=%f 7d=%f 1y=%f", m.Uptime24h, m.Uptime7d, m.Uptime1y)
	}
	if m.Ping != 1.5 {
		t.Errorf("expected ping 1.5, got %f", m.Ping)
	}
	if m.Status != 1 {
		t.Errorf("expected status up (1), got %d", m.Status)
	}

	select {
	case raw := <-loginPacket:
		if !strings.HasPrefix(raw, `421["login",`) {
			t.Errorf("login packet must use v5 wire format, got %q", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("login packet was never received over polling transport")
	}
}

func TestQueryMonitorsViaSocketIO_ParsesMonitorListFields(t *testing.T) {
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
		c.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"mock","upgrades":[],"pingInterval":25000,"pingTimeout":20000,"maxPayload":1000000}`))
		_, msg, _ := c.ReadMessage()
		if string(msg) != "40" {
			return
		}
		c.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"mock"}`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["loginRequired"]`))
		_, msg, _ = c.ReadMessage()
		ackID := strings.TrimPrefix(string(msg), "42")
		for i, ch := range ackID {
			if ch < '0' || ch > '9' {
				ackID = ackID[:i]
				break
			}
		}
		c.WriteMessage(websocket.TextMessage, []byte("43"+ackID+`[{"ok":true,"token":"jwt"}]`))
		// active:false, interval/retryInterval/maxretries, tags populated
		c.WriteMessage(websocket.TextMessage, []byte(`42["monitorList",{"10":{"id":10,"name":"paused-svc","url":"http://paused.local","type":"http","active":false,"interval":45,"retryInterval":30,"maxretries":5,"tags":[{"id":1,"name":"prod","color":"#ff0000"},{"id":2,"name":"infra","color":"#00ff00"}]}}]`))
		time.Sleep(600 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	monitors, err := QueryMonitorsViaSocketIO(srv.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(monitors) != 1 {
		t.Fatalf("expected 1 monitor, got %d", len(monitors))
	}
	m := monitors[0]
	if m.ID != 10 || m.Name != "paused-svc" {
		t.Errorf("unexpected id/name: %+v", m)
	}
	if m.Active != false {
		t.Errorf("expected active false, got %v", m.Active)
	}
	if m.Interval != 45 || m.RetryInterval != 30 || m.MaxRetries != 5 {
		t.Errorf("expected intervals 45/30/5, got %d/%d/%d", m.Interval, m.RetryInterval, m.MaxRetries)
	}
	if len(m.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d: %+v", len(m.Tags), m.Tags)
	}
	if m.Tags[0].ID != 1 || m.Tags[0].Name != "prod" {
		t.Errorf("unexpected tag 0: %+v", m.Tags[0])
	}
	if m.Tags[1].ID != 2 || m.Tags[1].Name != "infra" {
		t.Errorf("unexpected tag 1: %+v", m.Tags[1])
	}
}

func TestQueryMonitorsViaSocketIO_ParsesMonitorListDefaults(t *testing.T) {
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
		c.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"mock","upgrades":[],"pingInterval":25000,"pingTimeout":20000,"maxPayload":1000000}`))
		_, msg, _ := c.ReadMessage()
		if string(msg) != "40" {
			return
		}
		c.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"mock"}`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["loginRequired"]`))
		_, msg, _ = c.ReadMessage()
		ackID := strings.TrimPrefix(string(msg), "42")
		for i, ch := range ackID {
			if ch < '0' || ch > '9' {
				ackID = ackID[:i]
				break
			}
		}
		c.WriteMessage(websocket.TextMessage, []byte("43"+ackID+`[{"ok":true,"token":"jwt"}]`))
		// entry 1 omits active (should default true) and has empty tags
		// entry 2 has active true and tags empty array explicitly
		c.WriteMessage(websocket.TextMessage, []byte(`42["monitorList",{"1":{"id":1,"name":"no-active","url":"http://a.local","type":"http","interval":60},"2":{"id":2,"name":"with-tags","url":"http://b.local","type":"http","active":true,"tags":[]}}]`))
		time.Sleep(600 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)

	monitors, err := QueryMonitorsViaSocketIO(srv.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(monitors) != 2 {
		t.Fatalf("expected 2 monitors, got %d", len(monitors))
	}
	if !monitors[0].Active {
		t.Errorf("expected active default true for monitor without active field, got false")
	}
	if len(monitors[0].Tags) != 0 {
		t.Errorf("expected empty tags for monitor 1, got %+v", monitors[0].Tags)
	}
	if !monitors[1].Active {
		t.Errorf("expected active true for monitor 2, got false")
	}
	if monitors[1].Tags != nil && len(monitors[1].Tags) != 0 {
		t.Errorf("expected empty tags for monitor 2, got %+v", monitors[1].Tags)
	}
	// tags empty vs populated edge: first is nil/empty, second also empty
	if monitors[0].Interval != 60 {
		t.Errorf("expected interval 60, got %d", monitors[0].Interval)
	}
}
