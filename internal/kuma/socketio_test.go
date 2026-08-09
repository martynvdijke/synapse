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
