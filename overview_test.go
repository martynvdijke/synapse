package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestOverviewPage_Public(t *testing.T) {
	_, r := setupTest(t)
	req := httptest.NewRequest("GET", "/overview", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// Should not redirect
	if loc := w.Header().Get("Location"); loc != "" {
		t.Errorf("expected no redirect, got Location: %s", loc)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type text/html, got %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Synapse") {
		t.Error("body should contain Synapse")
	}
	if !strings.Contains(body, "Board") {
		t.Error("body should contain Board")
	}
	// version badge contains v + version
	if !strings.Contains(body, "v") {
		t.Error("body should contain version v")
	}
	if !strings.Contains(body, version) {
		t.Errorf("body should contain version %q", version)
	}
}

func TestBoardAlias_Public(t *testing.T) {
	_, r := setupTest(t)
	req := httptest.NewRequest("GET", "/board", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html, got %q", ct)
	}
}

func TestPublicOverview_Unauthenticated(t *testing.T) {
	_, r := setupTest(t)
	req := httptest.NewRequest("GET", "/api/public/overview", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"version", "generated", "services", "proxies", "monitors", "groups", "stats"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
	// services slice not nil (should be [] not null)
	var services []map[string]any
	if err := json.Unmarshal(resp["services"], &services); err != nil {
		t.Fatalf("unmarshal services: %v", err)
	}
	if services == nil {
		t.Error("services should not be nil (expect [] not null)")
	}
	var stats map[string]any
	if err := json.Unmarshal(resp["stats"], &stats); err != nil {
		t.Fatalf("unmarshal stats: %v", err)
	}
	if v, ok := stats["total_services"].(float64); !ok || v < 1 {
		t.Errorf("stats.total_services should be >=1, got %v", stats["total_services"])
	}
	// check that total_services matches testdata (6)
	if v, ok := stats["total_services"].(float64); ok && v < 6 {
		t.Errorf("expected at least 6 services from testdata, got %v", v)
	}
	// services[].domains is never nil, group non-empty
	for _, s := range services {
		// domains field must be present and not null
		raw, _ := json.Marshal(s)
		var typed struct {
			Domains []string `json:"domains"`
			Group   string   `json:"group"`
		}
		if err := json.Unmarshal(raw, &typed); err != nil {
			t.Fatalf("unmarshal service: %v", err)
		}
		if typed.Domains == nil {
			t.Errorf("service %v domains is nil, expected []", s["name"])
		}
		if strings.TrimSpace(typed.Group) == "" {
			t.Errorf("service %v group should be non-empty", s["name"])
		}
	}
	// raw JSON should contain "domains":[] not "domains":null for at least one service with empty domains
	// We already checked nil above; also ensure raw contains not null for domains
	rawBody := string(resp["services"])
	if strings.Contains(rawBody, `"domains":null`) {
		t.Error("services domains should be [] not null")
	}
}

func TestPublicOverview_DoesNotRequireAuth(t *testing.T) {
	_, r := setupTest(t)
	// public endpoint without auth should succeed
	req := httptest.NewRequest("GET", "/api/public/overview", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("public overview without auth: expected 200, got %d", w.Code)
	}
	// private endpoint without auth should 401
	req2 := httptest.NewRequest("GET", "/api/services", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("private /api/services without auth: expected 401, got %d", w2.Code)
	}
}

func TestPublicOverview_GroupsAndDomains(t *testing.T) {
	_, r := setupTest(t)
	dir := t.TempDir()
	composePath := dir + "/docker-compose.yml"
	content := `version: "3"
services:
  myapp:
    image: myapp:latest
    container_name: myapp
    labels:
      synapse.group: Media
      synapse.domains: myapp.example.com
      synapse.icon: "🎬"
`
	if err := os.WriteFile(composePath, []byte(content), 0644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	t.Setenv("COMPOSE_PATH", composePath)

	req := httptest.NewRequest("GET", "/api/public/overview", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Services []struct {
			Name       string   `json:"name"`
			Group      string   `json:"group"`
			Domains    []string `json:"domains"`
			Icon       string   `json:"icon"`
			PrimaryURL string   `json:"primary_url"`
		} `json:"services"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found *struct {
		Name       string   `json:"name"`
		Group      string   `json:"group"`
		Domains    []string `json:"domains"`
		Icon       string   `json:"icon"`
		PrimaryURL string   `json:"primary_url"`
	}
	for i := range resp.Services {
		if resp.Services[i].Name == "myapp" {
			found = &resp.Services[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("myapp not found in services: %+v", resp.Services)
	}
	if found.Group != "Media" {
		t.Errorf("group: expected Media, got %q", found.Group)
	}
	if len(found.Domains) != 1 || found.Domains[0] != "myapp.example.com" {
		t.Errorf("domains: expected [myapp.example.com], got %v", found.Domains)
	}
	if found.Icon != "🎬" {
		t.Errorf("icon: expected 🎬, got %q", found.Icon)
	}
	if found.PrimaryURL != "https://myapp.example.com" {
		t.Errorf("primary_url: expected https://myapp.example.com, got %q", found.PrimaryURL)
	}
}

func TestPublicOverview_NoSecretsLeaked(t *testing.T) {
	_, r := setupTest(t)
	// set secrets via env to ensure they don't leak (though PublicOverview shouldn't expose settings)
	t.Setenv("NPM_PASS", "super-secret-npm-pass")
	t.Setenv("KUMA_PASS", "kuma-pass-123")
	t.Setenv("GOTIFY_TOKEN", "gotify-secret-token")

	req := httptest.NewRequest("GET", "/api/public/overview", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := strings.ToLower(w.Body.String())
	for _, needle := range []string{"password", "secret", "token", "npm_pass", "kuma_pass"} {
		if strings.Contains(body, needle) {
			t.Errorf("response should not contain %q (case-insensitive), body: %s", needle, w.Body.String())
		}
	}
}
