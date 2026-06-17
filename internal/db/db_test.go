package db

import (
	"os"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	path := t.TempDir() + "/test.db"
	database, err := Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestAutheliaAlertCRUD(t *testing.T) {
	db := setupTestDB(t)

	// Create alert
	alert := &AutheliaAlert{
		CNAME:     "unprotected.example.com",
		Message:   "CNAME not found in Authelia access_control rules",
		Severity:  "warning",
		CreatedAt: time.Now(),
	}
	if err := db.AddAutheliaAlert(alert); err != nil {
		t.Fatalf("add alert: %v", err)
	}

	// Get all alerts
	alerts, err := db.GetAutheliaAlerts()
	if err != nil {
		t.Fatalf("get alerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].CNAME != "unprotected.example.com" {
		t.Errorf("expected cname=unprotected.example.com, got %q", alerts[0].CNAME)
	}
	if alerts[0].Status != "open" {
		t.Errorf("expected status=open, got %q", alerts[0].Status)
	}

	// Get open alerts
	openAlerts, err := db.GetOpenAutheliaAlerts()
	if err != nil {
		t.Fatalf("get open alerts: %v", err)
	}
	if len(openAlerts) != 1 {
		t.Fatalf("expected 1 open alert, got %d", len(openAlerts))
	}

	// Resolve alert
	if err := db.ResolveAutheliaAlert(alerts[0].ID); err != nil {
		t.Fatalf("resolve alert: %v", err)
	}

	// Verify it's resolved
	openAlerts, err = db.GetOpenAutheliaAlerts()
	if err != nil {
		t.Fatalf("get open alerts after resolve: %v", err)
	}
	if len(openAlerts) != 0 {
		t.Errorf("expected 0 open alerts after resolve, got %d", len(openAlerts))
	}
}

func TestTempAccessCRUD(t *testing.T) {
	db := setupTestDB(t)

	// Create temp access rule
	rule := &TempAccess{
		IP:        "192.168.1.100",
		Reason:    "Temporary developer access",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}
	if err := db.AddTempAccess(rule); err != nil {
		t.Fatalf("add temp access: %v", err)
	}

	// Get all rules
	rules, err := db.GetTempAccessRules()
	if err != nil {
		t.Fatalf("get temp access: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].IP != "192.168.1.100" {
		t.Errorf("expected ip=192.168.1.100, got %q", rules[0].IP)
	}
	if rules[0].Status != "active" {
		t.Errorf("expected status=active, got %q", rules[0].Status)
	}

	// Get active rules
	active, err := db.GetActiveTempAccess()
	if err != nil {
		t.Fatalf("get active temp access: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active rule, got %d", len(active))
	}

	// Revoke rule
	if err := db.RevokeTempAccess(rules[0].ID); err != nil {
		t.Fatalf("revoke temp access: %v", err)
	}

	active, err = db.GetActiveTempAccess()
	if err != nil {
		t.Fatalf("get active after revoke: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("expected 0 active after revoke, got %d", len(active))
	}
}

func TestCleanupExpiredTempAccess(t *testing.T) {
	db := setupTestDB(t)

	// Create expired rule
	rule := &TempAccess{
		IP:        "10.0.0.1",
		Reason:    "Already expired",
		ExpiresAt: time.Now().Add(-1 * time.Hour), // expired 1 hour ago
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	if err := db.AddTempAccess(rule); err != nil {
		t.Fatalf("add expired rule: %v", err)
	}

	// Create active rule
	activeRule := &TempAccess{
		IP:        "10.0.0.2",
		Reason:    "Still valid",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}
	if err := db.AddTempAccess(activeRule); err != nil {
		t.Fatalf("add active rule: %v", err)
	}

	// Run cleanup
	if err := db.CleanupExpiredTempAccess(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	// Only the active rule should remain
	active, err := db.GetActiveTempAccess()
	if err != nil {
		t.Fatalf("get active after cleanup: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active rule, got %d", len(active))
	}
	if active[0].IP != "10.0.0.2" {
		t.Errorf("expected remaining rule for 10.0.0.2, got %q", active[0].IP)
	}
}

func TestAutheliaSettings(t *testing.T) {
	db := setupTestDB(t)

	defaults := Settings{
		AutheliaConfigPath:    "/config/authelia/configuration.yml",
		AutheliaDBPath:        "/config/authelia/db.sqlite3",
		AutheliaSyncEnabled:   true,
		AutheliaDefaultPolicy: "one_factor",
		AutheliaSyncOverrides: `{"public.example.com":"bypass"}`,
	}

	if err := db.SaveSettings(defaults); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	loaded := db.GetSettings(Settings{})
	if loaded.AutheliaConfigPath != defaults.AutheliaConfigPath {
		t.Errorf("config path: expected %q, got %q", defaults.AutheliaConfigPath, loaded.AutheliaConfigPath)
	}
	if loaded.AutheliaDBPath != defaults.AutheliaDBPath {
		t.Errorf("db path: expected %q, got %q", defaults.AutheliaDBPath, loaded.AutheliaDBPath)
	}
	if loaded.AutheliaSyncEnabled != defaults.AutheliaSyncEnabled {
		t.Errorf("sync enabled: expected %v, got %v", defaults.AutheliaSyncEnabled, loaded.AutheliaSyncEnabled)
	}
	if loaded.AutheliaDefaultPolicy != defaults.AutheliaDefaultPolicy {
		t.Errorf("default policy: expected %q, got %q", defaults.AutheliaDefaultPolicy, loaded.AutheliaDefaultPolicy)
	}
	if loaded.AutheliaSyncOverrides != defaults.AutheliaSyncOverrides {
		t.Errorf("overrides: expected %q, got %q", defaults.AutheliaSyncOverrides, loaded.AutheliaSyncOverrides)
	}
}

func TestAutheliaSettingsRoundTrip(t *testing.T) {
	db := setupTestDB(t)

	// Save with authelia settings
	s := Settings{
		ComposePath:           "/data/docker-compose.yml",
		AutheliaConfigPath:    "/data/authelia/config.yml",
		AutheliaSyncEnabled:   true,
		AutheliaDefaultPolicy: "two_factor",
	}
	if err := db.SaveSettings(s); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Load with defaults
	loaded := db.GetSettings(Settings{})
	if loaded.ComposePath != s.ComposePath {
		t.Errorf("compose_path: expected %q, got %q", s.ComposePath, loaded.ComposePath)
	}
	if loaded.AutheliaConfigPath != s.AutheliaConfigPath {
		t.Errorf("authelia_config_path: expected %q, got %q", s.AutheliaConfigPath, loaded.AutheliaConfigPath)
	}
	if loaded.AutheliaSyncEnabled != s.AutheliaSyncEnabled {
		t.Errorf("authelia_sync_enabled: expected %v, got %v", s.AutheliaSyncEnabled, loaded.AutheliaSyncEnabled)
	}
	if loaded.AutheliaDefaultPolicy != s.AutheliaDefaultPolicy {
		t.Errorf("authelia_default_policy: expected %q, got %q", s.AutheliaDefaultPolicy, loaded.AutheliaDefaultPolicy)
	}

	// Verify old settings keys still work
	if loaded.ComposePath != "/data/docker-compose.yml" {
		t.Errorf("compose_path not preserved")
	}
}

func TestSaveSettingsMap(t *testing.T) {
	db := setupTestDB(t)

	err := db.SaveSettingsMap(map[string]string{
		"compose_path": "/custom/path.yml",
		"kuma_url":     "http://kuma:3001",
	})
	if err != nil {
		t.Fatalf("save settings map: %v", err)
	}

	loaded := db.GetSettings(Settings{})
	if loaded.ComposePath != "/custom/path.yml" {
		t.Errorf("compose_path: expected %q, got %q", "/custom/path.yml", loaded.ComposePath)
	}
	if loaded.KumaURL != "http://kuma:3001" {
		t.Errorf("kuma_url: expected %q, got %q", "http://kuma:3001", loaded.KumaURL)
	}
}

func TestSaveSettingsMapOverwrite(t *testing.T) {
	db := setupTestDB(t)

	err := db.SaveSettingsMap(map[string]string{"compose_path": "/first.yml"})
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	loaded := db.GetSettings(Settings{})
	if loaded.ComposePath != "/first.yml" {
		t.Fatalf("first: expected %q, got %q", "/first.yml", loaded.ComposePath)
	}

	// Overwrite with different value
	err = db.SaveSettingsMap(map[string]string{"compose_path": "/second.yml"})
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	loaded = db.GetSettings(Settings{})
	if loaded.ComposePath != "/second.yml" {
		t.Errorf("compose_path: expected %q, got %q", "/second.yml", loaded.ComposePath)
	}
}

func TestSaveSettingsMapDoesNotAffectUnsentFields(t *testing.T) {
	db := setupTestDB(t)

	// Save initial compose_path
	if err := db.SaveSettingsMap(map[string]string{"compose_path": "/initial.yml"}); err != nil {
		t.Fatalf("save compose_path: %v", err)
	}

	// Now save only kuma_url — compose_path should be unaffected
	if err := db.SaveSettingsMap(map[string]string{"kuma_url": "http://kuma:3001"}); err != nil {
		t.Fatalf("save kuma_url: %v", err)
	}

	loaded := db.GetSettings(Settings{})
	if loaded.ComposePath != "/initial.yml" {
		t.Errorf("compose_path should not be affected: expected %q, got %q", "/initial.yml", loaded.ComposePath)
	}
	if loaded.KumaURL != "http://kuma:3001" {
		t.Errorf("kuma_url: expected %q, got %q", "http://kuma:3001", loaded.KumaURL)
	}
}

func TestSaveSettingsMapBoolRoundTrip(t *testing.T) {
	db := setupTestDB(t)

	if err := db.SaveSettingsMap(map[string]string{
		"authelia_sync_enabled": "true",
		"otel_enabled":          "true",
	}); err != nil {
		t.Fatalf("save bools: %v", err)
	}

	loaded := db.GetSettings(Settings{})
	if !loaded.AutheliaSyncEnabled {
		t.Error("authelia_sync_enabled expected true")
	}
	if !loaded.OTelEnabled {
		t.Error("otel_enabled expected true")
	}

	// Flip to false
	if err := db.SaveSettingsMap(map[string]string{
		"authelia_sync_enabled": "false",
		"otel_enabled":          "false",
	}); err != nil {
		t.Fatalf("save bools false: %v", err)
	}

	loaded = db.GetSettings(Settings{})
	if loaded.AutheliaSyncEnabled {
		t.Error("authelia_sync_enabled expected false")
	}
	if loaded.OTelEnabled {
		t.Error("otel_enabled expected false")
	}
}

func TestKumaInstanceCRUD(t *testing.T) {
	db := setupTestDB(t)

	// Create two instances: one enabled, one disabled.
	prod, err := db.CreateKumaInstance(&KumaInstance{
		Name: "prod", URL: "http://kuma-prod:3001",
		Username: "admin", Password: "secret", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create prod: %v", err)
	}
	if prod.ID <= 0 {
		t.Fatalf("expected positive id, got %d", prod.ID)
	}
	if prod.Name != "prod" || prod.URL != "http://kuma-prod:3001" || prod.Password != "secret" || !prod.Enabled {
		t.Errorf("prod fields mismatch: %+v", prod)
	}

	staging, err := db.CreateKumaInstance(&KumaInstance{
		Name: "staging", URL: "http://kuma-staging:3001",
		Username: "admin", Password: "stgpass", Enabled: false,
	})
	if err != nil {
		t.Fatalf("create staging: %v", err)
	}

	all, err := db.GetKumaInstances()
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(all))
	}

	enabled, err := db.GetEnabledKumaInstances()
	if err != nil {
		t.Fatalf("get enabled: %v", err)
	}
	if len(enabled) != 1 || enabled[0].Name != "prod" {
		t.Fatalf("expected 1 enabled (prod), got %+v", enabled)
	}

	got, err := db.GetKumaInstance(prod.ID)
	if err != nil {
		t.Fatalf("get prod: %v", err)
	}
	if got.Password != "secret" {
		t.Errorf("expected password secret, got %q", got.Password)
	}

	// Update with empty password must keep existing password.
	if err := db.UpdateKumaInstance(prod.ID, &KumaInstance{
		Name: "prod2", URL: "http://new", Username: "user2", Password: "", Enabled: true,
	}); err != nil {
		t.Fatalf("update keep pass: %v", err)
	}
	got, err = db.GetKumaInstance(prod.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.Name != "prod2" || got.URL != "http://new" || got.Username != "user2" {
		t.Errorf("fields not updated: %+v", got)
	}
	if got.Password != "secret" {
		t.Errorf("password should be preserved, got %q", got.Password)
	}

	// Update with a new password.
	if err := db.UpdateKumaInstance(prod.ID, &KumaInstance{
		Name: "prod3", URL: "http://new", Username: "u", Password: "newpass", Enabled: true,
	}); err != nil {
		t.Fatalf("update new pass: %v", err)
	}
	got, _ = db.GetKumaInstance(prod.ID)
	if got.Password != "newpass" {
		t.Errorf("password should be newpass, got %q", got.Password)
	}

	// Delete staging.
	if err := db.DeleteKumaInstance(staging.ID); err != nil {
		t.Fatalf("delete staging: %v", err)
	}
	all, _ = db.GetKumaInstances()
	if len(all) != 1 || all[0].Name != "prod3" {
		t.Fatalf("expected 1 instance (prod3) after delete, got %+v", all)
	}
}

func TestDeleteKumaInstanceCascadesMonitors(t *testing.T) {
	db := setupTestDB(t)

	inst, err := db.CreateKumaInstance(&KumaInstance{
		Name: "main", URL: "http://kuma:3001", Username: "admin", Password: "p", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Two monitors belonging to the instance, one orphan with a different instance id.
	if err := db.AddMonitor(&Monitor{Name: "svc-a", ServiceName: "a", MonitorType: "http", KumaInstanceID: int(inst.ID), CreatedAt: time.Now()}); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := db.AddMonitor(&Monitor{Name: "svc-b", ServiceName: "b", MonitorType: "docker", KumaInstanceID: int(inst.ID), CreatedAt: time.Now()}); err != nil {
		t.Fatalf("add b: %v", err)
	}
	if err := db.AddMonitor(&Monitor{Name: "orphan", ServiceName: "o", MonitorType: "http", KumaInstanceID: 999, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("add orphan: %v", err)
	}

	mons, _ := db.GetMonitors()
	if len(mons) != 3 {
		t.Fatalf("expected 3 monitors, got %d", len(mons))
	}

	if err := db.DeleteKumaInstance(inst.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	mons, _ = db.GetMonitors()
	if len(mons) != 1 {
		t.Fatalf("expected 1 monitor (orphan) after cascade delete, got %d", len(mons))
	}
	if mons[0].Name != "orphan" {
		t.Errorf("expected orphan to remain, got %q", mons[0].Name)
	}
}

func TestMonitorUniqueConstraintWithInstanceID(t *testing.T) {
	db := setupTestDB(t)

	a, _ := db.CreateKumaInstance(&KumaInstance{Name: "a", URL: "http://a", Username: "u", Password: "p", Enabled: true})
	b, _ := db.CreateKumaInstance(&KumaInstance{Name: "b", URL: "http://b", Username: "u", Password: "p", Enabled: true})

	// Same name/type on instance A — first succeeds.
	if err := db.AddMonitor(&Monitor{Name: "svc", ServiceName: "svc", MonitorType: "http", KumaInstanceID: int(a.ID), CreatedAt: time.Now()}); err != nil {
		t.Fatalf("add svc/a: %v", err)
	}
	// Duplicate on same instance A — silently dropped (constraint error returns nil).
	if err := db.AddMonitor(&Monitor{Name: "svc", ServiceName: "svc", MonitorType: "http", KumaInstanceID: int(a.ID), CreatedAt: time.Now()}); err != nil {
		t.Fatalf("dup add should not return error: %v", err)
	}
	// Same name/type on instance B — allowed (different instance).
	if err := db.AddMonitor(&Monitor{Name: "svc", ServiceName: "svc", MonitorType: "http", KumaInstanceID: int(b.ID), CreatedAt: time.Now()}); err != nil {
		t.Fatalf("add svc/b: %v", err)
	}

	mons, _ := db.GetMonitors()
	if len(mons) != 2 {
		t.Fatalf("expected 2 monitors (one per instance), got %d", len(mons))
	}
}

func TestMigrateKumaInstances(t *testing.T) {
	db := setupTestDB(t)

	// Add an orphan monitor (kuma_instance_id=0) before migration.
	if err := db.AddMonitor(&Monitor{Name: "orphan", ServiceName: "o", MonitorType: "http", KumaInstanceID: 0, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("add orphan: %v", err)
	}

	// Migrate from legacy settings.
	if err := db.MigrateKumaInstances(Settings{KumaURL: "http://kuma:3001", KumaUser: "admin", KumaPass: "secret"}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	insts, _ := db.GetKumaInstances()
	if len(insts) != 1 {
		t.Fatalf("expected 1 instance after migrate, got %d", len(insts))
	}
	if insts[0].Name != "default" || insts[0].URL != "http://kuma:3001" || insts[0].Username != "admin" || insts[0].Password != "secret" || !insts[0].Enabled {
		t.Errorf("default instance fields mismatch: %+v", insts[0])
	}

	// Orphan monitor should be backfilled to the default instance id.
	mons, _ := db.GetMonitors()
	if len(mons) != 1 {
		t.Fatalf("expected 1 monitor, got %d", len(mons))
	}
	if mons[0].KumaInstanceID != int(insts[0].ID) {
		t.Errorf("orphan not backfilled: expected instance id %d, got %d", insts[0].ID, mons[0].KumaInstanceID)
	}

	// Idempotent: second migrate must not create another instance.
	if err := db.MigrateKumaInstances(Settings{KumaURL: "http://other:3001", KumaUser: "x", KumaPass: "y"}); err != nil {
		t.Fatalf("migrate again: %v", err)
	}
	insts, _ = db.GetKumaInstances()
	if len(insts) != 1 || insts[0].URL != "http://kuma:3001" {
		t.Errorf("migrate not idempotent: %+v", insts)
	}
}

func TestMigrateKumaInstancesEmptyURL(t *testing.T) {
	db := setupTestDB(t)

	if err := db.MigrateKumaInstances(Settings{KumaURL: ""}); err != nil {
		t.Fatalf("migrate empty: %v", err)
	}
	insts, _ := db.GetKumaInstances()
	if len(insts) != 0 {
		t.Fatalf("expected 0 instances for empty url, got %d", len(insts))
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
