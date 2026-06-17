package kuma

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

// mockKumaServer returns an httptest server that handles /api/login and
// /api/monitors. The loginCall counter is incremented atomically on each
// login request.
func mockKumaServer(t *testing.T, loginCalls *int32, loginOK bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if loginCalls != nil {
			atomic.AddInt32(loginCalls, 1)
		}
		if !loginOK {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResult{Token: "test-token"})
	})
	mux.HandleFunc("/api/monitors", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string][]Monitor{"monitors": {}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestRegistryAllLazyLogin(t *testing.T) {
	d := setupTestDB(t)

	var logins1, logins2 int32
	srv1 := mockKumaServer(t, &logins1, true)
	srv2 := mockKumaServer(t, &logins2, true)

	inst1, err := d.CreateKumaInstance(&db.KumaInstance{Name: "a", URL: srv1.URL, Username: "u", Password: "p", Enabled: true})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	inst2, err := d.CreateKumaInstance(&db.KumaInstance{Name: "b", URL: srv2.URL, Username: "u", Password: "p", Enabled: true})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}

	r := NewRegistry(d)

	// First All() logs in to both.
	clients, err := r.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(clients) != 2 {
		t.Fatalf("expected 2 clients, got %d", len(clients))
	}
	if atomic.LoadInt32(&logins1) != 1 || atomic.LoadInt32(&logins2) != 1 {
		t.Fatalf("expected 1 login each, got %d/%d", logins1, logins2)
	}

	// Verify instance ids are present.
	ids := map[int]bool{}
	for _, c := range clients {
		ids[c.InstanceID] = true
	}
	if !ids[int(inst1.ID)] || !ids[int(inst2.ID)] {
		t.Errorf("missing instance ids in result: %+v", ids)
	}

	// Second All() reuses cached clients — no new logins.
	clients, _ = r.All()
	if len(clients) != 2 {
		t.Fatalf("expected 2 clients on second call, got %d", len(clients))
	}
	if atomic.LoadInt32(&logins1) != 1 || atomic.LoadInt32(&logins2) != 1 {
		t.Fatalf("expected cached (still 1 login each), got %d/%d", logins1, logins2)
	}
}

func TestRegistryAllSkipsDisabled(t *testing.T) {
	d := setupTestDB(t)

	var loginsEnabled, loginsDisabled int32
	srvEn := mockKumaServer(t, &loginsEnabled, true)
	srvDis := mockKumaServer(t, &loginsDisabled, true)

	if _, err := d.CreateKumaInstance(&db.KumaInstance{Name: "en", URL: srvEn.URL, Username: "u", Password: "p", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateKumaInstance(&db.KumaInstance{Name: "dis", URL: srvDis.URL, Username: "u", Password: "p", Enabled: false}); err != nil {
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
	if atomic.LoadInt32(&loginsDisabled) != 0 {
		t.Errorf("disabled instance should not be logged into, got %d calls", loginsDisabled)
	}
	if atomic.LoadInt32(&loginsEnabled) != 1 {
		t.Errorf("enabled instance should be logged into once, got %d calls", loginsEnabled)
	}
}

func TestRegistryAllSkipsFailedLogin(t *testing.T) {
	d := setupTestDB(t)

	var loginsBad, loginsGood int32
	srvBad := mockKumaServer(t, &loginsBad, false) // 401 on login
	srvGood := mockKumaServer(t, &loginsGood, true)

	if _, err := d.CreateKumaInstance(&db.KumaInstance{Name: "bad", URL: srvBad.URL, Username: "u", Password: "p", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateKumaInstance(&db.KumaInstance{Name: "good", URL: srvGood.URL, Username: "u", Password: "p", Enabled: true}); err != nil {
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

func TestRegistryGet(t *testing.T) {
	d := setupTestDB(t)

	var logins int32
	srv := mockKumaServer(t, &logins, true)

	inst, err := d.CreateKumaInstance(&db.KumaInstance{Name: "a", URL: srv.URL, Username: "u", Password: "p", Enabled: true})
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
	if atomic.LoadInt32(&logins) != 1 {
		t.Errorf("expected 1 login, got %d", logins)
	}

	// Second Get reuses cache.
	c, _ = r.Get(int(inst.ID))
	if c == nil {
		t.Fatal("expected non-nil client on second call")
	}
	if atomic.LoadInt32(&logins) != 1 {
		t.Errorf("expected cached (still 1 login), got %d", logins)
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	d := setupTestDB(t)
	r := NewRegistry(d)
	if _, err := r.Get(999); err == nil {
		t.Fatal("expected error for non-existent instance")
	}
}

func TestRegistryInvalidate(t *testing.T) {
	d := setupTestDB(t)

	var logins int32
	srv := mockKumaServer(t, &logins, true)

	inst, err := d.CreateKumaInstance(&db.KumaInstance{Name: "a", URL: srv.URL, Username: "u", Password: "p", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(d)
	r.Get(int(inst.ID)) // login 1
	if atomic.LoadInt32(&logins) != 1 {
		t.Fatalf("expected 1 login, got %d", logins)
	}
	r.Get(int(inst.ID)) // cached
	if atomic.LoadInt32(&logins) != 1 {
		t.Fatalf("expected still 1 login (cached), got %d", logins)
	}

	r.Invalidate(int(inst.ID))

	r.Get(int(inst.ID)) // re-login after invalidate
	if atomic.LoadInt32(&logins) != 2 {
		t.Errorf("expected 2 logins after invalidate, got %d", logins)
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
