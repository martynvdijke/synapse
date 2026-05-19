package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"synapse/internal/db"
)

func TestStatusEndpoint(t *testing.T) {
	tmpDB := t.TempDir() + "/test.db"
	database, err := db.Open(tmpDB)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	app := &App{
		database: database,
		defaultSettings: db.Settings{
			ComposePath: "testdata/docker-compose.yml",
			NPMHost:     "http://localhost:81",
			NPMUser:     "admin",
			NPMPass:     "",
			KumaURL:     "http://localhost:3001",
			KumaUser:    "admin",
			KumaPass:    "",
		},
	}

	r := setupRouter(app)

	req := httptest.NewRequest("GET", "/api/status", nil)
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
	tmpDB := t.TempDir() + "/test.db"
	database, err := db.Open(tmpDB)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	app := &App{database: database}
	r := setupRouter(app)

	req := httptest.NewRequest("GET", "/api/sync/history", nil)
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
	tmpDB := t.TempDir() + "/test.db"
	database, err := db.Open(tmpDB)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	app := &App{database: database}
	r := setupRouter(app)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestSettingsEndpoint(t *testing.T) {
	tmpDB := t.TempDir() + "/test.db"
	database, err := db.Open(tmpDB)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	app := &App{
		database: database,
		defaultSettings: db.Settings{
			KumaURL: "http://default-kuma:3001",
		},
	}
	r := setupRouter(app)

	// GET settings returns defaults
	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var settings map[string]any
	json.NewDecoder(w.Body).Decode(&settings)
	if settings["kuma_url"] != "http://default-kuma:3001" {
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
	tmpDB := t.TempDir() + "/test.db"
	database, err := db.Open(tmpDB)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	app := &App{
		database: database,
		defaultSettings: db.Settings{
			KumaURL: "http://default:3001",
		},
	}
	r := setupRouter(app)

	// POST new settings
	body := strings.NewReader(`{"kuma_url":"http://new-kuma:4000","npm_host":"http://npm:81"}`)
	req := httptest.NewRequest("POST", "/api/settings", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("post settings: expected 200, got %d", w.Code)
	}

	// GET should return saved values
	req2 := httptest.NewRequest("GET", "/api/settings", nil)
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
	api := r.Group("/api")
	{
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
