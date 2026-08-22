package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"synapse/internal/notify"
)

func TestNtfySendPayload(t *testing.T) {
	var gotPriority, gotTitle, gotTags, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPriority = r.Header.Get("Priority")
		gotTitle = r.Header.Get("Title")
		gotTags = r.Header.Get("Tags")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewNtfy(Config{Type: TypeNtfy, Enabled: true, URL: srv.URL, Priority: 4})
	if err := n.Send(context.Background(), notify.CatDockerDie, "Container died", "plex stopped"); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if gotPriority != "4" || gotTitle != "Container died" || gotTags != string(notify.CatDockerDie) {
		t.Fatalf("headers wrong: prio=%q title=%q tags=%q", gotPriority, gotTitle, gotTags)
	}
	if gotBody != "plex stopped" {
		t.Fatalf("body wrong: %q", gotBody)
	}
}

func TestNtfyDisabled(t *testing.T) {
	n := NewNtfy(Config{Type: TypeNtfy, Enabled: false, URL: "http://x"})
	if n.Enabled() {
		t.Fatal("disabled channel must not be enabled")
	}
	if err := n.Send(context.Background(), notify.CatReconcile, "t", "m"); err == nil {
		t.Fatal("send on disabled channel must error")
	}
}

func TestTelegramSendPayload(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tg := NewTelegram(Config{Type: TypeTelegram, Enabled: true, URL: srv.URL + "/12345", Token: "TOK"})
	if !tg.Enabled() {
		t.Fatal("telegram should be enabled with url+chat id")
	}
	if err := tg.Send(context.Background(), notify.CatDockerHealth, "Title here", "body there"); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if got["chat_id"] != "12345" {
		t.Fatalf("chat id wrong: %v", got["chat_id"])
	}
	if got["text"] != "Title here\nbody there" {
		t.Fatalf("text wrong: %v", got["text"])
	}
}

func TestDiscordSendPayload(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := NewDiscord(Config{Type: TypeDiscord, Enabled: true, URL: srv.URL})
	if err := d.Send(context.Background(), notify.CatDockerImage, "Image updated", "nginx 1.25 -> 1.27"); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	want := "**Image updated**\nnginx 1.25 -> 1.27"
	if got["content"] != want {
		t.Fatalf("content wrong: %v", got["content"])
	}
}

func TestWebhookSendEnvelope(t *testing.T) {
	var got map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := NewWebhook(Config{Type: TypeWebhook, Enabled: true, URL: srv.URL, Token: "s3cret"})
	if err := wh.Send(context.Background(), notify.CatReconcile, "Reconcile done", "2 changes"); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if got["category"] != string(notify.CatReconcile) || got["title"] != "Reconcile done" || got["message"] != "2 changes" {
		t.Fatalf("envelope wrong: %v", got)
	}
	if _, ok := got["timestamp"]; !ok {
		t.Fatal("timestamp missing from envelope")
	}
	if gotAuth != "Bearer s3cret" {
		t.Fatalf("auth header wrong: %q", gotAuth)
	}
}

func TestAdapterSendTimeoutBounded(t *testing.T) {
	// All adapters share the 10s cap; verify the constant to keep the spec honest.
	if sendTimeout != 10*time.Second {
		t.Fatalf("send timeout drifted: %v", sendTimeout)
	}
}
