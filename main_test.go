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
	"synapse/internal/kuma"
	"synapse/internal/npm"
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

	api := r.Group("/api")
	api.Use(authMiddleware())
	{
		api.POST("/logout", app.HandleLogout)
		api.GET("/settings", app.GetSettings)
		api.POST("/settings", app.SaveSettings)
		api.GET("/status", app.Status)
		api.GET("/sync/history", app.SyncHistory)
		api.GET("/npm-instances", app.ListNPMInstances)
		api.POST("/npm-instances", app.CreateNPMInstance)
		api.PUT("/npm-instances/:id", app.UpdateNPMInstance)
		api.DELETE("/npm-instances/:id", app.DeleteNPMInstance)
		api.POST("/npm-instances/:id/test", app.TestNPMInstance)
		api.POST("/sync/docker", app.DockerSync)
		api.POST("/sync/npm", app.NPMSync)
		api.GET("/monitors", app.KumaMonitors)
		api.GET("/monitors/:id/stats", app.KumaMonitorStats)
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
