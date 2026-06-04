package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"synapse/internal/db"
	"synapse/internal/kuma"
	synclib "synapse/internal/sync"
	"synapse/internal/telemetry"
)

var version = "1.0.3"

type App struct {
	database *db.DB

	defaultSettings db.Settings

	mu            sync.Mutex
	running       bool
	progressChans []chan synclib.Progress
}

func (app *App) settings() db.Settings {
	return app.database.GetSettings(app.defaultSettings)
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func mask(s string) string {
	if s != "" {
		return "****"
	}
	return ""
}

func main() {
	defaults := db.Settings{
		ComposePath: getEnv("COMPOSE_PATH", "docker-compose.yml"),
		NPMHost:     getEnv("NPM_HOST", "http://nginx:81"),
		NPMUser:     getEnv("NPM_USER", "admin"),
		NPMPass:     getEnv("NPM_PASS", ""),
		KumaURL:     getEnv("KUMA_URL", "http://uptime-kuma:3001"),
		KumaUser:    getEnv("KUMA_USER", "admin"),
		KumaPass:    getEnv("KUMA_PASS", ""),
	}
	dbPath := getEnv("DB_PATH", "synapse.db")
	addr := getEnv("LISTEN_ADDR", ":6270")

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	app := &App{
		database:        database,
		defaultSettings: defaults,
	}

	tp, err := telemetry.InitTracerProvider()
	if err != nil {
		log.Fatalf("failed to init telemetry: %v", err)
	}
	defer telemetry.Shutdown(tp)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.SetTrustedProxies(nil)

	r.Use(otelgin.Middleware("synapse"))

	r.LoadHTMLGlob("static/*.html")

	r.GET("/", app.Dashboard)
	r.GET("/static/*filepath", func(c *gin.Context) {
		c.File("static/" + c.Param("filepath"))
	})

	api := r.Group("/api")
	{
		api.GET("/settings", app.GetSettings)
		api.POST("/settings", app.SaveSettings)
		api.POST("/test/npm", app.TestNPM)
		api.POST("/test/kuma", app.TestKuma)
		api.POST("/sync/docker", app.DockerSync)
		api.POST("/sync/npm", app.NPMSync)
		api.GET("/sync/progress", app.ProgressSSE)
		api.GET("/sync/history", app.SyncHistory)
		api.GET("/services", app.Services)
		api.GET("/proxies", app.Proxies)
		api.GET("/monitors", app.KumaMonitors)
		api.GET("/status", app.Status)
	}

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

func (app *App) Dashboard(c *gin.Context) {
	c.HTML(http.StatusOK, "index.html", gin.H{
		"Version": version,
	})
}

func (app *App) GetSettings(c *gin.Context) {
	s := app.settings()
	c.JSON(http.StatusOK, gin.H{
		"compose_path": s.ComposePath,
		"npm_host":     s.NPMHost,
		"npm_user":     s.NPMUser,
		"npm_pass":     mask(s.NPMPass),
		"kuma_url":    s.KumaURL,
		"kuma_user":   s.KumaUser,
		"kuma_pass":   mask(s.KumaPass),
	})
}

func (app *App) SaveSettings(c *gin.Context) {
	var s db.Settings
	if err := c.ShouldBindJSON(&s); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	current := app.settings()

	if s.NPMPass == "" || s.NPMPass == "****" {
		s.NPMPass = current.NPMPass
	}
	if s.KumaPass == "" || s.KumaPass == "****" {
		s.KumaPass = current.KumaPass
	}

	if err := app.database.SaveSettings(s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "saved"})
}

func (app *App) TestNPM(c *gin.Context) {
	s := app.settings()
	_, err := synclib.GetNPMProxiesWithStatus(s.NPMHost, s.NPMUser, s.NPMPass, s.KumaURL, s.KumaUser, s.KumaPass)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "NPM connection successful"})
}

func (app *App) TestKuma(c *gin.Context) {
	s := app.settings()
	client := kuma.NewClient(s.KumaURL)
	if err := client.Login(s.KumaUser, s.KumaPass); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	_, err := client.GetMonitors()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "Uptime Kuma connection successful"})
}

func (app *App) Status(c *gin.Context) {
	s := app.settings()
	services, err := synclib.LoadServices(s.ComposePath)
	dockerCount := 0
	if err == nil {
		dockerCount = len(services)
	}

	npmCount := 0
	npmErr := ""
	npmProxies, npmFetchErr := synclib.GetNPMProxiesWithStatus(s.NPMHost, s.NPMUser, s.NPMPass, s.KumaURL, s.KumaUser, s.KumaPass)
	if npmFetchErr == nil {
		npmCount = len(npmProxies)
	} else {
		npmErr = npmFetchErr.Error()
	}

	monitorCount, _ := app.database.GetMonitorCount()

	lastDocker, _ := app.database.GetLatestSyncRun("docker")
	lastNPM, _ := app.database.GetLatestSyncRun("npm")

	c.JSON(http.StatusOK, gin.H{
		"docker_count":  dockerCount,
		"npm_count":     npmCount,
		"npm_error":     npmErr,
		"monitor_count": monitorCount,
		"last_docker":   lastDocker,
		"last_npm":      lastNPM,
		"running":       app.running,
	})
}

func (app *App) Services(c *gin.Context) {
	s := app.settings()
	result, err := synclib.GetDockerServicesWithStatus(s.ComposePath, s.KumaURL, s.KumaUser, s.KumaPass)
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
	s := app.settings()
	result, err := synclib.GetNPMProxiesWithStatus(s.NPMHost, s.NPMUser, s.NPMPass, s.KumaURL, s.KumaUser, s.KumaPass)
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
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	URL   string `json:"url,omitempty"`
	DockerContainer string `json:"docker_container,omitempty"`
}

func (app *App) KumaMonitors(c *gin.Context) {
	s := app.settings()
	client := kuma.NewClient(s.KumaURL)
	if err := client.Login(s.KumaUser, s.KumaPass); err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error()})
		return
	}
	kumaMonitors, err := client.GetMonitors()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"error": err.Error()})
		return
	}

	result := make([]KumaMonitorSummary, 0, len(kumaMonitors))
	for _, m := range kumaMonitors {
		result = append(result, KumaMonitorSummary{
			ID:              m.ID,
			Name:            m.Name,
			Type:            m.Type,
			URL:             m.URL,
			DockerContainer: m.DockerContainer,
		})
	}
	c.JSON(http.StatusOK, result)
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

		synclib.RunDockerSync(s.ComposePath, s.KumaURL, s.KumaUser, s.KumaPass, app.database, func(p synclib.Progress) {
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

		synclib.RunNPMSync(s.NPMHost, s.NPMUser, s.NPMPass, s.KumaURL, s.KumaUser, s.KumaPass, app.database, func(p synclib.Progress) {
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
