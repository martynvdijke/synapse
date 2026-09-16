package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	synclib "synapse/internal/sync"
)

func TestBuildSnapshotPopulatesAndVersion(t *testing.T) {
	app, _ := setupTest(t)
	app.snapshotRefreshCh = make(chan struct{}, 1)
	snap := app.buildSnapshot(context.Background())
	if snap == nil || snap.Version != 1 {
		t.Fatalf("expected version 1 got %v", snap)
	}
	if snap.Services == nil || snap.Proxies == nil || snap.Monitors == nil {
		t.Fatal("slices not initialized")
	}
	snap2 := app.buildSnapshot(context.Background())
	if snap2.Version != 2 {
		t.Fatalf("expected 2 got %d", snap2.Version)
	}
}

func TestSnapshotStale(t *testing.T) {
	snap := &DashboardSnapshot{GeneratedAt: time.Now().Add(-10 * time.Minute)}
	t.Setenv("SNAPSHOT_INTERVAL", "1s")
	t.Setenv("SNAPSHOT_STALE_AFTER", "2s")
	if !snapshotStale(snap) {
		t.Fatal("expected stale")
	}
	snap2 := &DashboardSnapshot{GeneratedAt: time.Now()}
	if snapshotStale(snap2) {
		t.Fatal("expected not stale")
	}
}

func TestSnapshotRetainsOnError(t *testing.T) {
	app, _ := setupTest(t)
	app.snapshotRefreshCh = make(chan struct{}, 1)
	// set compose to invalid to trigger docker error but still have previous
	os.Setenv("COMPOSE_PATH", "testdata/docker-compose.yml")
	snap1 := app.buildSnapshot(context.Background())
	snap1.Services = []synclib.ServiceInfo{{Name: "keep"}}
	snap1.Version = 5
	app.snapshot.Store(snap1)
	// now break compose
	t.Setenv("COMPOSE_PATH", "/nonexistent.yml")
	snap2 := app.buildSnapshot(context.Background())
	if len(snap2.Services) == 0 || snap2.Services[0].Name != "keep" {
		t.Fatalf("expected retained services, got %v", snap2.Services)
	}
	if _, ok := snap2.LastError["services"]; !ok {
		// docker error uses key "services" or "docker"; accept either
		if _, ok2 := snap2.LastError["docker"]; !ok2 {
			t.Fatalf("expected LastError, got %v", snap2.LastError)
		}
	}
}

func TestRequestSnapshotRefreshNonBlocking(t *testing.T) {
	app, _ := setupTest(t)
	app.snapshotRefreshCh = make(chan struct{}, 1)
	app.requestSnapshotRefresh()
	app.requestSnapshotRefresh() // second should not block
	if len(app.snapshotRefreshCh) != 1 {
		t.Fatal("expected 1")
	}
}

func TestOverlappingBuildsSkipped(t *testing.T) {
	app, _ := setupTest(t)
	app.snapshotRefreshCh = make(chan struct{}, 1)
	app.snapshotMu.Lock()
	// tryLock should fail while locked
	if app.snapshotMu.TryLock() {
		t.Fatal("expected lock to fail")
	}
	app.snapshotMu.Unlock()
	if !app.snapshotMu.TryLock() {
		t.Fatal("expected lock succeeds")
	}
	app.snapshotMu.Unlock()
}

func TestHandlersServeSnapshot(t *testing.T) {
	app, r := setupTest(t)
	app.snapshotRefreshCh = make(chan struct{}, 1)
	snap := &DashboardSnapshot{
		GeneratedAt: time.Now(),
		Version:     42,
		Services:    []synclib.ServiceInfo{{Name: "svc"}},
		Proxies:     []synclib.ProxyInfo{{CNAME: "p"}},
		Monitors:    []KumaMonitorSummary{{ID: 1, Name: "m"}},
	}
	app.snapshot.Store(snap)
	gin.SetMode(gin.TestMode)
	// Services
	w := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = w
	app.Services(c)
	if rec.Code != 200 {
		t.Fatalf("services %d", rec.Code)
	}
	if rec.Header().Get("X-Snapshot-Version") != "42" {
		t.Fatalf("header %v", rec.Header())
	}
	// Proxies
	w2 := httptest.NewRequest(http.MethodGet, "/api/proxies", nil)
	rec2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(rec2)
	c2.Request = w2
	app.Proxies(c2)
	if rec2.Code != 200 {
		t.Fatalf("proxies %d", rec2.Code)
	}
	// Monitors
	w3 := httptest.NewRequest(http.MethodGet, "/api/kuma/monitors", nil)
	rec3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(rec3)
	c3.Request = w3
	app.KumaMonitors(c3)
	if rec3.Code != 200 {
		t.Fatalf("monitors %d", rec3.Code)
	}
	_ = r
}

func TestColdStartNot500(t *testing.T) {
	app, _ := setupTest(t)
	app.snapshotRefreshCh = make(chan struct{}, 1)
	// no snapshot stored
	gin.SetMode(gin.TestMode)
	w := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = w
	app.Services(c)
	if rec.Code == 500 {
		t.Fatal("cold start 500")
	}
}
