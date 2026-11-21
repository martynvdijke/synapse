package kuma

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"synapse/internal/db"
)

// --- shared mock helpers ---

func testUpgrader() websocket.Upgrader {
	return websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
}

// writeOpenAndLoginRequired performs the Engine.IO/SIO handshake and tells the
// client that login is required.
func writeOpenAndLoginRequired(t *testing.T, c *websocket.Conn) bool {
	t.Helper()
	if err := c.WriteMessage(websocket.TextMessage, []byte(`0{"sid":"mock","upgrades":[],"pingInterval":25000,"pingTimeout":20000,"maxPayload":1000000}`)); err != nil {
		return false
	}
	_, msg, err := c.ReadMessage()
	if err != nil {
		return false
	}
	if string(msg) != "40" {
		t.Errorf("expected sio connect \"40\", got %q", string(msg))
		return false
	}
	if err := c.WriteMessage(websocket.TextMessage, []byte(`40{"sid":"mock"}`)); err != nil {
		return false
	}
	return c.WriteMessage(websocket.TextMessage, []byte(`42["loginRequired"]`)) == nil
}

// packetAckID extracts the v5 ack id from a client packet like "421[...]".
func packetAckID(pkt string) string {
	rest := strings.TrimPrefix(pkt, "42")
	for i, r := range rest {
		if r < '0' || r > '9' {
			return rest[:i]
		}
	}
	return rest
}

func withAckTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	orig := ackTimeout
	ackTimeout = d
	t.Cleanup(func() { ackTimeout = orig })
}

func withCollectionWindow(t *testing.T, d time.Duration) {
	t.Helper()
	orig := dataCollectionWindow
	dataCollectionWindow = d
	t.Cleanup(func() { dataCollectionWindow = orig })
}

// --- URL parsing ---

func TestParseKumaURLEdges(t *testing.T) {
	cases := []struct {
		in       string
		wantWS   string
		wantPoll string
	}{
		{"http://kuma:3001", "ws://kuma:3001/socket.io/?EIO=4&transport=websocket", "http://kuma:3001"},
		{"http://kuma:3001/", "ws://kuma:3001/socket.io/?EIO=4&transport=websocket", "http://kuma:3001"},
		{"https://kuma.example.com/kuma/", "wss://kuma.example.com/kuma/socket.io/?EIO=4&transport=websocket", "https://kuma.example.com/kuma"},
		{"ws://kuma:3001", "ws://kuma:3001/socket.io/?EIO=4&transport=websocket", "http://kuma:3001"},
		{"wss://kuma.example.com/prefix", "wss://kuma.example.com/prefix/socket.io/?EIO=4&transport=websocket", "https://kuma.example.com/prefix"},
	}
	for _, tc := range cases {
		u, err := parseKumaURL(tc.in)
		if err != nil {
			t.Errorf("parseKumaURL(%q): %v", tc.in, err)
			continue
		}
		if got := wsEndpoint(u); got != tc.wantWS {
			t.Errorf("parseKumaURL(%q) ws = %q, want %q", tc.in, got, tc.wantWS)
		}
		if got := pollingBase(u); got != tc.wantPoll {
			t.Errorf("parseKumaURL(%q) poll = %q, want %q", tc.in, got, tc.wantPoll)
		}
	}

	for _, bad := range []string{"", "   ", "http://", "not a url", "ftp://host", "kuma:3001"} {
		if _, err := parseKumaURL(bad); err == nil {
			t.Errorf("parseKumaURL(%q) should fail", bad)
		}
	}
}

// --- loginByToken ---

func TestSessionLoginByToken(t *testing.T) {
	upgrader := testUpgrader()
	var persisted atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		if !writeOpenAndLoginRequired(t, c) {
			return
		}
		_, pkt, err := c.ReadMessage()
		if err != nil {
			return
		}
		if !strings.Contains(string(pkt), `"loginByToken"`) || !strings.Contains(string(pkt), "tok-1") {
			t.Errorf("expected loginByToken with stored token, got %q", string(pkt))
			return
		}
		c.WriteMessage(websocket.TextMessage, []byte("43"+packetAckID(string(pkt))+`[{"ok":true}]`))
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	cfg := connConfig{
		URL:      srv.URL,
		Username: "admin",
		Password: "secret",
		Token:    "tok-1",
		PersistToken: func(string) error {
			persisted.Store(true)
			return nil
		},
	}
	s, err := newSIOSession(context.Background(), cfg)
	if err != nil {
		t.Fatalf("token login failed: %v", err)
	}
	s.close()
	if persisted.Load() {
		t.Error("token login must not re-persist the token")
	}
}

func TestSessionTokenLoginFallsBackToPassword(t *testing.T) {
	upgrader := testUpgrader()

	var mu sync.Mutex
	var persisted []string
	var sawPassword atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		if !writeOpenAndLoginRequired(t, c) {
			return
		}
		_, pkt, err := c.ReadMessage()
		if err != nil {
			return
		}
		if !strings.Contains(string(pkt), `"loginByToken"`) {
			t.Errorf("expected loginByToken first, got %q", string(pkt))
			return
		}
		// Reject the stale token.
		c.WriteMessage(websocket.TextMessage, []byte("43"+packetAckID(string(pkt))+`[{"ok":false,"msg":"invalid token"}]`))

		_, pkt, err = c.ReadMessage()
		if err != nil {
			return
		}
		if !strings.Contains(string(pkt), `"login"`) || strings.Contains(string(pkt), `"loginByToken"`) {
			t.Errorf("expected password login fallback, got %q", string(pkt))
			return
		}
		sawPassword.Store(true)
		c.WriteMessage(websocket.TextMessage, []byte("43"+packetAckID(string(pkt))+`[{"ok":true,"token":"fresh-token"}]`))
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	cfg := connConfig{
		URL:      srv.URL,
		Username: "admin",
		Password: "secret",
		Token:    "stale",
		PersistToken: func(token string) error {
			mu.Lock()
			persisted = append(persisted, token)
			mu.Unlock()
			return nil
		},
	}
	s, err := newSIOSession(context.Background(), cfg)
	if err != nil {
		t.Fatalf("fallback login failed: %v", err)
	}
	s.close()

	if !sawPassword.Load() {
		t.Fatal("password login was never attempted")
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, tok := range persisted {
		if tok == "fresh-token" {
			found = true
		}
	}
	if !found {
		t.Fatalf("fresh token was not persisted: %v", persisted)
	}
}

// --- status truthfulness ---

func TestQueryMonitorsStatusUnknownWithoutHeartbeatList(t *testing.T) {
	withCollectionWindow(t, 300*time.Millisecond)
	upgrader := testUpgrader()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		if !writeOpenAndLoginRequired(t, c) {
			return
		}
		_, pkt, err := c.ReadMessage()
		if err != nil {
			return
		}
		c.WriteMessage(websocket.TextMessage, []byte("43"+packetAckID(string(pkt))+`[{"ok":true,"token":"jwt"}]`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["monitorList",{"1":{"id":1,"name":"m1","url":"http://m1","type":"http","active":true}}]`))
		// No heartbeatList: status must stay unknown, not DOWN.
		c.WriteMessage(websocket.TextMessage, []byte(`42["uptime","1",24,0]`))
		time.Sleep(700 * time.Millisecond)
	}))
	defer srv.Close()

	monitors, err := QueryMonitorsViaSocketIO(srv.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(monitors) != 1 {
		t.Fatalf("expected 1 monitor, got %d", len(monitors))
	}
	m := monitors[0]
	if m.StatusKnown {
		t.Errorf("expected StatusKnown=false without heartbeatList")
	}
	if m.Status != 0 {
		t.Errorf("expected zero status when unknown, got %d", m.Status)
	}
	if m.Uptime24h != 0 {
		t.Errorf("expected uptime still parsed, got %v", m.Uptime24h)
	}
}

func TestQueryMonitorsClosesEarlyOnCompleteState(t *testing.T) {
	withCollectionWindow(t, 5*time.Second)
	upgrader := testUpgrader()

	origGrace := heartbeatSettleGrace
	heartbeatSettleGrace = 50 * time.Millisecond
	defer func() { heartbeatSettleGrace = origGrace }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		if !writeOpenAndLoginRequired(t, c) {
			return
		}
		_, pkt, err := c.ReadMessage()
		if err != nil {
			return
		}
		c.WriteMessage(websocket.TextMessage, []byte("43"+packetAckID(string(pkt))+`[{"ok":true,"token":"jwt"}]`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["monitorList",{"1":{"id":1,"name":"m1","url":"http://m1","type":"http","active":true},"2":{"id":2,"name":"m2","url":"http://m2","type":"http","active":true}}]`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["heartbeatList","1",[{"status":1}]]`))
		c.WriteMessage(websocket.TextMessage, []byte(`42["heartbeatList","2",[{"status":0}]]`))
		// Hold the connection open: completion must come from events, not a
		// fixed window.
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	start := time.Now()
	monitors, err := QueryMonitorsViaSocketIO(srv.URL, "admin", "secret")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(monitors) != 2 {
		t.Fatalf("expected 2 monitors, got %d", len(monitors))
	}
	if elapsed > 2*time.Second {
		t.Fatalf("expected event-aware early close, took %s (ceiling 5s)", elapsed)
	}
	if !monitors[0].StatusKnown || monitors[0].Status != 1 {
		t.Errorf("monitor 1 status = %d known=%v", monitors[0].Status, monitors[0].StatusKnown)
	}
	if !monitors[1].StatusKnown || monitors[1].Status != 0 {
		t.Errorf("monitor 2 status = %d known=%v", monitors[1].Status, monitors[1].StatusKnown)
	}
}

// --- write path hardening ---

func TestAddMonitorVerifiesAndDoesNotRetry(t *testing.T) {
	withAckTimeout(t, 300*time.Millisecond)
	upgrader := testUpgrader()
	var addCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		if !writeOpenAndLoginRequired(t, c) {
			return
		}
		_, pkt, err := c.ReadMessage()
		if err != nil {
			return
		}
		c.WriteMessage(websocket.TextMessage, []byte("43"+packetAckID(string(pkt))+`[{"ok":true,"token":"jwt"}]`))

		for {
			_, pkt, err := c.ReadMessage()
			if err != nil {
				return
			}
			req := string(pkt)
			switch {
			case strings.Contains(req, `"add"`):
				atomic.AddInt32(&addCount, 1)
				// Ack the create but never answer the verification read.
				c.WriteMessage(websocket.TextMessage, []byte("43"+packetAckID(req)+`[{"ok":true,"monitorID":7}]`))
			case strings.Contains(req, `"getMonitor"`):
				// Swallow: verification times out.
			}
		}
	}))
	defer srv.Close()

	id, err := AddMonitorViaSocketIO(srv.URL, "admin", "secret", "http", "svc", "http://svc", "", 0)
	if !errors.Is(err, ErrWriteUnverified) {
		t.Fatalf("expected ErrWriteUnverified after unconfirmed create, got %v", err)
	}
	if id != 7 {
		t.Errorf("expected known monitor id 7 alongside the error, got %d", id)
	}
	if got := atomic.LoadInt32(&addCount); got != 1 {
		t.Errorf("add must not be retried after an ack, got %d attempts", got)
	}
}

func TestDeleteMonitorAckTimeoutDoesNotHang(t *testing.T) {
	withAckTimeout(t, 200*time.Millisecond)
	upgrader := testUpgrader()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		if !writeOpenAndLoginRequired(t, c) {
			return
		}
		_, pkt, err := c.ReadMessage()
		if err != nil {
			return
		}
		c.WriteMessage(websocket.TextMessage, []byte("43"+packetAckID(string(pkt))+`[{"ok":true,"token":"jwt"}]`))

		stop := make(chan struct{})
		writerDone := make(chan struct{})
		// Flood events: the old command flow never drained these and deadlocked
		// close(); the new drain must keep the socket responsive.
		go func() {
			defer close(writerDone)
			for {
				select {
				case <-stop:
					return
				default:
					if err := c.WriteMessage(websocket.TextMessage, []byte(`42["heartbeat",{"monitorID":1,"status":1}]`)); err != nil {
						return
					}
					time.Sleep(2 * time.Millisecond)
				}
			}
		}()
		// Never ack deleteMonitor.
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				break
			}
		}
		close(stop)
		<-writerDone
	}))
	defer srv.Close()

	done := make(chan error, 1)
	go func() {
		done <- DeleteMonitorViaSocketIO(srv.URL, "admin", "secret", 99)
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error when delete is never acked")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("delete monitor hung (command-session deadlock regression)")
	}
}

func TestPendingAcksFailOnClose(t *testing.T) {
	upgrader := testUpgrader()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		if !writeOpenAndLoginRequired(t, c) {
			return
		}
		// Never ack anything.
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	cli, err := dialSIO(srv.URL)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	cli.setOnEvent(func(rawEvent) {})

	ch, err := cli.emitWithAck("deleteMonitor", 1)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	closed := make(chan struct{})
	go func() {
		cli.close()
		close(closed)
	}()

	select {
	case resp := <-ch:
		if resp != nil {
			t.Errorf("expected nil ack on close, got %v", resp)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pending ack leaked on close")
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("close did not return promptly")
	}
}

// --- registry snapshot ---

func TestRegistrySnapshotReportsEveryEnabledInstance(t *testing.T) {
	d := setupTestDB(t)

	restoreHook := SetVerifySocketIOLoginTestHook(func(url, user, pass string) error {
		if url == "http://bad" {
			return errors.New("login failed")
		}
		return nil
	})
	defer restoreHook()

	if _, err := d.CreateKumaInstance(&db.KumaInstance{Name: "good", URL: "http://good", Username: "u", Password: "p", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateKumaInstance(&db.KumaInstance{Name: "bad", URL: "http://bad", Username: "u", Password: "p", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateKumaInstance(&db.KumaInstance{Name: "off", URL: "http://off", Username: "u", Password: "p", Enabled: false}); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(d)
	results := r.Snapshot(context.Background())
	if len(results) != 2 {
		t.Fatalf("expected 2 rows (enabled only), got %d: %+v", len(results), results)
	}
	for _, res := range results {
		switch res.Name {
		case "good":
			if res.Err != nil {
				t.Errorf("good instance returned Err: %v", res.Err)
			}
			if res.Client == nil {
				t.Error("good instance returned nil Client with nil Err")
			}
		case "bad":
			if res.Err == nil {
				t.Error("bad instance must carry Err, not be skipped")
			}
			if res.Client != nil {
				t.Error("bad instance must not carry a Client")
			}
		default:
			t.Errorf("unexpected row %+v", res)
		}
	}
}

// --- unit: spec defaults and edit merge ---

func TestApplyMonitorDefaults(t *testing.T) {
	got := applyMonitorDefaults(MonitorSpec{})
	if got.Interval != 60 || got.RetryInterval != 60 || got.MaxRetries != 3 {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	if len(got.AcceptedStatusCodes) != 1 || got.AcceptedStatusCodes[0] != "200-299" {
		t.Fatalf("unexpected accepted codes: %v", got.AcceptedStatusCodes)
	}

	custom := applyMonitorDefaults(MonitorSpec{Interval: 30, MaxRetries: 5, AcceptedStatusCodes: []string{"200"}})
	if custom.Interval != 30 || custom.MaxRetries != 5 || custom.AcceptedStatusCodes[0] != "200" {
		t.Fatalf("caller values must win: %+v", custom)
	}
}

func TestBuildEditPayloadPreservesUnchangedFields(t *testing.T) {
	current := map[string]any{}
	current["id"] = 3
	current["name"] = "old"
	current["type"] = "http"
	current["url"] = "http://old"
	current["notificationIDList"] = map[string]any{"1": true}
	current["accepted_statuscodes"] = []any{"200-299"}
	current["interval"] = float64(60)
	payload := buildEditSpecPayload(current, 3, MonitorSpec{Name: "new"})

	if payload["name"] != "new" || payload["url"] != "http://old" {
		t.Fatalf("unexpected merge: %+v", payload)
	}
	if _, ok := payload["notificationIDList"]; !ok {
		t.Fatal("notificationIDList must be preserved (omitting it deletes notification links)")
	}
	if payload["type"] != "http" {
		t.Fatalf("type must be preserved, got %v", payload["type"])
	}
	codes, ok := payload["accepted_statuscodes"].([]string)
	if !ok || len(codes) == 0 || codes[0] != "200-299" {
		t.Fatalf("accepted_statuscodes must be a string array, got %T %v", payload["accepted_statuscodes"], payload["accepted_statuscodes"])
	}
}
