package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendMessage_Success(t *testing.T) {
	var received struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		Priority int    `json:"priority"`
	}
	var gotKey string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/message" {
			t.Errorf("expected path /message, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		gotKey = r.Header.Get("X-Gotify-Key")
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type, got %q", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "app-token-123", 7)
	if err := c.SendMessage(context.Background(), "Synapse: 2 items missing", "Docker services:\n- api"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if gotKey != "app-token-123" {
		t.Errorf("expected X-Gotify-Key app-token-123, got %q", gotKey)
	}
	if received.Title != "Synapse: 2 items missing" {
		t.Errorf("expected title, got %q", received.Title)
	}
	if received.Priority != 7 {
		t.Errorf("expected priority 7, got %d", received.Priority)
	}
}

func TestSendMessage_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "wrong-token", 5)
	err := c.SendMessage(context.Background(), "t", "m")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestSendMessage_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token", 5)
	if err := c.SendMessage(context.Background(), "t", "m"); err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestSendMessage_NetworkError(t *testing.T) {
	// Point at a closed server so the request fails at the transport layer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := NewClient(url, "token", 5)
	if err := c.SendMessage(context.Background(), "t", "m"); err == nil {
		t.Fatal("expected network error")
	}
}

func TestSendMessage_NotConfigured(t *testing.T) {
	c := NewClient("", "", 5)
	if err := c.SendMessage(context.Background(), "t", "m"); err == nil {
		t.Fatal("expected error when not configured")
	}
}

func TestSendMessage_PriorityClamp(t *testing.T) {
	if NewClient("u", "t", -3).priority != 0 {
		t.Error("expected priority clamped to 0")
	}
	if NewClient("u", "t", 99).priority != 10 {
		t.Error("expected priority clamped to 10")
	}
}
