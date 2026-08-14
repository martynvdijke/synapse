package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"synapse/internal/db"
	"synapse/internal/docker"
	"synapse/internal/kuma"
	"synapse/internal/npm"
	synclib "synapse/internal/sync"
)

func setupTest(t *testing.T) (*App, *gin.Engine) {
	t.Setenv("COMPOSE_PATH", "testdata/docker-compose.yml")
	t.Setenv("NPM_HOST", "http://localhost:81")
	t.Setenv("NPM_USER", "admin")
	t.Setenv("NPM_PASS", "")
	t.Setenv("KUMA_URL", "http://localhost:3001")
	t.Setenv("KUMA_USER", "admin")
	t.Setenv("KUMA_PASS", "")

	tmpDB := t.TempDir() + "/test.db"
	database, err := db.Open(tmpDB)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	app := &App{
		database:     database,
		kumaRegistry: kuma.NewRegistry(database),
		npmRegistry:  npm.NewRegistry(database),
	}
	r := setupRouter(app)
	return app, r
}

func createTestSession(t *testing.T, app *App) string {
	_, err := app.database.CreateAdminUser("admin", "$2a$10$dummyhashnotusedfortest")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	sessionID := generateSessionID()
	sessionStoreMu.Lock()
	sessionStore[sessionID] = sessionInfo{Expiry: time.Now().Add(1 * time.Hour)}
	sessionStoreMu.Unlock()
	return sessionID
}

func authRequest(t *testing.T, method, path string, body string, sessionID string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if sessionID != "" {
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	}
	return req
}

func TestCheckSetup_NoAdmin(t *testing.T) {
	_, r := setupTest(t)
	req := httptest.NewRequest("GET", "/api/check-setup", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body["setup"] != false {
		t.Errorf("expected setup=false, got %v", body["setup"])
	}
}

func TestCheckSetup_WithAdmin(t *testing.T) {
	app, r := setupTest(t)
	createTestSession(t, app)

	req := httptest.NewRequest("GET", "/api/check-setup", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body["setup"] != true {
		t.Errorf("expected setup=true, got %v", body["setup"])
	}
}

func TestSetupLogin_CreatesAdmin(t *testing.T) {
	app, r := setupTest(t)
	t.Cleanup(func() {}) // app already cleaned up

	body := `{"username":"admin","password":"testpassword123","setup":true}`
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	count, err := app.database.CountAdminUsers()
	if err != nil {
		t.Fatalf("count admin users: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 admin user, got %d", count)
	}
}

func TestSetupLogin_ShortPassword(t *testing.T) {
	_, r := setupTest(t)

	body := `{"username":"admin","password":"short","setup":true}`
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSetupLogin_AlreadySetup(t *testing.T) {
	app, r := setupTest(t)
	createTestSession(t, app)

	body := `{"username":"admin2","password":"testpassword123","setup":true}`
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	app, r := setupTest(t)
	_, err := app.database.CreateAdminUser("admin", "$2a$10$dummyhas")
	if err != nil {
		// Just proceed - the login flow will handle missing user gracefully
	}

	body := `{"username":"admin","password":"wrongpassword"}`
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestLogin_Successful(t *testing.T) {
	app, r := setupTest(t)

	// Create admin with known password hash
	hash, err := bcrypt.GenerateFromPassword([]byte("correctpass"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	_, err = app.database.CreateAdminUser("admin", string(hash))
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	body := `{"username":"admin","password":"correctpass"}`
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPI_RequiresAuth(t *testing.T) {
	_, r := setupTest(t)

	endpoints := []string{
		"/api/status",
		"/api/settings",
		"/api/sync/history",
		"/api/monitors",
		"/api/monitors/1/stats",
		"/api/npm-instances",
		"/api/authelia/status",
		"/api/authelia/alerts",
		"/api/authelia/temp-access",
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest("GET", ep, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401, got %d", ep, w.Code)
		}
	}
}

func TestLogout(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	req := authRequest(t, "POST", "/api/logout", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Session should be invalidated
	req2 := httptest.NewRequest("GET", "/api/status", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 after logout, got %d", w2.Code)
	}
}

func TestStatusEndpoint(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	req := authRequest(t, "GET", "/api/status", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := body["docker_count"]; !ok {
		t.Error("missing docker_count")
	}
	if _, ok := body["monitor_count"]; !ok {
		t.Error("missing monitor_count")
	}
}

func TestSyncHistoryEmpty(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	req := authRequest(t, "GET", "/api/sync/history", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected empty array, got %d items", len(body))
	}
}

func TestDashboard(t *testing.T) {
	app, r := setupTest(t)
	createTestSession(t, app)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestSettingsEndpoint(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// GET settings returns defaults
	req := authRequest(t, "GET", "/api/settings", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var settings map[string]any
	json.NewDecoder(w.Body).Decode(&settings)
	// kuma_* settings moved to the /api/kuma-instances endpoints and are
	// no longer part of the settings response.
	if _, ok := settings["kuma_url"]; ok {
		t.Errorf("kuma_url should not be in settings response, got %v", settings["kuma_url"])
	}
	if settings["npm_pass"] != "" {
		t.Errorf("expected empty npm_pass, got %v", settings["npm_pass"])
	}
}

func TestSettingsSaveAndLoad(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// npm_host is now managed via /api/npm-instances; compose_path is still managed via /api/settings
	body := `{"compose_path":"/data/compose.yml"}`
	req := authRequest(t, "POST", "/api/settings", body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("post settings: expected 200, got %d", w.Code)
	}

	// GET should return saved compose_path only
	req2 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var s2 map[string]any
	json.NewDecoder(w2.Body).Decode(&s2)
	if s2["compose_path"] != "/data/compose.yml" {
		t.Errorf("expected saved compose_path, got %v", s2["compose_path"])
	}
}

func TestSaveSettingsOnlySentFields(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// Save compose_path first via direct DB
	if err := app.database.SaveSettingsMap(map[string]string{
		"compose_path": "/docker/compose.yml",
	}); err != nil {
		t.Fatalf("seed compose_path: %v", err)
	}

	// POST settings with ONLY otel_enabled — compose_path should NOT be affected
	body := `{"otel_enabled":true}`
	req := authRequest(t, "POST", "/api/settings", body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("post settings: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// GET settings and verify only sent fields changed
	req2 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var s map[string]any
	json.NewDecoder(w2.Body).Decode(&s)

	if s["compose_path"] != "/docker/compose.yml" {
		t.Errorf("compose_path should be preserved: expected %q, got %v", "/docker/compose.yml", s["compose_path"])
	}
	if s["otel_enabled"] != true {
		t.Errorf("otel_enabled should be true: got %v", s["otel_enabled"])
	}
}

func TestEinkEnabledSaveAndLoad(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// Default: eink_enabled should be false
	req0 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w0 := httptest.NewRecorder()
	r.ServeHTTP(w0, req0)
	var s0 map[string]any
	json.NewDecoder(w0.Body).Decode(&s0)
	if s0["eink_enabled"] != false {
		t.Errorf("default eink_enabled should be false: got %v", s0["eink_enabled"])
	}

	// Seed compose_path, then enable eink — compose_path must be preserved
	if err := app.database.SaveSettingsMap(map[string]string{
		"compose_path": "/docker/compose.yml",
	}); err != nil {
		t.Fatalf("seed compose_path: %v", err)
	}

	body := `{"eink_enabled":true}`
	req := authRequest(t, "POST", "/api/settings", body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("post settings: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	req2 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var s map[string]any
	json.NewDecoder(w2.Body).Decode(&s)

	if s["eink_enabled"] != true {
		t.Errorf("eink_enabled should be true after save: got %v", s["eink_enabled"])
	}
	if s["compose_path"] != "/docker/compose.yml" {
		t.Errorf("compose_path should be preserved: expected %q, got %v", "/docker/compose.yml", s["compose_path"])
	}

	// Toggle back off via only-sent-fields
	body2 := `{"eink_enabled":false}`
	req3 := authRequest(t, "POST", "/api/settings", body2, sessionID)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("post settings (off): expected 200, got %d: %s", w3.Code, w3.Body.String())
	}

	req4 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	var s4 map[string]any
	json.NewDecoder(w4.Body).Decode(&s4)
	if s4["eink_enabled"] != false {
		t.Errorf("eink_enabled should be false after toggling off: got %v", s4["eink_enabled"])
	}
	if s4["compose_path"] != "/docker/compose.yml" {
		t.Errorf("compose_path should remain preserved: expected %q, got %v", "/docker/compose.yml", s4["compose_path"])
	}
}

func TestTrmnlApiTokenSaveAndLoad(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// Default: no token configured
	req0 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w0 := httptest.NewRecorder()
	r.ServeHTTP(w0, req0)
	var s0 map[string]any
	json.NewDecoder(w0.Body).Decode(&s0)
	if s0["trmnl_api_token"] != "" {
		t.Errorf("default trmnl_api_token should be empty: got %v", s0["trmnl_api_token"])
	}

	// Seed compose_path, then save a token — compose_path must be preserved
	if err := app.database.SaveSettingsMap(map[string]string{
		"compose_path": "/docker/compose.yml",
	}); err != nil {
		t.Fatalf("seed compose_path: %v", err)
	}

	body := `{"trmnl_api_token":"secret-token-123"}`
	req := authRequest(t, "POST", "/api/settings", body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("post settings: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	req2 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var s map[string]any
	json.NewDecoder(w2.Body).Decode(&s)
	if s["trmnl_api_token"] != "secret-token-123" {
		t.Errorf("trmnl_api_token should round-trip: got %v", s["trmnl_api_token"])
	}
	if s["compose_path"] != "/docker/compose.yml" {
		t.Errorf("compose_path should be preserved: expected %q, got %v", "/docker/compose.yml", s["compose_path"])
	}
}

func TestTrmnlStatsEndpoint(t *testing.T) {
	app, r := setupTest(t)

	// No token configured → 503, no data leak
	reqNoToken := httptest.NewRequest("GET", "/api/v1/trmnl/stats", nil)
	wNoToken := httptest.NewRecorder()
	r.ServeHTTP(wNoToken, reqNoToken)
	if wNoToken.Code != http.StatusServiceUnavailable {
		t.Fatalf("no token configured: expected 503, got %d: %s", wNoToken.Code, wNoToken.Body.String())
	}

	// Configure token
	if err := app.database.SaveSettingsMap(map[string]string{
		"trmnl_api_token": "sekret",
	}); err != nil {
		t.Fatalf("save token: %v", err)
	}

	// Missing token → 401
	reqMissing := httptest.NewRequest("GET", "/api/v1/trmnl/stats", nil)
	wMissing := httptest.NewRecorder()
	r.ServeHTTP(wMissing, reqMissing)
	if wMissing.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: expected 401, got %d", wMissing.Code)
	}

	// Wrong token → 401
	reqWrong := httptest.NewRequest("GET", "/api/v1/trmnl/stats", nil)
	reqWrong.Header.Set("Authorization", "Bearer wrong")
	wWrong := httptest.NewRecorder()
	r.ServeHTTP(wWrong, reqWrong)
	if wWrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: expected 401, got %d", wWrong.Code)
	}

	// Valid Bearer token → 200 + flat payload fields
	reqOk := httptest.NewRequest("GET", "/api/v1/trmnl/stats", nil)
	reqOk.Header.Set("Authorization", "Bearer sekret")
	wOk := httptest.NewRecorder()
	r.ServeHTTP(wOk, reqOk)
	if wOk.Code != http.StatusOK {
		t.Fatalf("valid bearer: expected 200, got %d: %s", wOk.Code, wOk.Body.String())
	}
	var s map[string]any
	json.NewDecoder(wOk.Body).Decode(&s)
	for _, field := range []string{"docker_count", "npm_count", "monitor_count", "running", "last_docker", "last_npm", "docker_ok", "npm_ok", "kuma_ok", "up", "down"} {
		if _, ok := s[field]; !ok {
			t.Errorf("payload missing field %q: %v", field, s)
		}
	}
	if _, nested := s["connection_health"]; nested {
		t.Errorf("payload must be flat, found connection_health: %v", s)
	}

	// Valid token via query param → 200
	reqQuery := httptest.NewRequest("GET", "/api/v1/trmnl/stats?token=sekret", nil)
	wQuery := httptest.NewRecorder()
	r.ServeHTTP(wQuery, reqQuery)
	if wQuery.Code != http.StatusOK {
		t.Fatalf("valid query token: expected 200, got %d: %s", wQuery.Code, wQuery.Body.String())
	}
}

func TestSaveSettingsPreservesEmptyPassword(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// npm_* settings are now managed via /api/npm-instances; test that legacy fields are still readable but not writable
	if err := app.database.SaveSettingsMap(map[string]string{
		"npm_pass": "secret123",
	}); err != nil {
		t.Fatalf("seed npm_pass: %v", err)
	}

	// Legacy npm_* fields should be ignored by the settings PUT handler
	body := `{"npm_pass":"","npm_host":"http://test:81","compose_path":"/custom/path.yml"}`
	req := authRequest(t, "POST", "/api/settings", body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("post settings: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify compose_path was updated (still active) and npm_* were ignored
	req2 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var s map[string]any
	json.NewDecoder(w2.Body).Decode(&s)

	if s["npm_pass"] != "****" {
		t.Errorf("npm_pass should be masked and non-empty: got %v", s["npm_pass"])
	}
	if s["compose_path"] != "/custom/path.yml" {
		t.Errorf("compose_path should be updated: expected %q, got %v", "/custom/path.yml", s["compose_path"])
	}
}

func TestKumaMonitorsHandler_Empty(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	req := authRequest(t, "GET", "/api/monitors", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body == nil || len(body) != 0 {
		t.Errorf("expected empty array, got %v", body)
	}
}

func TestKumaMonitorStatsHandler_InvalidID(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	req := authRequest(t, "GET", "/api/monitors/abc/stats?instance=1", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestKumaMonitorStatsHandler_NoInstanceParam(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	req := authRequest(t, "GET", "/api/monitors/1/stats", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing instance param, got %d: %s", w.Code, w.Body.String())
	}
}

func TestKumaMonitorStatsHandler_NotFoundInstance(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	req := authRequest(t, "GET", "/api/monitors/1/stats?instance=999", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-existent instance, got %d: %s", w.Code, w.Body.String())
	}
}

func TestNPMInstancesHandler_List(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// Create one instance directly via DB
	if _, err := app.database.CreateNPMInstance(&db.NPMInstance{Name: "test", URL: "https://npm:81", Username: "admin", Password: "pass", Enabled: true}); err != nil {
		t.Fatalf("seed instance: %v", err)
	}

	req := authRequest(t, "GET", "/api/npm-instances", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(body))
	}
	if body[0]["name"] != "test" {
		t.Errorf("expected name=test, got %v", body[0]["name"])
	}
}

func TestNPMInstancesHandler_Create(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	body := `{"name":"new","url":"https://npm:81","username":"admin","password":"secret","enabled":true}`
	req := authRequest(t, "POST", "/api/npm-instances", body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if id, ok := resp["id"].(float64); !ok || id <= 0 {
		t.Errorf("expected positive id, got %v", resp["id"])
	}
	if resp["name"] != "new" {
		t.Errorf("expected name=new, got %v", resp["name"])
	}
}

func TestNPMInstancesHandler_CreateMissingFields(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	body := `{"name":"only-name"}`
	req := authRequest(t, "POST", "/api/npm-instances", body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing url, got %d: %s", w.Code, w.Body.String())
	}
}

func TestNPMInstancesHandler_Update(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	inst, err := app.database.CreateNPMInstance(&db.NPMInstance{Name: "old", URL: "https://old:81", Username: "admin", Password: "oldpass", Enabled: true})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	body := `{"name":"updated","url":"https://new:81","username":"newuser","enabled":false}`
	req := authRequest(t, "PUT", "/api/npm-instances/"+fmt.Sprintf("%d", inst.ID), body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["name"] != "updated" {
		t.Errorf("expected name=updated, got %v", resp["name"])
	}
	if resp["url"] != "https://new:81" {
		t.Errorf("expected url=https://new:81, got %v", resp["url"])
	}
}

func TestNPMInstancesHandler_UpdateNotFound(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	body := `{"name":"nope"}`
	req := authRequest(t, "PUT", "/api/npm-instances/999", body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestNPMInstancesHandler_Delete(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	inst, err := app.database.CreateNPMInstance(&db.NPMInstance{Name: "del", URL: "https://del:81", Username: "admin", Password: "p", Enabled: true})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := authRequest(t, "DELETE", "/api/npm-instances/"+fmt.Sprintf("%d", inst.ID), "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "deleted" {
		t.Errorf("expected status=deleted, got %v", resp["status"])
	}

	// Verify it's gone
	req2 := authRequest(t, "GET", "/api/npm-instances", "", sessionID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var list []map[string]any
	json.NewDecoder(w2.Body).Decode(&list)
	if len(list) != 0 {
		t.Errorf("expected empty list after delete, got %d items", len(list))
	}
}

func TestNPMInstancesHandler_Test(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	inst, err := app.database.CreateNPMInstance(&db.NPMInstance{Name: "test", URL: "https://test:81", Username: "admin", Password: "p", Enabled: true})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := authRequest(t, "POST", "/api/npm-instances/"+fmt.Sprintf("%d", inst.ID)+"/test", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	// NPM at localhost:81 is not running, so we expect ok=false
	if resp["ok"] != false {
		t.Logf("test result: %+v", resp)
	}
}

func TestSaveSettingsDoesNotOverwriteUnsentOtelFields(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// Seed DB with otel_enabled=true
	if err := app.database.SaveSettingsMap(map[string]string{
		"otel_enabled": "true",
	}); err != nil {
		t.Fatalf("seed otel_enabled: %v", err)
	}

	// POST settings without any otel fields
	body := `{"kuma_url":"http://kuma:3001"}`
	req := authRequest(t, "POST", "/api/settings", body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("post settings: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// otel_enabled should still be true
	req2 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var s map[string]any
	json.NewDecoder(w2.Body).Decode(&s)

	if s["otel_enabled"] != true {
		t.Errorf("otel_enabled should remain true: got %v", s["otel_enabled"])
	}
}

func setupRouter(app *App) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	absPath, _ := filepath.Abs("static/*.html")
	if _, err := os.Stat("static/index.html"); os.IsNotExist(err) {
		absPath = filepath.Join(filepath.Dir(absPath), "../../static/*.html")
	}
	r.LoadHTMLGlob(absPath)

	r.GET("/", app.Dashboard)
	r.GET("/login", func(c *gin.Context) { c.HTML(http.StatusOK, "login.html", nil) })
	r.GET("/setup", func(c *gin.Context) { c.HTML(http.StatusOK, "setup.html", nil) })
	r.GET("/api/check-setup", app.HandleCheckSetup)
	r.POST("/api/login", app.HandleLogin)

	apiV1 := r.Group("/api/v1")
	{
		apiV1.GET("/trmnl/stats", app.TrmnlStats)
	}

	api := r.Group("/api")
	api.Use(authMiddleware())
	{
		api.POST("/logout", app.HandleLogout)
		api.GET("/settings", app.GetSettings)
		api.POST("/settings", app.SaveSettings)
		api.GET("/status", app.Status)
		api.GET("/sync/history", app.SyncHistory)
		api.GET("/services", app.Services)
		api.GET("/kuma-instances", app.ListKumaInstances)
		api.POST("/kuma-instances", app.CreateKumaInstance)
		api.PUT("/kuma-instances/:id", app.UpdateKumaInstance)
		api.DELETE("/kuma-instances/:id", app.DeleteKumaInstance)
		api.POST("/kuma-instances/:id/test", app.TestKumaInstance)
		api.GET("/npm-instances", app.ListNPMInstances)
		api.POST("/npm-instances", app.CreateNPMInstance)
		api.PUT("/npm-instances/:id", app.UpdateNPMInstance)
		api.DELETE("/npm-instances/:id", app.DeleteNPMInstance)
		api.POST("/npm-instances/:id/test", app.TestNPMInstance)
		api.POST("/sync/docker", app.DockerSync)
		api.POST("/sync/npm", app.NPMSync)
		api.GET("/monitors", app.KumaMonitors)
		api.POST("/monitors", app.CreateKumaMonitor)
		api.PUT("/monitors/:kumaId", app.UpdateKumaMonitor)
		api.DELETE("/monitors/:kumaId", app.DeleteKumaMonitor)
		api.GET("/monitors/:id/stats", app.KumaMonitorStats)
		api.GET("/npm/proxy-hosts", app.NPMProxyHosts)
		api.POST("/npm/proxy-hosts", app.CreateNPMProxyHost)
		api.PUT("/npm/proxy-hosts/:id", app.UpdateNPMProxyHost)
		api.GET("/service-links", app.ServiceLinks)
		api.POST("/service-links", app.CreateServiceLink)
		api.PUT("/service-links/:id", app.UpdateServiceLink)
		api.DELETE("/service-links/:id", app.DeleteServiceLink)
		api.POST("/service-links/:id/refresh", app.RefreshServiceLink)
		api.POST("/reconcile", app.Reconcile)
		api.GET("/reconcile/runs", app.ReconcileRuns)
		api.GET("/docker/events", app.DockerEvents)
		api.GET("/events", app.EventsFeed)
		api.POST("/notify/test", app.NotifyTest)
		api.GET("/notify/missing", app.NotifyMissing)
		api.GET("/authelia/status", app.AutheliaStatus)
		api.GET("/authelia/alerts", app.AutheliaAlerts)
		api.POST("/authelia/alerts/:id/resolve", app.AutheliaResolveAlert)
		api.GET("/authelia/temp-access", app.AutheliaTempAccess)
		api.POST("/authelia/temp-access", app.AutheliaAddTempAccess)
		api.POST("/authelia/temp-access/:id/revoke", app.AutheliaRevokeTempAccess)
		api.POST("/authelia/sync", app.AutheliaSync)
	}

	return r
}

func TestKumaInstancesHandler_Test_Unreachable(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	inst, err := app.database.CreateKumaInstance(&db.KumaInstance{Name: "test", URL: "http://127.0.0.1:1", Username: "admin", Password: "p", Enabled: true})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := authRequest(t, "POST", "/api/kuma-instances/"+fmt.Sprintf("%d", inst.ID)+"/test", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	// Instances at 127.0.0.1:1 is not running, so we expect ok=false
	if resp["ok"] != false {
		t.Logf("test result: %+v", resp)
	}
}

func TestNPMInstancesHandler_Test_2FAError(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// Mock server that returns requires_2fa: true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tokens" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"requires_2fa": true,
				"challenge_token": "challenge-jwt",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	inst, err := app.database.CreateNPMInstance(&db.NPMInstance{Name: "2fa-test", URL: srv.URL, Username: "admin", Password: "p", Enabled: true})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := authRequest(t, "POST", "/api/npm-instances/"+fmt.Sprintf("%d", inst.ID)+"/test", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["ok"] != false {
		t.Fatalf("expected ok=false for 2FA account, got %+v", resp)
	}
	if resp["message"] == nil || resp["message"] == "" {
		t.Fatalf("expected error message for 2FA, got %+v", resp)
	}
}

func TestNotifyTest_Unconfigured(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	req := authRequest(t, "POST", "/api/notify/test", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["ok"] != false {
		t.Errorf("expected ok=false, got %+v", resp)
	}
}

func TestNotifyTest_Success(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	var gotPath, gotKey, gotTitle, gotMsg string
	gotPriority := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		gotKey = req.Header.Get("X-Gotify-Key")
		var body map[string]any
		json.NewDecoder(req.Body).Decode(&body)
		gotTitle, _ = body["title"].(string)
		gotMsg, _ = body["message"].(string)
		if p, ok := body["priority"].(float64); ok {
			gotPriority = int(p)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	if err := app.database.SaveSettingsMap(map[string]string{
		"gotify_url":      srv.URL,
		"gotify_token":    "app-token",
		"gotify_priority": "7",
	}); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	req := authRequest(t, "POST", "/api/notify/test", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["ok"] != true {
		t.Fatalf("expected ok=true, got %+v", resp)
	}
	if gotPath != "/message" {
		t.Errorf("expected POST /message, got %q", gotPath)
	}
	if gotKey != "app-token" {
		t.Errorf("expected X-Gotify-Key app-token, got %q", gotKey)
	}
	if gotPriority != 7 {
		t.Errorf("expected priority 7, got %d", gotPriority)
	}
	if gotTitle == "" || gotMsg == "" {
		t.Errorf("expected title+message in payload, got %q / %q", gotTitle, gotMsg)
	}
}

func TestNotifyEndpoints_RequireAuth(t *testing.T) {
	_, r := setupTest(t)
	for _, path := range []string{"/api/notify/test", "/api/notify/missing"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "/test") {
			method = http.MethodPost
		}
		req := httptest.NewRequest(method, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401, got %d", path, w.Code)
		}
	}
}

func TestNotifyMissing_ReportsMissingItems(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// No Kuma/NPM instances configured and testdata compose has 5 services,
	// all of which are uncovered (no Kuma clients), so they are reported missing.
	req := authRequest(t, "GET", "/api/notify/missing", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["degraded"] != false {
		t.Errorf("expected degraded=false, got %+v", resp)
	}
	docker, ok := resp["docker"].([]any)
	if !ok || len(docker) != 6 {
		t.Errorf("expected 6 missing docker services, got %+v", resp["docker"])
	}
	if npm, ok := resp["npm"].([]any); !ok || len(npm) != 0 {
		t.Errorf("expected empty npm list, got %+v", resp["npm"])
	}
	if resp["fetched_at"] == nil || resp["fetched_at"] == "" {
		t.Errorf("expected fetched_at timestamp, got %+v", resp["fetched_at"])
	}
}

func TestNotifyMissing_DegradedWhenComposeMissing(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)
	t.Setenv("COMPOSE_PATH", "/nonexistent/docker-compose.yml")

	req := authRequest(t, "GET", "/api/notify/missing", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["degraded"] != true {
		t.Errorf("expected degraded=true, got %+v", resp)
	}
	reasons, ok := resp["reasons"].([]any)
	if !ok || len(reasons) == 0 {
		t.Errorf("expected reasons, got %+v", resp["reasons"])
	}
}

func TestSettingsRoundTrip_NotifyFields(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// Seed a token, then verify it is masked in GET responses.
	if err := app.database.SaveSettingsMap(map[string]string{
		"gotify_token": "secret-token",
	}); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	// POST clamps: interval below 1 → 1, priority above 10 → 10.
	body := `{"notify_enabled":true,"notify_interval_minutes":0,"gotify_url":"http://gotify:8080","gotify_priority":99}`
	req := authRequest(t, "POST", "/api/settings", body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("post settings: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	req2 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var s map[string]any
	json.NewDecoder(w2.Body).Decode(&s)

	if s["notify_enabled"] != true {
		t.Errorf("notify_enabled: got %v", s["notify_enabled"])
	}
	if s["notify_interval_minutes"] != float64(1) {
		t.Errorf("notify_interval_minutes clamp: got %v", s["notify_interval_minutes"])
	}
	if s["gotify_priority"] != float64(10) {
		t.Errorf("gotify_priority clamp: got %v", s["gotify_priority"])
	}
	if s["gotify_url"] != "http://gotify:8080" {
		t.Errorf("gotify_url: got %v", s["gotify_url"])
	}
	if s["gotify_token"] != "****" {
		t.Errorf("gotify_token should be masked: got %v", s["gotify_token"])
	}

	// Sending "****" back must not overwrite the stored token.
	body2 := `{"gotify_token":"****"}`
	req3 := authRequest(t, "POST", "/api/settings", body2, sessionID)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("post settings (masked token): expected 200, got %d: %s", w3.Code, w3.Body.String())
	}

	req4 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	var s2 map[string]any
	json.NewDecoder(w4.Body).Decode(&s2)
	if s2["gotify_token"] != "****" {
		t.Errorf("masked token should remain stored: got %v", s2["gotify_token"])
	}
}

func TestSettingsRoundTrip_ReconcileDockerFields(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	body := `{"docker_socket":"unix:///var/run/docker.sock","docker_events_enabled":true,"docker_events_retention_days":14,"reconcile_enabled":true,"reconcile_interval_minutes":15,"reconcile_dry_run_default":false,"notify_docker_die":true,"notify_docker_health":true,"notify_docker_image":false,"notify_reconcile":true,"notify_cooldown_minutes":3}`
	req := authRequest(t, "POST", "/api/settings", body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("post settings: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	req2 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var s map[string]any
	json.NewDecoder(w2.Body).Decode(&s)

	if s["docker_socket"] != "unix:///var/run/docker.sock" {
		t.Errorf("docker_socket: got %v", s["docker_socket"])
	}
	if s["docker_events_enabled"] != true {
		t.Errorf("docker_events_enabled: got %v", s["docker_events_enabled"])
	}
	if s["docker_events_retention_days"] != float64(14) {
		t.Errorf("docker_events_retention_days: got %v", s["docker_events_retention_days"])
	}
	if s["reconcile_enabled"] != true {
		t.Errorf("reconcile_enabled: got %v", s["reconcile_enabled"])
	}
	if s["reconcile_interval_minutes"] != float64(15) {
		t.Errorf("reconcile_interval_minutes: got %v", s["reconcile_interval_minutes"])
	}
	if s["reconcile_dry_run_default"] != false {
		t.Errorf("reconcile_dry_run_default: got %v", s["reconcile_dry_run_default"])
	}
	if s["notify_docker_die"] != true || s["notify_docker_health"] != true || s["notify_docker_image"] != false {
		t.Errorf("notify toggles: die=%v health=%v image=%v", s["notify_docker_die"], s["notify_docker_health"], s["notify_docker_image"])
	}
	if s["notify_reconcile"] != true {
		t.Errorf("notify_reconcile: got %v", s["notify_reconcile"])
	}
	if s["notify_cooldown_minutes"] != float64(3) {
		t.Errorf("notify_cooldown_minutes: got %v", s["notify_cooldown_minutes"])
	}

	// Clamping: retention below 1 → 1, cooldown above 1440 → 1440.
	body2 := `{"docker_events_retention_days":0,"notify_cooldown_minutes":9999}`
	req3 := authRequest(t, "POST", "/api/settings", body2, sessionID)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("post settings clamp: expected 200, got %d", w3.Code)
	}
	req4 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	var s2 map[string]any
	json.NewDecoder(w4.Body).Decode(&s2)
	if s2["docker_events_retention_days"] != float64(1) {
		t.Errorf("retention clamp: got %v", s2["docker_events_retention_days"])
	}
	if s2["notify_cooldown_minutes"] != float64(1440) {
		t.Errorf("cooldown clamp: got %v", s2["notify_cooldown_minutes"])
	}
}

func TestReconcileEndpoint_NoLinks(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	req := authRequest(t, "POST", "/api/reconcile", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reconcile: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Run struct {
			Source string `json:"source"`
			Status string `json:"status"`
			DryRun bool   `json:"dry_run"`
		} `json:"run"`
		Changes []map[string]any `json:"changes"`
		DryRun  bool             `json:"dry_run"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode reconcile response: %v", err)
	}
	if resp.Run.Source != "reconcile" {
		t.Errorf("run source: got %q", resp.Run.Source)
	}
	if resp.Run.Status != "completed" {
		t.Errorf("run status: got %q", resp.Run.Status)
	}
	// Default dry-run from settings is true.
	if !resp.Run.DryRun {
		t.Errorf("run dry_run should default to true, got %v", resp.Run.DryRun)
	}

	// The run must be persisted and visible via /api/reconcile/runs.
	req2 := authRequest(t, "GET", "/api/reconcile/runs", "", sessionID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var runs []db.SyncRun
	if err := json.NewDecoder(w2.Body).Decode(&runs); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 reconcile run, got %d", len(runs))
	}
	if runs[0].Source != "reconcile" || runs[0].Status != "completed" {
		t.Errorf("unexpected run: %+v", runs[0])
	}
}

func TestReconcileEndpoint_ExplicitApply(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	req := authRequest(t, "POST", "/api/reconcile", `{"dry_run":false}`, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reconcile: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Run struct {
			DryRun bool `json:"dry_run"`
		} `json:"run"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Run.DryRun {
		t.Errorf("explicit dry_run=false should apply, run reported dry_run=true")
	}
}

func TestDockerEventsEndpoint(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	now := time.Now()
	for i, ev := range []db.DockerEvent{
		{EventType: "container", Action: "start", ActorName: "web", Image: "nginx:1.25", CreatedAt: now.Add(-time.Minute)},
		{EventType: "container", Action: "die", ActorName: "api", Image: "myapp:latest", CreatedAt: now},
		{EventType: "image", Action: "update", ActorName: "web", ImageOld: "nginx:1.25", ImageNew: "nginx:1.27", CreatedAt: now.Add(-30 * time.Second)},
	} {
		ev.ActorID = fmt.Sprintf("id%d", i)
		if err := app.database.CreateDockerEvent(&ev); err != nil {
			t.Fatalf("seed event: %v", err)
		}
	}

	// Full list, newest first.
	req := authRequest(t, "GET", "/api/docker/events", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("docker events: expected 200, got %d", w.Code)
	}
	var events []db.DockerEvent
	json.NewDecoder(w.Body).Decode(&events)
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Action != "die" {
		t.Errorf("expected newest-first ordering, first=%s", events[0].Action)
	}

	// Filter by action.
	req2 := authRequest(t, "GET", "/api/docker/events?action=die", "", sessionID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var dieEvents []db.DockerEvent
	json.NewDecoder(w2.Body).Decode(&dieEvents)
	if len(dieEvents) != 1 || dieEvents[0].ActorName != "api" {
		t.Fatalf("action filter: expected 1 die event for api, got %+v", dieEvents)
	}

	// Filter by container (substring).
	req3 := authRequest(t, "GET", "/api/docker/events?container=web", "", sessionID)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	var webEvents []db.DockerEvent
	json.NewDecoder(w3.Body).Decode(&webEvents)
	if len(webEvents) != 2 {
		t.Fatalf("container filter: expected 2 web events, got %d", len(webEvents))
	}

	// Invalid since → 400.
	req4 := authRequest(t, "GET", "/api/docker/events?since=garbage", "", sessionID)
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusBadRequest {
		t.Errorf("invalid since: expected 400, got %d", w4.Code)
	}
}

func TestEventsFeed(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// Seed a docker event (2 min ago) and a reconcile run (1 min ago).
	past := time.Now().Add(-2 * time.Minute)
	if err := app.database.CreateDockerEvent(&db.DockerEvent{
		EventType: "container", Action: "start", ActorName: "web", Image: "nginx:1.25", CreatedAt: past,
	}); err != nil {
		t.Fatalf("seed docker event: %v", err)
	}

	// Run reconcile directly to persist a run (no links → completed).
	synclib.RunReconcile("testdata/docker-compose.yml", nil, nil, app.database, synclib.ReconcileOptions{DryRun: true}, nil)

	req := authRequest(t, "GET", "/api/events", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("events feed: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var items []FeedItem
	json.NewDecoder(w.Body).Decode(&items)

	kinds := map[string]int{}
	for _, it := range items {
		kinds[it.Kind]++
	}
	if kinds["docker"] < 1 {
		t.Errorf("expected docker feed item, got %+v", items)
	}
	if kinds["reconcile"] < 1 {
		t.Errorf("expected reconcile feed item, got %+v", items)
	}
	// Newest first (reconcile run happened after the docker event).
	if len(items) >= 2 && items[0].Kind != "reconcile" {
		t.Errorf("expected newest (reconcile) first, got %+v", items[0])
	}
}

func TestServicesWithContainerState(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// Fake docker engine reporting one running container for the compose
	// "web" service (container name nginx-web).
	app.dockerClient = fakeDockerEngine(t, []docker.ContainerSummary{
		{ID: "abc123", Names: []string{"/nginx-web"}, Image: "nginx:1.25", State: "running", Status: "Up 2 hours"},
	})

	req := authRequest(t, "GET", "/api/services", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("services: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var services []map[string]any
	json.NewDecoder(w.Body).Decode(&services)

	var web *map[string]any
	for i := range services {
		if services[i]["container_name"] == "nginx-web" {
			web = &services[i]
			break
		}
	}
	if web == nil {
		t.Fatal("nginx-web not found in services response")
	}
	if (*web)["container_state"] != "running" {
		t.Errorf("container_state: got %v", (*web)["container_state"])
	}
	if (*web)["container_status"] != "Up 2 hours" {
		t.Errorf("container_status: got %v", (*web)["container_status"])
	}
}

func fakeDockerEngine(t *testing.T, containers []docker.ContainerSummary) *docker.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/json" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(containers)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return docker.NewWithClient(srv.URL, srv.Client())
}
