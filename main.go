package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"golang.org/x/crypto/bcrypt"

	"synapse/internal/authelia"
	"synapse/internal/db"
	"synapse/internal/kuma"
	"synapse/internal/logging"
	"synapse/internal/npm"
	synclib "synapse/internal/sync"
	"synapse/internal/telemetry"
	"log/slog"
)

var version = "1.9.24"

type sessionInfo struct {
	Expiry time.Time
}

var (
	sessionStore   = make(map[string]sessionInfo)
	sessionStoreMu sync.RWMutex
)

func generateSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func setSessionCookie(c *gin.Context, id string) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("session", id, 86400, "/", "", false, true)
}

func cleanupSessions() {
	sessionStoreMu.Lock()
	defer sessionStoreMu.Unlock()
	now := time.Now()
	for id, s := range sessionStore {
		if now.After(s.Expiry) {
			delete(sessionStore, id)
		}
	}
}

func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID, err := c.Cookie("session")
		if err != nil || sessionID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
			return
		}
		sessionStoreMu.Lock()
		s, ok := sessionStore[sessionID]
		if ok && time.Now().After(s.Expiry) {
			delete(sessionStore, sessionID)
			ok = false
		}
		sessionStoreMu.Unlock()
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "session expired"})
			return
		}
		c.Next()
	}
}

type App struct {
	database     *db.DB
	kumaRegistry *kuma.Registry
	npmRegistry  *npm.Registry

	mu            sync.Mutex
	running       bool
	progressChans []chan synclib.Progress
}

func (app *App) settings() db.Settings {
	return app.database.GetSettings(db.Settings{
		ComposePath:           getEnv("COMPOSE_PATH", "docker-compose.yml"),
		NPMHost:               getEnv("NPM_HOST", "http://nginx:81"),
		NPMUser:               getEnv("NPM_USER", "admin"),
		NPMPass:               getEnv("NPM_PASS", ""),
		KumaURL:               getEnv("KUMA_URL", "http://uptime-kuma:3001"),
		KumaUser:              getEnv("KUMA_USER", "admin"),
		KumaPass:              getEnv("KUMA_PASS", ""),
		AutheliaConfigPath:    getEnv("AUTHELIA_CONFIG_PATH", ""),
		AutheliaDBPath:        getEnv("AUTHELIA_DB_PATH", ""),
		AutheliaSyncEnabled:   getEnv("AUTHELIA_SYNC_ENABLED", "") == "true",
		AutheliaDefaultPolicy: getEnv("AUTHELIA_DEFAULT_POLICY", authelia.DefaultPolicy),
		OTelEndpoint:          getEnv("OTEL_ENDPOINT", ""),
		OTelEnabled:           getEnv("OTEL_ENABLED", "") == "true",
	})
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func mask(s string) string {
	if s == "" {
		return ""
	}
	return "****"
}

func main() {
	logging.Init()

	dbPath := getEnv("DB_PATH", "/db/synapse.db")
	addr := getEnv("LISTEN_ADDR", ":6270")

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Migrate legacy single-instance kuma_* settings into KumaInstance rows.
	legacySettings := db.Settings{
		KumaURL:  getEnv("KUMA_URL", "http://uptime-kuma:3001"),
		KumaUser: getEnv("KUMA_USER", "admin"),
		KumaPass: getEnv("KUMA_PASS", ""),
	}
	// Read persisted legacy settings (override env defaults if present).
	persisted := database.GetSettings(legacySettings)
	legacySettings.KumaURL = persisted.KumaURL
	legacySettings.KumaUser = persisted.KumaUser
	legacySettings.KumaPass = persisted.KumaPass
	if err := database.MigrateKumaInstances(legacySettings); err != nil {
		slog.Warn("kuma instance migration failed", "error", err)
	}
	if err := database.MigrateNPMInstances(legacySettings); err != nil {
		slog.Warn("npm instance migration failed", "error", err)
	}

	app := &App{
		database:     database,
		kumaRegistry: kuma.NewRegistry(database),
		npmRegistry:  npm.NewRegistry(database),
	}

	// Read OTel endpoint from database settings, fall back to env var
	otelSettings := app.settings()
	otelEndpoint := ""
	if otelSettings.OTelEnabled {
		otelEndpoint = otelSettings.OTelEndpoint
	}

	providers, err := telemetry.InitTelemetry(otelEndpoint)
	if err != nil {
		slog.Warn("telemetry initialization failed, continuing without tracing", "error", err)
		providers = nil
	}
	defer telemetry.Shutdown(providers)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.SetTrustedProxies(nil)

	r.Use(otelgin.Middleware("synapse"))
	r.Use(telemetry.MetricsMiddleware())

	r.LoadHTMLGlob("static/*.html")
	r.Static("/dist", "./static/dist")

	r.GET("/", app.Dashboard)
	r.GET("/setup", func(c *gin.Context) {
		count, _ := app.database.CountAdminUsers()
		if count > 0 {
			c.Redirect(http.StatusFound, "/login")
			return
		}
		c.HTML(http.StatusOK, "setup.html", nil)
	})
	r.GET("/login", func(c *gin.Context) {
		sessionID, err := c.Cookie("session")
		if err == nil && sessionID != "" {
			sessionStoreMu.RLock()
			s, ok := sessionStore[sessionID]
			sessionStoreMu.RUnlock()
			if ok && time.Now().Before(s.Expiry) {
				c.Redirect(http.StatusFound, "/")
				return
			}
		}
		count, _ := app.database.CountAdminUsers()
		if count == 0 {
			c.Redirect(http.StatusFound, "/setup")
			return
		}
		c.HTML(http.StatusOK, "login.html", nil)
	})
	r.GET("/static/*filepath", func(c *gin.Context) {
		c.File("static/" + c.Param("filepath"))
	})

	r.GET("/api/check-setup", app.HandleCheckSetup)
	r.POST("/api/login", app.HandleLogin)

	api := r.Group("/api")
	api.Use(authMiddleware())
	{
		api.POST("/logout", app.HandleLogout)
		api.GET("/settings", app.GetSettings)
		api.POST("/settings", app.SaveSettings)
		api.POST("/test/npm", app.TestNPM)
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
		api.GET("/sync/progress", app.ProgressSSE)
		api.GET("/sync/history", app.SyncHistory)
		api.GET("/services", app.Services)
		api.GET("/proxies", app.Proxies)
		api.GET("/monitors", app.KumaMonitors)
		api.GET("/monitors/:id/stats", app.KumaMonitorStats)
		api.GET("/status", app.Status)

		// Logs endpoints
		api.GET("/logs", app.LogsHandler)
		api.GET("/logs/stream", app.LogsStreamSSE)

		// Authelia endpoints
		api.GET("/authelia/status", app.AutheliaStatus)
		api.GET("/authelia/alerts", app.AutheliaAlerts)
		api.POST("/authelia/alerts/:id/resolve", app.AutheliaResolveAlert)
		api.GET("/authelia/temp-access", app.AutheliaTempAccess)
		api.POST("/authelia/temp-access", app.AutheliaAddTempAccess)
		api.POST("/authelia/temp-access/:id/revoke", app.AutheliaRevokeTempAccess)
		api.POST("/authelia/sync", app.AutheliaSync)
		api.GET("/authelia-instances", app.ListAutheliaInstances)
		api.POST("/authelia-instances", app.CreateAutheliaInstance)
		api.PUT("/authelia-instances/:id", app.UpdateAutheliaInstance)
		api.DELETE("/authelia-instances/:id", app.DeleteAutheliaInstance)
		api.POST("/authelia-instances/:id/test", app.TestAutheliaInstance)
	}

	// Start periodic sync scheduler (if SYNC_INTERVAL > 0)
	syncInterval := getEnvInt("SYNC_INTERVAL", 0)
	if syncInterval > 0 {
		go app.startSyncScheduler(ctx, syncInterval)
	}

	// Session cleanup goroutine
	go func() {
		for {
			time.Sleep(15 * time.Minute)
			cleanupSessions()
		}
	}()

	fmt.Printf("synapse v%s listening on %s\n", version, addr)

	srv := &http.Server{Addr: addr, Handler: r}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}
}

func (app *App) HandleCheckSetup(c *gin.Context) {
	count, err := app.database.CountAdminUsersCtx(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"setup": count > 0})
}

func (app *App) HandleLogin(c *gin.Context) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Setup    bool   `json:"setup"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	count, _ := app.database.CountAdminUsersCtx(c.Request.Context())

	if input.Setup && count > 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "Setup already completed"})
		return
	}

	if count == 0 {
		if len(input.Password) < 8 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be at least 8 characters"})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
			return
		}
		_, err = app.database.CreateAdminUser(input.Username, string(hash))
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
			return
		}
		sessionID := generateSessionID()
		sessionStoreMu.Lock()
		sessionStore[sessionID] = sessionInfo{Expiry: time.Now().Add(24 * time.Hour)}
		sessionStoreMu.Unlock()
		setSessionCookie(c, sessionID)
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	user, err := app.database.GetAdminUserCtx(c.Request.Context(), input.Username)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	sessionID := generateSessionID()
	sessionStoreMu.Lock()
	sessionStore[sessionID] = sessionInfo{Expiry: time.Now().Add(24 * time.Hour)}
	sessionStoreMu.Unlock()
	setSessionCookie(c, sessionID)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (app *App) HandleLogout(c *gin.Context) {
	sessionID, err := c.Cookie("session")
	if err == nil && sessionID != "" {
		sessionStoreMu.Lock()
		delete(sessionStore, sessionID)
		sessionStoreMu.Unlock()
	}
	c.SetCookie("session", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (app *App) Dashboard(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", gin.H{
		"Version": version,
	})
}

func (app *App) GetSettings(c *gin.Context) {
	s := app.settings()
	// Check if NPM/Authelia instances have been migrated
	npmInstances, _ := app.database.GetNPMInstances()
	npmMigrated := len(npmInstances) > 0
	autheliaInstances, _ := app.database.GetAutheliaInstances()
	autheliaMigrated := len(autheliaInstances) > 0
	c.JSON(http.StatusOK, gin.H{
		"compose_path":            s.ComposePath,
		"npm_host":                s.NPMHost,
		"npm_user":                s.NPMUser,
		"npm_pass":                mask(s.NPMPass),
		"npm_migrated":            npmMigrated,
		"authelia_config_path":    s.AutheliaConfigPath,
		"authelia_db_path":        s.AutheliaDBPath,
		"authelia_sync_enabled":   s.AutheliaSyncEnabled,
		"authelia_default_policy": s.AutheliaDefaultPolicy,
		"authelia_sync_overrides": s.AutheliaSyncOverrides,
		"authelia_migrated":       autheliaMigrated,
		"otel_endpoint":           s.OTelEndpoint,
		"otel_enabled":            s.OTelEnabled,
	})
}

func (app *App) SaveSettings(c *gin.Context) {
	// Parse raw JSON to detect which fields were explicitly sent
	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Only save fields that were explicitly sent in the request body
	pairs := make(map[string]string)

	// Legacy npm_host/user/pass are deprecated — log warning if sent, ignore.
	if _, ok := raw["npm_host"]; ok {
		logging.LogWarn("app", "Legacy npm_host setting is deprecated, use NPM API instead")
	}
	if _, ok := raw["npm_user"]; ok {
		logging.LogWarn("app", "Legacy npm_user setting is deprecated, use NPM API instead")
	}
	if _, ok := raw["npm_pass"]; ok {
		logging.LogWarn("app", "Legacy npm_pass setting is deprecated, use NPM API instead")
	}
	// Legacy authelia_* settings are deprecated — warn if sent, ignore.
	if _, ok := raw["authelia_config_path"]; ok {
		logging.LogWarn("app", "Legacy authelia_config_path setting is deprecated, use Authelia API instead")
	}
	if _, ok := raw["authelia_db_path"]; ok {
		logging.LogWarn("app", "Legacy authelia_db_path setting is deprecated, use Authelia API instead")
	}
	if _, ok := raw["authelia_sync_enabled"]; ok {
		logging.LogWarn("app", "Legacy authelia_sync_enabled setting is deprecated, use Authelia API instead")
	}
	if _, ok := raw["authelia_default_policy"]; ok {
		logging.LogWarn("app", "Legacy authelia_default_policy setting is deprecated, use Authelia API instead")
	}
	if _, ok := raw["authelia_sync_overrides"]; ok {
		logging.LogWarn("app", "Legacy authelia_sync_overrides setting is deprecated, use Authelia API instead")
	}
	if v, ok := raw["compose_path"]; ok {
		var val string; json.Unmarshal(v, &val)
		pairs["compose_path"] = val
	}
	if v, ok := raw["otel_endpoint"]; ok {
		var val string; json.Unmarshal(v, &val)
		pairs["otel_endpoint"] = val
	}
	if v, ok := raw["otel_enabled"]; ok {
		var val bool; json.Unmarshal(v, &val)
		pairs["otel_enabled"] = strconv.FormatBool(val)
	}

	if err := app.database.SaveSettingsMap(pairs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "saved"})
}

func (app *App) TestNPM(c *gin.Context) {
	start := time.Now()
	logging.LogDebug("app", "Testing NPM connection")

	s := app.settings()
	var input db.Settings
	if err := c.ShouldBindJSON(&input); err == nil {
		if input.NPMHost != "" {
			s.NPMHost = input.NPMHost
		}
		if input.NPMUser != "" {
			s.NPMUser = input.NPMUser
		}
		if input.NPMPass != "" && input.NPMPass != "****" {
			s.NPMPass = input.NPMPass
		}
	}
	clients, _ := app.kumaRegistry.All()
	npmClients, _ := app.npmRegistry.All()
	_, err := synclib.GetNPMProxiesWithStatus(npmClients, clients)
	if err != nil {
		logging.LogError("app", "NPM connection test failed",
			slog.String("npm_host", s.NPMHost),
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	logging.LogInfo("app", "NPM connection test successful",
		slog.String("npm_host", s.NPMHost),
		slog.Duration("duration", time.Since(start)),
	)
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "NPM connection successful"})
}

// ─── Kuma Instance Handlers ──────────────────────────────────────────────────

// kumaInstanceJSON is the JSON representation of a Kuma instance, with the
// password masked in responses.
type kumaInstanceJSON struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
}

func toKumaInstanceJSON(k *db.KumaInstance) kumaInstanceJSON {
	return kumaInstanceJSON{
		ID:        k.ID,
		Name:      k.Name,
		URL:       k.URL,
		Username:  k.Username,
		Password:  mask(k.Password),
		Enabled:   k.Enabled,
		CreatedAt: k.CreatedAt.Format(time.RFC3339),
	}
}

type npmInstanceJSON struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
}

func toNPMInstanceJSON(n *db.NPMInstance) npmInstanceJSON {
	return npmInstanceJSON{
		ID:        n.ID,
		Name:      n.Name,
		URL:       n.URL,
		Username:  n.Username,
		Password:  mask(n.Password),
		Enabled:   n.Enabled,
		CreatedAt: n.CreatedAt.Format(time.RFC3339),
	}
}

func (app *App) ListKumaInstances(c *gin.Context) {
	instances, err := app.database.GetKumaInstances()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	result := make([]kumaInstanceJSON, 0, len(instances))
	for i := range instances {
		result = append(result, toKumaInstanceJSON(&instances[i]))
	}
	c.JSON(http.StatusOK, result)
}

func (app *App) CreateKumaInstance(c *gin.Context) {
	var input struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if input.Name == "" || input.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and url are required"})
		return
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	created, err := app.database.CreateKumaInstance(&db.KumaInstance{
		Name:     input.Name,
		URL:      input.URL,
		Username: input.Username,
		Password: input.Password,
		Enabled:  enabled,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toKumaInstanceJSON(created))
}

func (app *App) UpdateKumaInstance(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var input struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	existing, err := app.database.GetKumaInstance(id)
	if err != nil || existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
		return
	}
	enabled := existing.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if err := app.database.UpdateKumaInstance(id, &db.KumaInstance{
		Name:     input.Name,
		URL:      input.URL,
		Username: input.Username,
		Password: input.Password, // empty = keep existing
		Enabled:  enabled,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	app.kumaRegistry.Invalidate(int(id))
	updated, _ := app.database.GetKumaInstance(id)
	c.JSON(http.StatusOK, toKumaInstanceJSON(updated))
}

func (app *App) DeleteKumaInstance(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	app.kumaRegistry.Invalidate(int(id))
	if err := app.database.DeleteKumaInstance(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (app *App) TestKumaInstance(c *gin.Context) {
	start := time.Now()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	inst, err := app.database.GetKumaInstance(id)
	if err != nil || inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
		return
	}
	logging.LogDebug("app", "Testing Kuma instance connection",
		slog.String("instance", inst.Name),
		slog.String("kuma_url", inst.URL),
	)
	monitors, err := kuma.QueryMonitorsViaSocketIO(inst.URL, inst.Username, inst.Password)
	if err != nil {
		logging.LogError("app", "Kuma instance connection test failed",
			slog.String("instance", inst.Name),
			slog.String("kuma_url", inst.URL),
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	logging.LogInfo("app", "Kuma instance connection test successful",
		slog.String("instance", inst.Name),
		slog.String("kuma_url", inst.URL),
		slog.Int("monitor_count", len(monitors)),
		slog.Duration("duration", time.Since(start)),
	)
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": fmt.Sprintf("Connected, %d monitors found", len(monitors))})
}

// --- NPM instance handlers ---

func (app *App) ListNPMInstances(c *gin.Context) {
	instances, err := app.database.GetNPMInstances()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	result := make([]npmInstanceJSON, 0, len(instances))
	for i := range instances {
		result = append(result, toNPMInstanceJSON(&instances[i]))
	}
	c.JSON(http.StatusOK, result)
}

func (app *App) CreateNPMInstance(c *gin.Context) {
	var input struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if input.Name == "" || input.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and url are required"})
		return
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	created, err := app.database.CreateNPMInstance(&db.NPMInstance{
		Name:     input.Name,
		URL:      input.URL,
		Username: input.Username,
		Password: input.Password,
		Enabled:  enabled,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toNPMInstanceJSON(created))
}

func (app *App) UpdateNPMInstance(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var input struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	existing, err := app.database.GetNPMInstance(id)
	if err != nil || existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
		return
	}
	enabled := existing.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if err := app.database.UpdateNPMInstance(id, &db.NPMInstance{
		Name:     input.Name,
		URL:      input.URL,
		Username: input.Username,
		Password: input.Password,
		Enabled:  enabled,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	app.npmRegistry.Invalidate(int(id))
	updated, _ := app.database.GetNPMInstance(id)
	c.JSON(http.StatusOK, toNPMInstanceJSON(updated))
}

func (app *App) DeleteNPMInstance(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	app.npmRegistry.Invalidate(int(id))
	if err := app.database.DeleteNPMInstance(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (app *App) TestNPMInstance(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	inst, err := app.database.GetNPMInstance(id)
	if err != nil || inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "message": "instance not found"})
		return
	}
	client := npm.NewClient(inst.URL, inst.Username, inst.Password)
	proxies, err := client.GetProxyHosts()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": fmt.Sprintf("Connected, %d proxy hosts found", len(proxies))})
}

// --- Authelia instance handlers ---

type autheliaInstanceJSON struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	ConfigPath     string `json:"config_path"`
	DBPath         string `json:"db_path"`
	DefaultPolicy  string `json:"default_policy"`
	Overrides      string `json:"overrides"`
	AutoSync       bool   `json:"auto_sync"`
	Enabled        bool   `json:"enabled"`
	NPMInstanceIDs string `json:"npm_instance_ids"`
	CreatedAt      string `json:"created_at"`
}

func toAutheliaInstanceJSON(a *db.AutheliaInstance) autheliaInstanceJSON {
	return autheliaInstanceJSON{
		ID:             a.ID,
		Name:           a.Name,
		ConfigPath:     a.ConfigPath,
		DBPath:         a.DBPath,
		DefaultPolicy:  a.DefaultPolicy,
		Overrides:      a.Overrides,
		AutoSync:       a.AutoSync,
		Enabled:        a.Enabled,
		NPMInstanceIDs: a.NPMInstanceIDs,
		CreatedAt:      a.CreatedAt.Format(time.RFC3339),
	}
}

func (app *App) ListAutheliaInstances(c *gin.Context) {
	instances, err := app.database.GetAutheliaInstances()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	result := make([]autheliaInstanceJSON, 0, len(instances))
	for i := range instances {
		result = append(result, toAutheliaInstanceJSON(&instances[i]))
	}
	c.JSON(http.StatusOK, result)
}

func (app *App) CreateAutheliaInstance(c *gin.Context) {
	var input struct {
		Name           string `json:"name"`
		ConfigPath     string `json:"config_path"`
		DBPath         string `json:"db_path"`
		DefaultPolicy  string `json:"default_policy"`
		Overrides      string `json:"overrides"`
		AutoSync       bool   `json:"auto_sync"`
		Enabled        *bool  `json:"enabled"`
		NPMInstanceIDs string `json:"npm_instance_ids"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if input.Name == "" || input.ConfigPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and config_path are required"})
		return
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if input.DefaultPolicy == "" {
		input.DefaultPolicy = "one_factor"
	}
	if input.Overrides == "" {
		input.Overrides = "{}"
	}
	if input.NPMInstanceIDs == "" {
		input.NPMInstanceIDs = "[]"
	}
	created, err := app.database.CreateAutheliaInstance(&db.AutheliaInstance{
		Name:           input.Name,
		ConfigPath:     input.ConfigPath,
		DBPath:         input.DBPath,
		DefaultPolicy:  input.DefaultPolicy,
		Overrides:      input.Overrides,
		AutoSync:       input.AutoSync,
		Enabled:        enabled,
		NPMInstanceIDs: input.NPMInstanceIDs,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toAutheliaInstanceJSON(created))
}

func (app *App) UpdateAutheliaInstance(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var input struct {
		Name           string `json:"name"`
		ConfigPath     string `json:"config_path"`
		DBPath         string `json:"db_path"`
		DefaultPolicy  string `json:"default_policy"`
		Overrides      string `json:"overrides"`
		AutoSync       *bool  `json:"auto_sync"`
		Enabled        *bool  `json:"enabled"`
		NPMInstanceIDs string `json:"npm_instance_ids"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	existing, err := app.database.GetAutheliaInstance(id)
	if err != nil || existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
		return
	}
	// Preserve existing fields if not sent
	name := input.Name
	if name == "" {
		name = existing.Name
	}
	configPath := input.ConfigPath
	if configPath == "" {
		configPath = existing.ConfigPath
	}
	dbPath := input.DBPath
	if dbPath == "" {
		dbPath = existing.DBPath
	}
	defaultPolicy := input.DefaultPolicy
	if defaultPolicy == "" {
		defaultPolicy = existing.DefaultPolicy
	}
	overrides := input.Overrides
	if overrides == "" {
		overrides = existing.Overrides
	}
	npmInstanceIDs := input.NPMInstanceIDs
	if npmInstanceIDs == "" {
		npmInstanceIDs = existing.NPMInstanceIDs
	}
	autoSync := existing.AutoSync
	if input.AutoSync != nil {
		autoSync = *input.AutoSync
	}
	enabled := existing.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if err := app.database.UpdateAutheliaInstance(id, &db.AutheliaInstance{
		Name:           name,
		ConfigPath:     configPath,
		DBPath:         dbPath,
		DefaultPolicy:  defaultPolicy,
		Overrides:      overrides,
		AutoSync:       autoSync,
		Enabled:        enabled,
		NPMInstanceIDs: npmInstanceIDs,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	updated, _ := app.database.GetAutheliaInstance(id)
	c.JSON(http.StatusOK, toAutheliaInstanceJSON(updated))
}

func (app *App) DeleteAutheliaInstance(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := app.database.DeleteAutheliaInstance(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (app *App) TestAutheliaInstance(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	inst, err := app.database.GetAutheliaInstance(id)
	if err != nil || inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "message": "instance not found"})
		return
	}
	// Test config file readability
	if _, err := os.ReadFile(inst.ConfigPath); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "config file not readable: " + err.Error()})
		return
	}
	// Test config file parse
	ac, err := authelia.ParseConfig(inst.ConfigPath)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": "config parse error: " + err.Error()})
		return
	}
	// Test DB path accessibility
	if inst.DBPath != "" {
		if _, err := os.Stat(inst.DBPath); err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "message": "db path not accessible: " + err.Error()})
			return
		}
	}
	domains := authelia.GetDomains(ac)
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": fmt.Sprintf("Config readable, %d domains found", len(domains))})
}

func (app *App) Status(c *gin.Context) {
	s := app.settings()

	// Docker health
	var dockerErr string
	services, err := synclib.LoadServices(s.ComposePath)
	dockerCount := 0
	if err == nil {
		dockerCount = len(services)
	} else {
		dockerErr = err.Error()
	}

	// Kuma clients (used for NPM status + kuma health)
	clients, _ := app.kumaRegistry.All()

	// NPM health
	npmClients, _ := app.npmRegistry.All()
	npmCount := 0
	npmErr := ""
	npmProxies, npmFetchErr := synclib.GetNPMProxiesWithStatus(npmClients, clients)
	if npmFetchErr == nil {
		npmCount = len(npmProxies)
	} else {
		npmErr = npmFetchErr.Error()
	}

	// Kuma health — check each enabled instance. Health is ok only if ALL
	// enabled instances are reachable.
	kumaErr := ""
	kumaInstances, _ := app.database.GetEnabledKumaInstances()
	kumaHealthList := make([]gin.H, 0, len(kumaInstances))
	for _, inst := range kumaInstances {
		instErr := ""
		if _, err := app.kumaRegistry.Get(int(inst.ID)); err != nil {
			instErr = err.Error()
		}
		kumaHealthList = append(kumaHealthList, gin.H{
			"id":         inst.ID,
			"name":       inst.Name,
			"ok":         instErr == "",
			"last_error": instErr,
		})
		if instErr != "" {
			if kumaErr == "" {
				kumaErr = instErr
			} else {
				kumaErr = inst.Name + ": " + instErr
			}
		}
	}
	if len(kumaInstances) == 0 {
		kumaErr = "no Kuma instances configured"
	}

	monitorCount, _ := app.database.GetMonitorCount()

	lastDocker, _ := app.database.GetLatestSyncRun("docker")
	lastNPM, _ := app.database.GetLatestSyncRun("npm")

	// NPM per-instance health
	npmInstances, _ := app.database.GetEnabledNPMInstances()
	npmHealthList := make([]gin.H, 0, len(npmInstances))
	for _, inst := range npmInstances {
		c := npm.NewClient(inst.URL, inst.Username, inst.Password)
		_, err := c.GetProxyHosts()
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		npmHealthList = append(npmHealthList, gin.H{
			"id":         inst.ID,
			"name":       inst.Name,
			"ok":         err == nil,
			"last_error": errMsg,
		})
	}

	connectionHealth := gin.H{
		"docker": gin.H{
			"ok":         dockerErr == "",
			"last_error": dockerErr,
		},
		"npm": gin.H{
			"ok":         npmErr == "",
			"last_error": npmErr,
			"instances":  npmHealthList,
		},
		"kuma": gin.H{
			"ok":         kumaErr == "",
			"last_error": kumaErr,
			"instances":  kumaHealthList,
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"docker_count":       dockerCount,
		"npm_count":          npmCount,
		"npm_error":          npmErr,
		"kuma_error":         kumaErr,
		"docker_error":       dockerErr,
		"monitor_count":      monitorCount,
		"last_docker":        lastDocker,
		"last_npm":           lastNPM,
		"running":            app.running,
		"connection_health":  connectionHealth,
	})
}

func (app *App) Services(c *gin.Context) {
	s := app.settings()
	clients, _ := app.kumaRegistry.All()
	result, err := synclib.GetDockerServicesWithStatus(s.ComposePath, clients)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if result == nil {
		result = []synclib.ServiceInfo{}
	}
	c.JSON(http.StatusOK, result)
}

func (app *App) Proxies(c *gin.Context) {
	clients, _ := app.kumaRegistry.All()
	npmClients, _ := app.npmRegistry.All()
	result, err := synclib.GetNPMProxiesWithStatus(npmClients, clients)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if result == nil {
		result = []synclib.ProxyInfo{}
	}
	c.JSON(http.StatusOK, result)
}

type KumaMonitorSummary struct {
	ID              int     `json:"id"`
	Name            string  `json:"name"`
	Type            string  `json:"type"`
	URL             string  `json:"url,omitempty"`
	DockerContainer string  `json:"docker_container,omitempty"`
	Status          int     `json:"status,omitempty"`
	Uptime24h       float64 `json:"uptime_24h,omitempty"`
	Uptime7d        float64 `json:"uptime_7d,omitempty"`
	Uptime1y        float64 `json:"uptime_1y,omitempty"`
	AvgPing         float64 `json:"ping,omitempty"`
	LastMsg         string  `json:"last_msg,omitempty"`
	InstanceID      int     `json:"instance_id"`
	InstanceName    string  `json:"instance_name"`
}

func (app *App) KumaMonitors(c *gin.Context) {
	clients, err := app.kumaRegistry.All()
	if err != nil {
		logging.LogWarn("app", "KumaRegistry returned partial results",
			slog.String("error", err.Error()),
		)
	}

	// Build instance name map from DB (lightweight — ent caches internally).
	instances, _ := app.database.GetKumaInstances()
	nameMap := make(map[int]string)
	for _, inst := range instances {
		nameMap[int(inst.ID)] = inst.Name
	}

	result := make([]KumaMonitorSummary, 0, len(clients)*10)
	for _, ic := range clients {
		monitors, err := ic.Client.GetMonitors()
		if err != nil {
			logging.LogWarn("app", "Failed to fetch monitors from Kuma instance",
				slog.Int("instance_id", ic.InstanceID),
				slog.String("error", err.Error()),
			)
			continue
		}
		instanceName, _ := nameMap[ic.InstanceID]
		for _, m := range monitors {
			result = append(result, KumaMonitorSummary{
				ID:              m.ID,
				Name:            m.Name,
				Type:            m.Type,
				URL:             m.URL,
				DockerContainer: m.DockerContainer,
				// Status/uptime/ping require Socket.IO detail stats.
				// The summary endpoint uses REST for speed.
				InstanceID:   ic.InstanceID,
				InstanceName: instanceName,
			})
		}
	}

	if result == nil {
		result = []KumaMonitorSummary{}
	}

	c.JSON(http.StatusOK, result)
}

func (app *App) KumaMonitorStats(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid monitor id"})
		return
	}
	instanceID, err := strconv.Atoi(c.Query("instance"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or missing instance query param"})
		return
	}

	client, err := app.kumaRegistry.Get(instanceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("instance %d not found or unreachable", instanceID)})
		return
	}

	stats, err := kuma.GetMonitorStats(client, id)
	if err != nil {
		logging.LogWarn("app", "Socket.IO monitor stats failed",
			slog.Int("monitor_id", id),
			slog.Int("instance_id", instanceID),
			slog.String("error", err.Error()),
		)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

func (app *App) SyncHistory(c *gin.Context) {
	source := c.Query("source")
	runs, err := app.database.GetSyncRuns(100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if runs == nil {
		runs = []db.SyncRun{}
	}

	if source != "" {
		filtered := []db.SyncRun{}
		for _, r := range runs {
			if r.Source == source {
				filtered = append(filtered, r)
			}
		}
		runs = filtered
	}

	c.JSON(http.StatusOK, runs)
}

func (app *App) DockerSync(c *gin.Context) {
	s := app.settings()

	app.mu.Lock()
	if app.running {
		app.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "sync already running"})
		return
	}
	app.running = true
	app.mu.Unlock()

	go func() {
		defer func() {
			app.mu.Lock()
			app.running = false
			app.progressChans = nil
			app.mu.Unlock()
		}()

		clients, _ := app.kumaRegistry.All()
		synclib.RunDockerSync(s.ComposePath, clients, app.database, func(p synclib.Progress) {
			app.mu.Lock()
			for _, ch := range app.progressChans {
				select {
				case ch <- p:
				default:
				}
			}
			app.mu.Unlock()
		})
	}()

	c.JSON(http.StatusAccepted, gin.H{"status": "started", "source": "docker"})
}

func (app *App) NPMSync(c *gin.Context) {
	app.mu.Lock()
	if app.running {
		app.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "sync already running"})
		return
	}
	app.running = true
	app.mu.Unlock()

	go func() {
		defer func() {
			app.mu.Lock()
			app.running = false
			app.progressChans = nil
			app.mu.Unlock()
		}()

		clients, _ := app.kumaRegistry.All()
		npmClients, _ := app.npmRegistry.All()
		synclib.RunNPMSync(npmClients, clients, app.database, func(p synclib.Progress) {
			app.mu.Lock()
			for _, ch := range app.progressChans {
				select {
				case ch <- p:
				default:
				}
			}
			app.mu.Unlock()
		})
	}()

	c.JSON(http.StatusAccepted, gin.H{"status": "started", "source": "npm"})
}

// startSyncScheduler runs periodic docker and NPM syncs on a configurable interval.
// It is intended to run as a background goroutine and respects context cancellation.
func (app *App) startSyncScheduler(ctx context.Context, intervalMinutes int) {
	logging.LogInfo("app", "Sync scheduler started",
		slog.Int("interval_minutes", intervalMinutes),
	)
	ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logging.LogInfo("app", "Sync scheduler stopped")
			return
		case <-ticker.C:
			logging.LogDebug("app", "Scheduler tick",
				slog.Int("interval_minutes", intervalMinutes),
			)
			app.runScheduledSync()
		}
	}
}

// runScheduledSync executes both docker and NPM syncs sequentially with proper locking.
func (app *App) runScheduledSync() {
	app.mu.Lock()
	if app.running {
		app.mu.Unlock()
		logging.LogDebug("app", "Scheduled sync skipped — already running")
		return
	}
	app.running = true
	app.mu.Unlock()

	defer func() {
		app.mu.Lock()
		app.running = false
		app.mu.Unlock()
	}()

	s := app.settings()
	clients, _ := app.kumaRegistry.All()
	logging.LogInfo("app", "Starting scheduled Docker sync",
		slog.String("compose_path", s.ComposePath),
		slog.Int("kuma_instances", len(clients)),
	)

	synclib.RunDockerSync(s.ComposePath, clients, app.database, func(p synclib.Progress) {
		logging.LogDebug("app", "Docker sync progress",
			slog.Int("current", p.Current),
			slog.Int("total", p.Total),
			slog.String("status", p.Status),
			slog.String("message", p.Message),
		)
	})

	log.Println("scheduler: starting periodic npm sync")

	npmClients, _ := app.npmRegistry.All()
	synclib.RunNPMSync(npmClients, clients, app.database, func(p synclib.Progress) {
		log.Printf("[scheduler] npm sync: [%d/%d] %s - %s", p.Current, p.Total, p.Status, p.Message)
	})

	log.Println("scheduler: periodic sync complete")
}

func (app *App) ProgressSSE(c *gin.Context) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "streaming unsupported")
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ch := make(chan synclib.Progress, 16)

	app.mu.Lock()
	app.progressChans = append(app.progressChans, ch)
	app.mu.Unlock()

	defer func() {
		app.mu.Lock()
		for i, c_ := range app.progressChans {
			if c_ == ch {
				app.progressChans = append(app.progressChans[:i], app.progressChans[i+1:]...)
				break
			}
		}
		app.mu.Unlock()
	}()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Fprintf(c.Writer, ": keepalive\n\n")
			flusher.Flush()
		case p, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(p)
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			flusher.Flush()
			if strings.Contains(p.Status, "completed") || p.Status == "error" {
				ticker.Stop()
			}
		}
	}
}

// ─── Logs API ────────────────────────────────────────────────────────────────

// LogsHandler returns filtered log entries from the in-memory buffer.
// Supports query params: level, source, search, limit, offset.
func (app *App) LogsHandler(c *gin.Context) {
	level := c.Query("level")
	source := c.Query("source")
	search := c.Query("search")
	errorKind := c.Query("error_kind")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	entries := logging.DefaultBuffer().Filter(logging.FilterParams{
		Level:     level,
		Source:    source,
		Search:    search,
		ErrorKind: errorKind,
		Limit:     limit,
		Offset:    offset,
	})

	if entries == nil {
		entries = []logging.Entry{}
	}

	c.JSON(http.StatusOK, entries)
}

// LogsStreamSSE streams new log entries in real-time via SSE.
func (app *App) LogsStreamSSE(c *gin.Context) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "streaming unsupported")
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ch := logging.Subscribe()
	defer logging.Unsubscribe(ch)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Fprintf(c.Writer, ": keepalive\n\n")
			flusher.Flush()
		case entry, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(entry)
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// ─── Authelia Handlers ──────────────────────────────────────────────────────

// resolveNPMEntries fetches NPM proxy entries for the given npm_instance_ids JSON array.
// If the array is empty or "[]", it fetches from all NPM instances.
func (app *App) resolveNPMEntries(npmInstanceIDs string) []npm.ProxyEntry {
	var ids []int
	json.Unmarshal([]byte(npmInstanceIDs), &ids)

	var allEntries []npm.ProxyEntry
	if len(ids) == 0 {
		// Use all NPM instances
		clients, err := app.npmRegistry.All()
		if err != nil {
			return allEntries
		}
		for _, c := range clients {
			entries, err := c.Client.GetProxyHosts()
			if err != nil {
				continue
			}
			allEntries = append(allEntries, entries...)
		}
		return allEntries
	}
	// Use specific NPM instances
	for _, id := range ids {
		client, err := app.npmRegistry.Get(id)
		if err != nil {
			continue
		}
		entries, err := client.GetProxyHosts()
		if err != nil {
			continue
		}
		allEntries = append(allEntries, entries...)
	}
	return allEntries
}

// AutheliaStatus returns the current state of Authelia integration.
// Accepts optional ?instance_id=:id to scope to a single instance.
func (app *App) AutheliaStatus(c *gin.Context) {
	instanceID, _ := strconv.ParseInt(c.Query("instance_id"), 10, 64)

	if instanceID > 0 {
		// Scoped status for a specific instance
		inst, err := app.database.GetAutheliaInstance(instanceID)
		if err != nil || inst == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "authelia instance not found"})
			return
		}
		app.buildAutheliaInstanceStatus(c, inst)
		return
	}

	// Aggregate status across all enabled instances
	s := app.settings()
	if s.AutheliaConfigPath == "" {
		// Check if there are any authelia instances configured
		instances, err := app.database.GetEnabledAutheliaInstances()
		if err != nil || len(instances) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"configured": false,
				"message":    "No Authelia instances configured",
			})
			return
		}
		// Return aggregate: domains across all instances
		var allDomains []string
		var allNPMLists []string
		for _, inst := range instances {
			ac, err := authelia.ParseConfig(inst.ConfigPath)
			if err != nil {
				continue
			}
			allDomains = append(allDomains, authelia.GetDomains(ac)...)
			npmEntries := app.resolveNPMEntries(inst.NPMInstanceIDs)
			for _, e := range npmEntries {
				allNPMLists = append(allNPMLists, e.CNAME)
			}
		}
		matched, missing := authelia.CompareCNAMEs(allNPMLists, allDomains)
		c.JSON(http.StatusOK, gin.H{
			"configured":     true,
			"domains":        allDomains,
			"npm_cnames":     allNPMLists,
			"matched":        matched,
			"missing":        missing,
			"instance_count": len(instances),
		})
		return
	}

	// Legacy fallback (single instance from settings)
	ac, err := authelia.ParseConfig(s.AutheliaConfigPath)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"configured": true,
			"error":      err.Error(),
			"domains":    []string{},
		})
		return
	}

	autheliaDomains := authelia.GetDomains(ac)

	var npmCNAMEs []string
	clients, _ := app.kumaRegistry.All()
	npmClients, _ := app.npmRegistry.All()
	proxies, npmErr := synclib.GetNPMProxiesWithStatus(npmClients, clients)
	if npmErr == nil {
		for _, p := range proxies {
			npmCNAMEs = append(npmCNAMEs, p.CNAME)
		}
	}

	matched, missing := authelia.CompareCNAMEs(npmCNAMEs, autheliaDomains)

	openAlerts := 0
	if alerts, err := app.database.GetOpenAutheliaAlerts(0); err == nil {
		openAlerts = len(alerts)
	}

	c.JSON(http.StatusOK, gin.H{
		"configured":     true,
		"domains":        autheliaDomains,
		"npm_cnames":     npmCNAMEs,
		"matched":        matched,
		"missing":        missing,
		"open_alerts":    openAlerts,
		"sync_enabled":   s.AutheliaSyncEnabled,
		"default_policy": s.AutheliaDefaultPolicy,
		"npm_error":      npmErr != nil,
		"npm_error_msg": func() string {
			if npmErr != nil {
				return npmErr.Error()
			}
			return ""
		}(),
	})
}

// buildAutheliaInstanceStatus builds a status response for a single Authelia instance.
func (app *App) buildAutheliaInstanceStatus(c *gin.Context, inst *db.AutheliaInstance) {
	ac, err := authelia.ParseConfig(inst.ConfigPath)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"configured": true,
			"instance_id":   inst.ID,
			"instance_name": inst.Name,
			"error":         err.Error(),
			"domains":       []string{},
		})
		return
	}

	autheliaDomains := authelia.GetDomains(ac)
	npmEntries := app.resolveNPMEntries(inst.NPMInstanceIDs)
	var npmCNAMEs []string
	for _, e := range npmEntries {
		npmCNAMEs = append(npmCNAMEs, e.CNAME)
	}

	matched, missing := authelia.CompareCNAMEs(npmCNAMEs, autheliaDomains)

	openAlerts := 0
	if alerts, err := app.database.GetOpenAutheliaAlerts(inst.ID); err == nil {
		openAlerts = len(alerts)
	}

	c.JSON(http.StatusOK, gin.H{
		"configured":     true,
		"instance_id":    inst.ID,
		"instance_name":  inst.Name,
		"domains":        autheliaDomains,
		"npm_cnames":     npmCNAMEs,
		"matched":        matched,
		"missing":        missing,
		"open_alerts":    openAlerts,
		"sync_enabled":   inst.AutoSync,
		"default_policy": inst.DefaultPolicy,
	})
}

// AutheliaAlerts returns all authelia alerts.
func (app *App) AutheliaAlerts(c *gin.Context) {
	logging.LogDebug("authelia", "Alerts requested")
	instanceID, _ := strconv.ParseInt(c.Query("instance_id"), 10, 64)
	alerts, err := app.database.GetAutheliaAlerts(instanceID)
	if err != nil {
		logging.LogError("authelia", "Failed to fetch alerts",
			slog.String("error", err.Error()),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if alerts == nil {
		alerts = []db.AutheliaAlert{}
	}
	logging.LogInfo("authelia", "Alerts fetched",
		slog.Int("alert_count", len(alerts)),
	)
	c.JSON(http.StatusOK, alerts)
}

// AutheliaResolveAlert marks an authelia alert as resolved.
func (app *App) AutheliaResolveAlert(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		logging.LogError("authelia", "Invalid alert ID",
			slog.String("id_str", idStr),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	logging.LogInfo("authelia", "Resolving alert",
		slog.Int64("alert_id", id),
	)
	if err := app.database.ResolveAutheliaAlert(id); err != nil {
		logging.LogError("authelia", "Failed to resolve alert",
			slog.Int64("alert_id", id),
			slog.String("error", err.Error()),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logging.LogInfo("authelia", "Alert resolved",
		slog.Int64("alert_id", id),
	)
	c.JSON(http.StatusOK, gin.H{"status": "resolved"})
}

// AutheliaTempAccess returns all temporary IP access rules.
func (app *App) AutheliaTempAccess(c *gin.Context) {
	logging.LogDebug("authelia", "Temp access rules requested")
	// Clean up expired rules first
	logging.LogDebug("authelia", "Cleaning up expired temp access rules")
	if err := app.database.CleanupExpiredTempAccess(); err != nil {
		logging.LogError("authelia", "Failed to clean up expired temp access",
			slog.String("error", err.Error()),
		)
	}

	instanceID, _ := strconv.ParseInt(c.Query("instance_id"), 10, 64)
	rules, err := app.database.GetTempAccessRules(instanceID)
	if err != nil {
		logging.LogError("authelia", "Failed to fetch temp access rules",
			slog.String("error", err.Error()),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rules == nil {
		rules = []db.TempAccess{}
	}
	c.JSON(http.StatusOK, rules)
}

// AutheliaAddTempAccess creates a new temporary IP access rule.
func (app *App) AutheliaAddTempAccess(c *gin.Context) {
	var input struct {
		IP        string `json:"ip"`
		Reason    string `json:"reason"`
		Duration  string `json:"duration"`   // Go duration string (e.g., "24h", "7d")
		ExpiresAt string `json:"expires_at"` // RFC3339 timestamp (alternative to duration)
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		logging.LogError("authelia", "Invalid temp access request",
			slog.String("error", err.Error()),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if input.IP == "" {
		logging.LogError("authelia", "Temp access missing IP")
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip is required"})
		return
	}

	var expiresAt time.Time

	if input.ExpiresAt != "" {
		var err error
		expiresAt, err = time.Parse(time.RFC3339, input.ExpiresAt)
		if err != nil {
			logging.LogError("authelia", "Invalid expires_at format",
				slog.String("expires_at", input.ExpiresAt),
			)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid expires_at format, use RFC3339"})
			return
		}
	} else if input.Duration != "" {
		d, err := time.ParseDuration(input.Duration)
		if err != nil {
			logging.LogError("authelia", "Invalid duration format",
				slog.String("duration", input.Duration),
			)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid duration: " + err.Error()})
			return
		}
		expiresAt = time.Now().Add(d)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "duration or expires_at is required"})
		return
	}

	rule := &db.TempAccess{
		IP:        input.IP,
		Reason:    input.Reason,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}

	if err := app.database.AddTempAccess(rule); err != nil {
		logging.LogError("authelia", "Failed to create temp access",
			slog.String("ip", input.IP),
			slog.String("error", err.Error()),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logging.LogInfo("authelia", "Temp access created",
		slog.String("ip", input.IP),
		slog.String("reason", input.Reason),
		slog.Time("expires_at", expiresAt),
	)
	c.JSON(http.StatusOK, gin.H{"status": "created"})
}

// AutheliaRevokeTempAccess revokes a temporary IP access rule.
func (app *App) AutheliaRevokeTempAccess(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		logging.LogError("authelia", "Invalid temp access ID",
			slog.String("id_str", idStr),
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	logging.LogInfo("authelia", "Revoking temp access",
		slog.Int64("rule_id", id),
	)
	if err := app.database.RevokeTempAccess(id); err != nil {
		logging.LogError("authelia", "Failed to revoke temp access",
			slog.Int64("rule_id", id),
			slog.String("error", err.Error()),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logging.LogInfo("authelia", "Temp access revoked",
		slog.Int64("rule_id", id),
	)
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

// AutheliaSync runs an Authelia sync operation (dry-run or actual).
// Accepts optional ?instance_id=:id to scope to a single instance.
// Request body can include:
//   - dry_run: bool (default: true for safety)
func (app *App) AutheliaSync(c *gin.Context) {
	start := time.Now()
	instanceID, _ := strconv.ParseInt(c.Query("instance_id"), 10, 64)

	var input struct {
		DryRun *bool `json:"dry_run"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		// Use defaults on parse error
	}
	dryRun := true
	if input.DryRun != nil {
		dryRun = *input.DryRun
	}

	if instanceID > 0 {
		// Sync a single instance
		inst, err := app.database.GetAutheliaInstance(instanceID)
		if err != nil || inst == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "authelia instance not found"})
			return
		}
		app.syncAutheliaInstance(c, inst, dryRun, start)
		return
	}

	// Sync all enabled instances (or fall back to legacy settings)
	s := app.settings()
	instances, err := app.database.GetEnabledAutheliaInstances()
	if err == nil && len(instances) > 0 {
		// Use new multi-instance path
		var allResults []gin.H
		for _, inst := range instances {
			result := app.syncAutheliaInstanceInternal(&inst, dryRun)
			allResults = append(allResults, result)
		}
		logging.LogInfo("authelia", "Authelia sync complete",
			slog.Int("instance_count", len(allResults)),
			slog.Duration("duration", time.Since(start)),
		)
		c.JSON(http.StatusOK, gin.H{
			"dry_run":   dryRun,
			"instances": allResults,
			"message":   fmt.Sprintf("Synced %d Authelia instance(s)", len(instances)),
		})
		return
	}

	// Legacy fallback (single instance from settings)
	if s.AutheliaConfigPath == "" {
		logging.LogError("authelia", "Authelia sync failed — no instances configured")
		c.JSON(http.StatusBadRequest, gin.H{"error": "No Authelia instances configured"})
		return
	}

	npmClients, _ := app.npmRegistry.All()
	npmEntries, err := synclib.GetNPMProxyEntries(npmClients)
	if err != nil {
		logging.LogError("authelia", "Failed to fetch NPM entries for Authelia sync",
			slog.String("error", err.Error()),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch NPM entries: " + err.Error()})
		return
	}

	var overrides map[string]string
	if s.AutheliaSyncOverrides != "" {
		json.Unmarshal([]byte(s.AutheliaSyncOverrides), &overrides)
	}

	actions, err := authelia.SyncConfig(s.AutheliaConfigPath, s.AutheliaDBPath, npmEntries, s.AutheliaDefaultPolicy, overrides, s.AutheliaSyncEnabled, dryRun)
	if err != nil {
		logging.LogError("authelia", "Authelia sync config failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "actions": actions})
		return
	}

	if !dryRun && s.AutheliaSyncEnabled {
		for _, a := range actions {
			if a.Action == "add" {
				app.database.AddAutheliaAlert(&db.AutheliaAlert{
					CNAME:     a.CNAME,
					Message:   a.Message,
					Severity:  "info",
					Status:    "resolved",
					CreatedAt: time.Now(),
				})
			}
		}
	}

	if !s.AutheliaSyncEnabled {
		for _, a := range actions {
			if a.Action == "alert" {
				app.database.AddAutheliaAlert(&db.AutheliaAlert{
					CNAME:     a.CNAME,
					Message:   a.Message,
					Severity:  "warning",
					CreatedAt: time.Now(),
				})
			}
		}
	}

	added, skipped, alerted := countActions(actions)
	logging.LogInfo("authelia", "Authelia sync complete",
		slog.Int("actions", len(actions)),
		slog.Int("added", added),
		slog.Int("skipped", skipped),
		slog.Int("alerted", alerted),
		slog.Bool("dry_run", dryRun),
		slog.Duration("duration", time.Since(start)),
	)
	c.JSON(http.StatusOK, gin.H{
		"dry_run": dryRun, "actions": actions,
		"added": added, "skipped": skipped, "alerted": alerted,
	})
}

// syncAutheliaInstance syncs a single Authelia instance and returns the HTTP response.
func (app *App) syncAutheliaInstance(c *gin.Context, inst *db.AutheliaInstance, dryRun bool, start time.Time) {
	npmEntries := app.resolveNPMEntries(inst.NPMInstanceIDs)

	var overrides map[string]string
	if inst.Overrides != "" && inst.Overrides != "{}" {
		json.Unmarshal([]byte(inst.Overrides), &overrides)
	}

	actions, err := authelia.SyncConfig(inst.ConfigPath, inst.DBPath, npmEntries, inst.DefaultPolicy, overrides, inst.AutoSync, dryRun)
	if err != nil {
		logging.LogError("authelia", "Authelia sync config failed",
			slog.Int64("instance_id", inst.ID),
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "actions": actions, "instance_id": inst.ID})
		return
	}

	// Create alerts for this instance
	if !dryRun && inst.AutoSync {
		for _, a := range actions {
			if a.Action == "add" {
				app.database.AddAutheliaAlert(&db.AutheliaAlert{
					CNAME:              a.CNAME,
					Message:            a.Message,
					Severity:           "info",
					Status:             "resolved",
					CreatedAt:          time.Now(),
					AutheliaInstanceID: inst.ID,
				})
			}
		}
	}

	if !inst.AutoSync {
		for _, a := range actions {
			if a.Action == "alert" {
				app.database.AddAutheliaAlert(&db.AutheliaAlert{
					CNAME:              a.CNAME,
					Message:            a.Message,
					Severity:           "warning",
					CreatedAt:          time.Now(),
					AutheliaInstanceID: inst.ID,
				})
			}
		}
	}

	added, skipped, alerted := countActions(actions)
	logging.LogInfo("authelia", "Authelia sync instance complete",
		slog.Int64("instance_id", inst.ID),
		slog.Int("added", added),
		slog.Int("skipped", skipped),
		slog.Int("alerted", alerted),
		slog.Duration("duration", time.Since(start)),
	)
	c.JSON(http.StatusOK, gin.H{
		"dry_run": dryRun, "actions": actions,
		"added": added, "skipped": skipped, "alerted": alerted,
		"instance_id": inst.ID, "instance_name": inst.Name,
	})
}

// syncAutheliaInstanceInternal runs a sync for a single instance without writing an HTTP response.
func (app *App) syncAutheliaInstanceInternal(inst *db.AutheliaInstance, dryRun bool) gin.H {
	npmEntries := app.resolveNPMEntries(inst.NPMInstanceIDs)

	var overrides map[string]string
	if inst.Overrides != "" && inst.Overrides != "{}" {
		json.Unmarshal([]byte(inst.Overrides), &overrides)
	}

	actions, err := authelia.SyncConfig(inst.ConfigPath, inst.DBPath, npmEntries, inst.DefaultPolicy, overrides, inst.AutoSync, dryRun)
	if err != nil {
		return gin.H{"instance_id": inst.ID, "instance_name": inst.Name, "error": err.Error()}
	}

	if !dryRun && inst.AutoSync {
		for _, a := range actions {
			if a.Action == "add" {
				app.database.AddAutheliaAlert(&db.AutheliaAlert{
					CNAME:              a.CNAME,
					Message:            a.Message,
					Severity:           "info",
					Status:             "resolved",
					CreatedAt:          time.Now(),
					AutheliaInstanceID: inst.ID,
				})
			}
		}
	}

	if !inst.AutoSync {
		for _, a := range actions {
			if a.Action == "alert" {
				app.database.AddAutheliaAlert(&db.AutheliaAlert{
					CNAME:              a.CNAME,
					Message:            a.Message,
					Severity:           "warning",
					CreatedAt:          time.Now(),
					AutheliaInstanceID: inst.ID,
				})
			}
		}
	}

	added, skipped, alerted := countActions(actions)
	return gin.H{
		"instance_id": inst.ID, "instance_name": inst.Name,
		"actions": actions, "added": added, "skipped": skipped, "alerted": alerted,
	}
}

// countActions tallies add/skip/alert actions.
func countActions(actions []authelia.SyncAction) (added, skipped, alerted int) {
	for _, a := range actions {
		switch a.Action {
		case "add":
			added++
		case "skip":
			skipped++
		case "alert":
			alerted++
		}
	}
	return
}
