package npm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockNPMJWT creates a test server that handles both POST /api/tokens (JWT login)
// and GET /api/nginx/proxy-hosts (proxy list). The proxyHosts handler is
// provided by the caller for each test's specific data.
func mockNPMJWT(t *testing.T, proxyHosts http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tokens", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST /api/tokens, got %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode token request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if body["identity"] != "admin" || body["secret"] != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid credentials"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"token":   "jwt-test-token",
			"expires": "2030-01-01T00:00:00Z",
		})
	})
	mux.HandleFunc("/api/nginx/proxy-hosts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET /api/nginx/proxy-hosts, got %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") != "Bearer jwt-test-token" {
			t.Errorf("expected Authorization: Bearer jwt-test-token, got %q", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		proxyHosts(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestGetProxyHosts_Success(t *testing.T) {
	srv := mockNPMJWT(t, func(w http.ResponseWriter, r *http.Request) {
		resp := []ProxyHost{
			{
				ID:            1,
				DomainNames:   []string{"app.example.com"},
				ForwardHost:   "192.168.1.10",
				ForwardPort:   8080,
				ForwardScheme: "http",
				Enabled:       true,
			},
			{
				ID:            2,
				DomainNames:   []string{"api.example.com", "api.internal"},
				ForwardHost:   "192.168.1.11",
				ForwardPort:   9000,
				ForwardScheme: "http",
				Enabled:       true,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	entries, err := GetProxyHosts(srv.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	expected := []ProxyEntry{
		{CNAME: "app.example.com", Host: "192.168.1.10", Port: 8080, Protocol: "http"},
		{CNAME: "api.example.com", Host: "192.168.1.11", Port: 9000, Protocol: "http"},
		{CNAME: "api.internal", Host: "192.168.1.11", Port: 9000, Protocol: "http"},
	}

	for i, e := range expected {
		if entries[i].CNAME != e.CNAME {
			t.Errorf("entry %d CNAME: expected %q, got %q", i, e.CNAME, entries[i].CNAME)
		}
		if entries[i].Host != e.Host {
			t.Errorf("entry %d Host: expected %q, got %q", i, e.Host, entries[i].Host)
		}
		if entries[i].Port != e.Port {
			t.Errorf("entry %d Port: expected %d, got %d", i, e.Port, entries[i].Port)
		}
		if entries[i].Protocol != e.Protocol {
			t.Errorf("entry %d Protocol: expected %q, got %q", i, e.Protocol, entries[i].Protocol)
		}
	}
}

func TestGetProxyHosts_NoContainerKept(t *testing.T) {
	srv := mockNPMJWT(t, func(w http.ResponseWriter, r *http.Request) {
		// The NPM API never returns a container field — hosts without one
		// must be kept, not skipped.
		resp := []ProxyHost{
			{
				ID:            1,
				DomainNames:   []string{"app.example.com"},
				ForwardHost:   "192.168.1.10",
				ForwardPort:   8080,
				ForwardScheme: "http",
				Enabled:       true,
			},
			{
				ID:            2,
				DomainNames:   []string{"api.example.com"},
				ForwardHost:   "192.168.1.11",
				ForwardPort:   9000,
				ForwardScheme: "http",
				Enabled:       true,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	entries, err := GetProxyHosts(srv.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (no-container hosts kept), got %d", len(entries))
	}
	for _, e := range entries {
		if e.Container != "" {
			t.Errorf("expected empty container for %q, got %q", e.CNAME, e.Container)
		}
	}
}

func TestGetProxyHosts_DisabledSkipped(t *testing.T) {
	srv := mockNPMJWT(t, func(w http.ResponseWriter, r *http.Request) {
		resp := []ProxyHost{
			{
				ID:            1,
				DomainNames:   []string{"disabled.example.com"},
				ForwardHost:   "192.168.1.10",
				ForwardPort:   8080,
				ForwardScheme: "http",
				Enabled:       false,
			},
			{
				ID:            2,
				DomainNames:   []string{"enabled.example.com"},
				ForwardHost:   "192.168.1.11",
				ForwardPort:   9000,
				ForwardScheme: "https",
				Enabled:       true,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	entries, err := GetProxyHosts(srv.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (disabled skipped), got %d", len(entries))
	}
	if entries[0].CNAME != "enabled.example.com" {
		t.Errorf("expected CNAME enabled.example.com, got %q", entries[0].CNAME)
	}
	if entries[0].Protocol != "https" {
		t.Errorf("expected protocol https, got %q", entries[0].Protocol)
	}
}

func TestGetProxyHosts_NoDomainsSkips(t *testing.T) {
	srv := mockNPMJWT(t, func(w http.ResponseWriter, r *http.Request) {
		resp := []ProxyHost{
			{
				ID:            1,
				DomainNames:   []string{}, // no domains → should be skipped
				ForwardHost:   "192.168.1.10",
				ForwardPort:   8080,
				ForwardScheme: "http",
				Enabled:       true,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	entries, err := GetProxyHosts(srv.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestGetProxyHosts_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return 401 — for both /api/tokens and any other path
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := GetProxyHosts(server.URL, "admin", "wrong")
	if err == nil {
		t.Fatal("expected error for unauthorized request")
	}
}

func TestGetProxyHosts_ServerError(t *testing.T) {
	srv := mockNPMJWT(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, err := GetProxyHosts(srv.URL, "admin", "secret")
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

func TestGetProxyHosts_EmptyResponse(t *testing.T) {
	srv := mockNPMJWT(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	})

	entries, err := GetProxyHosts(srv.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty result, got %d entries", len(entries))
	}
}

func TestGetProxyHosts_InvalidJSON(t *testing.T) {
	srv := mockNPMJWT(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	})

	_, err := GetProxyHosts(srv.URL, "admin", "secret")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
