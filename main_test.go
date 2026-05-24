package main

import (
	"encoding/json"
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
		database: database,
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
	if settings["kuma_url"] != "http://localhost:3001" {
		t.Errorf("unexpected kuma_url: %v", settings["kuma_url"])
	}
	if settings["npm_pass"] != "" {
		t.Errorf("expected empty npm_pass, got %v", settings["npm_pass"])
	}
	if settings["kuma_pass"] != "" {
		t.Errorf("expected empty kuma_pass, got %v", settings["kuma_pass"])
	}
}

func TestSettingsSaveAndLoad(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// POST new settings
	body := `{"kuma_url":"http://new-kuma:4000","npm_host":"http://npm:81"}`
	req := authRequest(t, "POST", "/api/settings", body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("post settings: expected 200, got %d", w.Code)
	}

	// GET should return saved values
	req2 := authRequest(t, "GET", "/api/settings", "", sessionID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	var s2 map[string]any
	json.NewDecoder(w2.Body).Decode(&s2)
	if s2["kuma_url"] != "http://new-kuma:4000" {
		t.Errorf("expected saved kuma_url, got %v", s2["kuma_url"])
	}
	if s2["npm_host"] != "http://npm:81" {
		t.Errorf("expected saved npm_host, got %v", s2["npm_host"])
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
		api.POST("/sync/docker", app.DockerSync)
		api.POST("/sync/npm", app.NPMSync)
		api.GET("/monitors", app.KumaMonitors)
	}

	return r
}
