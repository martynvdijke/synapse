package npm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetProxyHosts_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/nginx/proxy-hosts" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		resp := []ProxyHost{
			{
				ID:          1,
				DomainNames: []string{"app.example.com"},
				Forwarding: ForwardingConfig{
					Host:      "192.168.1.10",
					Port:      8080,
					Protocol:  "http",
					Container: "myapp",
				},
			},
			{
				ID:          2,
				DomainNames: []string{"api.example.com", "api.internal"},
				Forwarding: ForwardingConfig{
					Host:      "192.168.1.11",
					Port:      9000,
					Protocol:  "http",
					Container: "api",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	entries, err := GetProxyHosts(server.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	expected := []ProxyEntry{
		{CNAME: "app.example.com", Container: "myapp", Host: "192.168.1.10", Port: 8080, Protocol: "http"},
		{CNAME: "api.example.com", Container: "api", Host: "192.168.1.11", Port: 9000, Protocol: "http"},
		{CNAME: "api.internal", Container: "api", Host: "192.168.1.11", Port: 9000, Protocol: "http"},
	}

	for i, e := range expected {
		if entries[i].CNAME != e.CNAME {
			t.Errorf("entry %d CNAME: expected %q, got %q", i, e.CNAME, entries[i].CNAME)
		}
		if entries[i].Container != e.Container {
			t.Errorf("entry %d Container: expected %q, got %q", i, e.Container, entries[i].Container)
		}
		if entries[i].Host != e.Host {
			t.Errorf("entry %d Host: expected %q, got %q", i, e.Host, entries[i].Host)
		}
		if entries[i].Port != e.Port {
			t.Errorf("entry %d Port: expected %d, got %d", i, e.Port, entries[i].Port)
		}
	}
}

func TestGetProxyHosts_NoContainerSkips(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := []ProxyHost{
			{
				ID:          1,
				DomainNames: []string{"orphan.example.com"},
				Forwarding: ForwardingConfig{
					Host:      "192.168.1.10",
					Port:      8080,
					Protocol:  "http",
					Container: "", // no container → should be skipped
				},
			},
			{
				ID:          2,
				DomainNames: []string{"valid.example.com"},
				Forwarding: ForwardingConfig{
					Host:      "192.168.1.11",
					Port:      9000,
					Protocol:  "http",
					Container: "myapp",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	entries, err := GetProxyHosts(server.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].CNAME != "valid.example.com" {
		t.Errorf("expected CNAME valid.example.com, got %q", entries[0].CNAME)
	}
}

func TestGetProxyHosts_NoDomainsSkips(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := []ProxyHost{
			{
				ID:          1,
				DomainNames: []string{}, // no domains → should be skipped
				Forwarding: ForwardingConfig{
					Host:      "192.168.1.10",
					Port:      8080,
					Protocol:  "http",
					Container: "myapp",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	entries, err := GetProxyHosts(server.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestGetProxyHosts_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := GetProxyHosts(server.URL, "admin", "wrong")
	if err == nil {
		t.Fatal("expected error for unauthorized request")
	}
}

func TestGetProxyHosts_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := GetProxyHosts(server.URL, "admin", "secret")
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

func TestGetProxyHosts_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	entries, err := GetProxyHosts(server.URL, "admin", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty result, got %d entries", len(entries))
	}
}

func TestGetProxyHosts_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	_, err := GetProxyHosts(server.URL, "admin", "secret")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
