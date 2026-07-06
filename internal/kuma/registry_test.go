package kuma

import (
	"errors"
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

func TestRegistryAllLazyLogin(t *testing.T) {
	d := setupTestDB(t)

	var loginCalls1, loginCalls2 int32

	// Override verifySocketIOLoginFn to count calls and succeed
	original := verifySocketIOLoginFn
	verifySocketIOLoginFn = func(url, user, pass string) error {
		if url == "http://srv1" {
			atomic.AddInt32(&loginCalls1, 1)
		}
		if url == "http://srv2" {
			atomic.AddInt32(&loginCalls2, 1)
		}
		return nil
	}
	defer func() { verifySocketIOLoginFn = original }()

	inst1, err := d.CreateKumaInstance(&db.KumaInstance{Name: "a", URL: "http://srv1", Username: "u", Password: "p", Enabled: true})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	inst2, err := d.CreateKumaInstance(&db.KumaInstance{Name: "b", URL: "http://srv2", Username: "u", Password: "p", Enabled: true})
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
	if atomic.LoadInt32(&loginCalls1) != 1 || atomic.LoadInt32(&loginCalls2) != 1 {
		t.Fatalf("expected 1 login each, got %d/%d", loginCalls1, loginCalls2)
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
	if atomic.LoadInt32(&loginCalls1) != 1 || atomic.LoadInt32(&loginCalls2) != 1 {
		t.Fatalf("expected cached (still 1 login each), got %d/%d", loginCalls1, loginCalls2)
	}
}

func TestRegistryAllSkipsDisabled(t *testing.T) {
	d := setupTestDB(t)

	var loginCalls int32
	original := verifySocketIOLoginFn
	verifySocketIOLoginFn = func(url, user, pass string) error {
		atomic.AddInt32(&loginCalls, 1)
		return nil
	}
	defer func() { verifySocketIOLoginFn = original }()

	if _, err := d.CreateKumaInstance(&db.KumaInstance{Name: "en", URL: "http://srv1", Username: "u", Password: "p", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateKumaInstance(&db.KumaInstance{Name: "dis", URL: "http://srv2", Username: "u", Password: "p", Enabled: false}); err != nil {
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
	if atomic.LoadInt32(&loginCalls) != 1 {
		t.Errorf("expected 1 login call (enabled only), got %d", loginCalls)
	}
}

func TestRegistryAllSkipsFailedLogin(t *testing.T) {
	d := setupTestDB(t)

	var loginCalls int32
	original := verifySocketIOLoginFn
	verifySocketIOLoginFn = func(url, user, pass string) error {
		atomic.AddInt32(&loginCalls, 1)
		if url == "http://bad" {
			return errors.New("login failed")
		}
		return nil
	}
	defer func() { verifySocketIOLoginFn = original }()

	if _, err := d.CreateKumaInstance(&db.KumaInstance{Name: "bad", URL: "http://bad", Username: "u", Password: "p", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateKumaInstance(&db.KumaInstance{Name: "good", URL: "http://good", Username: "u", Password: "p", Enabled: true}); err != nil {
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

	var loginCalls int32
	original := verifySocketIOLoginFn
	verifySocketIOLoginFn = func(url, user, pass string) error {
		atomic.AddInt32(&loginCalls, 1)
		return nil
	}
	defer func() { verifySocketIOLoginFn = original }()

	inst, err := d.CreateKumaInstance(&db.KumaInstance{Name: "a", URL: "http://srv", Username: "u", Password: "p", Enabled: true})
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
	if atomic.LoadInt32(&loginCalls) != 1 {
		t.Errorf("expected 1 login, got %d", loginCalls)
	}

	// Second Get reuses cache.
	c, _ = r.Get(int(inst.ID))
	if c == nil {
		t.Fatal("expected non-nil client on second call")
	}
	if atomic.LoadInt32(&loginCalls) != 1 {
		t.Errorf("expected cached (still 1 login), got %d", loginCalls)
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

	var loginCalls int32
	original := verifySocketIOLoginFn
	verifySocketIOLoginFn = func(url, user, pass string) error {
		atomic.AddInt32(&loginCalls, 1)
		return nil
	}
	defer func() { verifySocketIOLoginFn = original }()

	inst, err := d.CreateKumaInstance(&db.KumaInstance{Name: "a", URL: "http://srv", Username: "u", Password: "p", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	r := NewRegistry(d)
	r.Get(int(inst.ID)) // login 1
	if atomic.LoadInt32(&loginCalls) != 1 {
		t.Fatalf("expected 1 login, got %d", loginCalls)
	}
	r.Get(int(inst.ID)) // cached
	if atomic.LoadInt32(&loginCalls) != 1 {
		t.Fatalf("expected still 1 login (cached), got %d", loginCalls)
	}

	r.Invalidate(int(inst.ID))

	r.Get(int(inst.ID)) // re-login after invalidate
	if atomic.LoadInt32(&loginCalls) != 2 {
		t.Errorf("expected 2 logins after invalidate, got %d", loginCalls)
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
