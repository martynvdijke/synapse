package npm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"synapse/internal/db"
)

// setupTestDB mirrors the helper in internal/db/db_test.go.
func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// mockNPMServer returns an httptest server that handles POST /api/tokens (JWT login)
// and GET /api/nginx/proxy-hosts (proxy list).
func mockNPMServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tokens", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"token":   "jwt-test-token",
			"expires": "2030-01-01T00:00:00Z",
		})
	})
	mux.HandleFunc("/api/nginx/proxy-hosts", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jwt-test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]ProxyHost{
			{
				ID:            1,
				DomainNames:   []string{"example.com"},
				ForwardHost:   "10.0.0.1",
				ForwardPort:   80,
				ForwardScheme: "http",
				Enabled:       true,
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestRegistryAll(t *testing.T) {
	d := setupTestDB(t)
	srv := mockNPMServer(t)

	if _, err := d.CreateNPMInstance(&db.NPMInstance{Name: "a", URL: srv.URL, Username: "u", Password: "p", Enabled: true}); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if _, err := d.CreateNPMInstance(&db.NPMInstance{Name: "b", URL: srv.URL, Username: "u", Password: "p", Enabled: true}); err != nil {
		t.Fatalf("create b: %v", err)
	}

	r := NewRegistry(d)
	clients, err := r.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(clients) != 2 {
		t.Fatalf("expected 2 clients, got %d", len(clients))
	}

	ids := map[int]bool{}
	for _, c := range clients {
		ids[c.InstanceID] = true
		if c.Client == nil {
			t.Error("expected non-nil Client")
		}
	}
	if !ids[1] || !ids[2] {
		t.Errorf("missing instance ids in result: %+v", ids)
	}
}

func TestRegistryAllSkipsDisabled(t *testing.T) {
	d := setupTestDB(t)
	srv := mockNPMServer(t)

	if _, err := d.CreateNPMInstance(&db.NPMInstance{Name: "en", URL: srv.URL, Username: "u", Password: "p", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateNPMInstance(&db.NPMInstance{Name: "dis", URL: srv.URL, Username: "u", Password: "p", Enabled: false}); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(d)
	clients, err := r.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(clients) != 1 {
		t.Fatalf("expected 1 client (enabled only), got %d", len(clients))
	}
}

func TestRegistryAllEmpty(t *testing.T) {
	d := setupTestDB(t)
	r := NewRegistry(d)
	clients, err := r.All()
	if err != nil {
		t.Fatalf("All on empty: %v", err)
	}
	if len(clients) != 0 {
		t.Errorf("expected 0 clients, got %d", len(clients))
	}
}

func TestRegistryGet(t *testing.T) {
	d := setupTestDB(t)
	srv := mockNPMServer(t)

	inst, err := d.CreateNPMInstance(&db.NPMInstance{Name: "a", URL: srv.URL, Username: "u", Password: "p", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(d)
	c, err := r.Get(int(inst.ID))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	d := setupTestDB(t)
	r := NewRegistry(d)
	if _, err := r.Get(999); err == nil {
		t.Fatal("expected error for non-existent instance")
	}
}

func TestRegistryInvalidateNoop(t *testing.T) {
	d := setupTestDB(t)
	r := NewRegistry(d)
	// Invalidate must not panic or error on any id.
	r.Invalidate(1)
	r.Invalidate(0)
	r.Invalidate(-1)
}

func TestRegistryAllSkipsFailedLogin(t *testing.T) {
	d := setupTestDB(t)
	// Server that returns 401 on /api/tokens
	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srvBad.Close)

	srvGood := mockNPMServer(t)

	if _, err := d.CreateNPMInstance(&db.NPMInstance{Name: "bad", URL: srvBad.URL, Username: "u", Password: "p", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateNPMInstance(&db.NPMInstance{Name: "good", URL: srvGood.URL, Username: "u", Password: "p", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(d)
	clients, err := r.All()
	if err != nil {
		t.Fatalf("All should not hard-fail on one bad instance: %v", err)
	}
	if len(clients) != 1 {
		t.Fatalf("expected 1 client (good only), got %d", len(clients))
	}
}

func TestRegistryTokenCaching(t *testing.T) {
	d := setupTestDB(t)

	var tokenCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tokens", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"token":   "jwt-test-token",
			"expires": "2030-01-01T00:00:00Z",
		})
	})
	mux.HandleFunc("/api/nginx/proxy-hosts", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jwt-test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	if _, err := d.CreateNPMInstance(&db.NPMInstance{Name: "a", URL: srv.URL, Username: "u", Password: "p", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(d)

	// First call logs in
	clients, _ := r.All()
	if len(clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(clients))
	}
	if tokenCalls != 1 {
		t.Errorf("expected 1 token call, got %d", tokenCalls)
	}

	// Second call reuses cached token
	clients, _ = r.All()
	if len(clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(clients))
	}
	if tokenCalls != 1 {
		t.Errorf("expected cached (still 1 token call), got %d", tokenCalls)
	}
}
