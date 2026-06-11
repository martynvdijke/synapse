package sync

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"

	"synapse/internal/db"
	"synapse/internal/kuma"
	"synapse/internal/logging"
	"synapse/internal/npm"
)

var tracer = otel.Tracer("sync")

type ServiceDef struct {
	ContainerName string     `yaml:"container_name"`
	HealthCheck   *HealthDef `yaml:"healthcheck"`
	NetworkMode   string     `yaml:"network_mode"`
}

type HealthDef struct {
	Test any `yaml:"test"`
}

type Compose struct {
	Services map[string]ServiceDef `yaml:"services"`
}

type Progress struct {
	Total   int    `json:"total"`
	Current int    `json:"current"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Added   int    `json:"added"`
	Skipped int    `json:"skipped"`
	Failed  int    `json:"failed"`
	RunID   int64  `json:"run_id"`
	Source  string `json:"source"`
}

type ProgressFn func(p Progress)

type ServiceInfo struct {
	Name          string `json:"name"`
	ContainerName string `json:"container_name"`
	MonitorType   string `json:"type"`
	URL           string `json:"url,omitempty"`
	InKuma        bool   `json:"in_kuma"`
	KumaID        int    `json:"kuma_id,omitempty"`
}

type ProxyInfo struct {
	CNAME     string `json:"cname"`
	Container string `json:"container"`
	InKuma    bool   `json:"in_kuma"`
	KumaID    int    `json:"kuma_id,omitempty"`
}

// extractTestString joins healthcheck.Test into a single string for regex matching.
func extractTestString(test any) string {
	switch v := test.(type) {
	case []any:
		var parts []string
		for _, t := range v {
			parts = append(parts, fmt.Sprintf("%v", t))
		}
		return strings.Join(parts, " ")
	case string:
		return v
	default:
		return ""
	}
}

// urlSuffixRx matches the optional port and path portion of a URL (starting after the host).
var urlSuffixRx = regexp.MustCompile(`(?::(\d+))?(/[^\s"']*)?`)

// ParseHealthcheck extracts a monitor URL from a docker-compose healthcheck definition.
//
// It supports these patterns:
//   - CMD curl/wget http://localhost:PORT/PATH
//   - CMD-SHELL curl/wget http://localhost:PORT/PATH || exit 1
//   - String healthcheck: curl -f http://localhost:PORT/PATH
//   - Non-localhost URLs (service names / container names) used as-is
//
// For localhost URLs, the host is rewritten to the service/container name so that
// Uptime Kuma can reach it via the Docker internal network. For non-localhost URLs
// the URL is returned unchanged.
func ParseHealthcheck(name string, svc ServiceDef) string {
	if svc.HealthCheck == nil {
		return ""
	}

	testStr := extractTestString(svc.HealthCheck.Test)
	if testStr == "" {
		return ""
	}

	// First try: match localhost / 127.0.0.1 URLs.
	// Extract port and path, then reconstruct with container/service name as host.
	reLocal := regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1)(?::(\d+))?(/[^\s"']*)?`)
	if m := reLocal.FindStringSubmatch(testStr); m != nil {
		port := "80"
		if m[1] != "" {
			port = m[1]
		}
		path := "/"
		if m[2] != "" {
			path = m[2]
		}

		hostname := name
		if svc.NetworkMode != "" {
			if len(svc.NetworkMode) > 8 && svc.NetworkMode[:8] == "service:" {
				hostname = svc.NetworkMode[8:]
			}
		} else if svc.ContainerName != "" {
			hostname = svc.ContainerName
		}

		return fmt.Sprintf("http://%s:%s%s", hostname, port, path)
	}

	// Second try: non-localhost URL (e.g. service-name:port/path).
	// These are already valid Docker-internal URLs, so use them as-is.
	reSvc := regexp.MustCompile(`https?://([^/\s"':]+)(?::(\d+))?(/[^\s"']*)?`)
	if m := reSvc.FindStringSubmatch(testStr); m != nil {
		host := m[1]
		port := "80"
		if m[2] != "" {
			port = m[2]
		}
		path := "/"
		if m[3] != "" {
			path = m[3]
		}
		return fmt.Sprintf("http://%s:%s%s", host, port, path)
	}

	return ""
}

func LoadServices(path string) (map[string]ServiceDef, error) {
	start := time.Now()
	logging.LogDebug("sync", "Loading compose file",
		slog.String("path", path),
	)
	data, err := os.ReadFile(path)
	if err != nil {
		logging.LogError("sync", "Failed to read compose file",
			slog.String("path", path),
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}
	var c Compose
	if err := yaml.Unmarshal(data, &c); err != nil {
		logging.LogError("sync", "Failed to parse compose file",
			slog.String("path", path),
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}
	logging.LogInfo("sync", "Loaded compose file",
		slog.String("path", path),
		slog.Int("service_count", len(c.Services)),
		slog.Duration("duration", time.Since(start)),
	)
	return c.Services, nil
}

func GetDockerServicesWithStatus(composePath, kumaURL, kumaUser, kumaPass string) ([]ServiceInfo, error) {
	_, span := tracer.Start(context.Background(), "GetDockerServicesWithStatus",
		trace.WithAttributes(attribute.String("compose_path", composePath)),
	)
	defer span.End()

	start := time.Now()
	logging.LogDebug("sync", "Getting Docker services with status",
		slog.String("compose_path", composePath),
	)

	services, err := LoadServices(composePath)
	if err != nil {
		return nil, err
	}

	k := kuma.NewClient(kumaURL)
	if err := k.Login(kumaUser, kumaPass); err != nil {
		return nil, fmt.Errorf("kuma login failed: %w", err)
	}

	kumaMonitors, err := k.GetMonitors()
	if err != nil {
		return nil, fmt.Errorf("get monitors failed: %w", err)
	}

	kumaMap := make(map[string]kuma.Monitor)
	for _, m := range kumaMonitors {
		kumaMap[m.Name] = m
	}

	var result []ServiceInfo
	for name, svc := range services {
		displayName := svc.ContainerName
		if displayName == "" {
			displayName = name
		}

		url := ParseHealthcheck(name, svc)
		monitorType := "docker"
		if url != "" {
			monitorType = "http"
		}

		info := ServiceInfo{
			Name:          name,
			ContainerName: displayName,
			MonitorType:   monitorType,
			URL:           url,
		}

		if km, ok := kumaMap[displayName]; ok {
			info.InKuma = true
			info.KumaID = km.ID
		}

		result = append(result, info)
	}

	logging.LogInfo("sync", "Docker services with status",
		slog.Int("service_count", len(result)),
		slog.Duration("duration", time.Since(start)),
	)
	return result, nil
}

// NPMProxyEntry is the full proxy entry data (without Kuma status).
type NPMProxyEntry struct {
	CNAME     string `json:"cname"`
	Container string `json:"container"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`
}

// GetNPMProxyEntries fetches proxy entries from NPM without Kuma status augmentation.
func GetNPMProxyEntries(npmHost, npmUser, npmPass string) ([]NPMProxyEntry, error) {
	entries, err := npm.GetProxyHosts(npmHost, npmUser, npmPass)
	if err != nil {
		return nil, err
	}

	result := make([]NPMProxyEntry, len(entries))
	for i, e := range entries {
		result[i] = NPMProxyEntry{
			CNAME:     e.CNAME,
			Container: e.Container,
			Host:      e.Host,
			Port:      e.Port,
			Protocol:  e.Protocol,
		}
	}
	return result, nil
}

func GetNPMProxiesWithStatus(npmHost, npmUser, npmPass, kumaURL, kumaUser, kumaPass string) ([]ProxyInfo, error) {
	_, span := tracer.Start(context.Background(), "GetNPMProxiesWithStatus",
		trace.WithAttributes(attribute.String("npm_host", npmHost)),
	)
	defer span.End()

	start := time.Now()
	logging.LogDebug("sync", "Getting NPM proxies with Kuma status",
		slog.String("npm_host", npmHost),
	)

	entries, err := npm.GetProxyHosts(npmHost, npmUser, npmPass)
	if err != nil {
		return nil, err
	}

	k := kuma.NewClient(kumaURL)
	if err := k.Login(kumaUser, kumaPass); err != nil {
		return nil, fmt.Errorf("kuma login failed: %w", err)
	}

	kumaMonitors, err := k.GetMonitors()
	if err != nil {
		return nil, fmt.Errorf("get monitors failed: %w", err)
	}

	kumaMap := make(map[string]kuma.Monitor)
	for _, m := range kumaMonitors {
		kumaMap[m.Name] = m
	}

	var result []ProxyInfo
	for _, e := range entries {
		info := ProxyInfo{
			CNAME:     e.CNAME,
			Container: e.Container,
		}

		if km, ok := kumaMap[e.CNAME]; ok {
			info.InKuma = true
			info.KumaID = km.ID
		}

		result = append(result, info)
	}

	logging.LogInfo("sync", "NPM proxies with status",
		slog.Int("proxy_count", len(result)),
		slog.Duration("duration", time.Since(start)),
	)
	return result, nil
}

func RunDockerSync(composePath, kumaURL, kumaUser, kumaPass string, database *db.DB, onProgress ProgressFn) db.SyncRun {
	_, span := tracer.Start(context.Background(), "RunDockerSync",
		trace.WithAttributes(attribute.String("compose_path", composePath)),
	)
	defer span.End()

	syncStart := time.Now()
	logging.LogInfo("sync", "Starting Docker sync",
		slog.String("compose_path", composePath),
	)

	run := db.SyncRun{
		Source:    "docker",
		Status:    "running",
		StartedAt: time.Now(),
	}
	id, err := database.CreateSyncRun(&run)
	if err != nil {
		logging.LogError("sync", "Failed to create Docker sync run",
			slog.String("error", err.Error()),
		)
		return db.SyncRun{Source: "docker", Status: "error", ErrorMessage: err.Error()}
	}
	run.ID = id

	services, err := LoadServices(composePath)
	if err != nil {
		database.FinishSyncRun(id, "error", 0, 0, 0, err.Error())
		run.Status = "error"
		run.ErrorMessage = err.Error()
		return run
	}
	run.TotalServices = len(services)
	logging.LogInfo("sync", "Docker sync loaded services",
		slog.Int("service_count", len(services)),
	)

	onProgress(Progress{RunID: id, Source: "docker", Total: len(services), Status: "logging_in", Message: "Logging into Uptime Kuma..."})

	client := kuma.NewClient(kumaURL)
	if err := client.Login(kumaUser, kumaPass); err != nil {
		database.FinishSyncRun(id, "error", 0, 0, 0, err.Error())
		run.Status = "error"
		run.ErrorMessage = err.Error()
		return run
	}

	onProgress(Progress{RunID: id, Source: "docker", Total: len(services), Status: "fetching_docker_hosts", Message: "Fetching Docker hosts..."})

	hosts, err := client.GetDockerHosts()
	if err != nil {
		database.FinishSyncRun(id, "error", 0, 0, 0, err.Error())
		run.Status = "error"
		run.ErrorMessage = err.Error()
		return run
	}

	var dockerHostID int
	if len(hosts) > 0 {
		dockerHostID = hosts[0].ID
	} else {
		database.FinishSyncRun(id, "error", 0, 0, 0, "no Docker hosts found in Uptime Kuma")
		run.Status = "error"
		run.ErrorMessage = "no Docker hosts found"
		return run
	}

	onProgress(Progress{RunID: id, Source: "docker", Total: len(services), Status: "fetching_monitors", Message: "Fetching existing monitors..."})

	existingMonitors, err := client.GetMonitors()
	if err != nil {
		database.FinishSyncRun(id, "error", 0, 0, 0, err.Error())
		run.Status = "error"
		run.ErrorMessage = err.Error()
		return run
	}

	existing := make(map[string]bool)
	for _, m := range existingMonitors {
		existing[m.Name] = true
	}

	added := 0
	skipped := 0
	failed := 0
	current := 0

	for name, svc := range services {
		current++
		displayName := svc.ContainerName
		if displayName == "" {
			displayName = name
		}

		if existing[displayName] {
			skipped++
			logging.LogInfo("sync", "Skipping service (already monitored)",
				slog.String("service", displayName),
				slog.Int("current", current),
				slog.Int("total", len(services)),
				slog.Int("added", added),
				slog.Int("skipped", skipped),
				slog.Int("failed", failed),
			)
			onProgress(Progress{RunID: id, Source: "docker", Total: len(services), Current: current,
				Status: "skipping", Message: fmt.Sprintf("Skipping %s (already exists)", displayName),
				Added: added, Skipped: skipped, Failed: failed})
			continue
		}

		url := ParseHealthcheck(name, svc)

		onProgress(Progress{RunID: id, Source: "docker", Total: len(services), Current: current,
			Status: "adding", Message: fmt.Sprintf("Adding %s...", displayName),
			Added: added, Skipped: skipped, Failed: failed})

		var err error
		var kumaID int
		if url != "" {
			kumaID, err = client.AddMonitor("http", displayName, url, "", dockerHostID)
		} else {
			containerID := svc.ContainerName
			if containerID == "" {
				containerID = name
			}
			kumaID, err = client.AddMonitor("docker", displayName, "", containerID, dockerHostID)
		}

		if err != nil {
			failed++
			logging.LogError("sync", "Failed to add monitor for service",
				slog.String("service", displayName),
				slog.String("error", err.Error()),
				slog.Int("added", added),
				slog.Int("skipped", skipped),
				slog.Int("failed", failed),
			)
			onProgress(Progress{RunID: id, Source: "docker", Total: len(services), Current: current,
				Status: "error", Message: fmt.Sprintf("Failed: %s - %v", displayName, err),
				Added: added, Skipped: skipped, Failed: failed})
		} else {
			added++
			logging.LogInfo("sync", "Added monitor for service",
				slog.String("service", displayName),
				slog.String("monitor_type", func() string {
					if url != "" { return "http" }
					return "docker"
				}()),
				slog.Int("added", added),
				slog.Int("skipped", skipped),
				slog.Int("failed", failed),
			)
			database.AddMonitor(&db.Monitor{
				Name:        displayName,
				ServiceName: name,
				MonitorType: func() string {
					if url != "" {
						return "http"
					}
					return "docker"
				}(),
				URL:             url,
				DockerContainer: svc.ContainerName,
				KumaID:          kumaID,
				CreatedAt:       time.Now(),
			})
			onProgress(Progress{RunID: id, Source: "docker", Total: len(services), Current: current,
				Status: "added", Message: fmt.Sprintf("Added %s", displayName),
				Added: added, Skipped: skipped, Failed: failed})
		}
	}

	status := "completed"
	if failed > 0 {
		status = "completed_with_errors"
	}

	errMsg := ""
	if failed > 0 {
		errMsg = fmt.Sprintf("%d monitor(s) failed to add", failed)
	}

	database.FinishSyncRun(id, status, added, skipped, failed, errMsg)

	run.Status = status
	run.FinishedAt = timePtr(time.Now())
	run.Added = added
	run.Skipped = skipped
	run.Failed = failed
	run.ErrorMessage = errMsg

	logging.LogInfo("sync", "Docker sync complete",
		slog.Int("added", added),
		slog.Int("skipped", skipped),
		slog.Int("failed", failed),
		slog.Duration("duration", time.Since(syncStart)),
	)

	onProgress(Progress{RunID: id, Source: "docker", Total: len(services), Current: len(services),
		Status: status, Message: "Docker sync complete",
		Added: added, Skipped: skipped, Failed: failed})

	return run
}

func RunNPMSync(npmHost, npmUser, npmPass, kumaURL, kumaUser, kumaPass string, database *db.DB, onProgress ProgressFn) db.SyncRun {
	_, span := tracer.Start(context.Background(), "RunNPMSync",
		trace.WithAttributes(attribute.String("npm_host", npmHost)),
	)
	defer span.End()

	syncStart := time.Now()
	logging.LogInfo("sync", "Starting NPM sync",
		slog.String("npm_host", npmHost),
	)

	run := db.SyncRun{
		Source:    "npm",
		Status:    "running",
		StartedAt: time.Now(),
	}
	id, err := database.CreateSyncRun(&run)
	if err != nil {
		logging.LogError("sync", "Failed to create NPM sync run",
			slog.String("error", err.Error()),
		)
		return db.SyncRun{Source: "npm", Status: "error", ErrorMessage: err.Error()}
	}
	run.ID = id

	entries, err := npm.GetProxyHosts(npmHost, npmUser, npmPass)
	if err != nil {
		database.FinishSyncRun(id, "error", 0, 0, 0, err.Error())
		run.Status = "error"
		run.ErrorMessage = err.Error()
		return run
	}
	run.TotalServices = len(entries)
	logging.LogInfo("sync", "NPM sync loaded proxy entries",
		slog.Int("entry_count", len(entries)),
	)

	if len(entries) == 0 {
		database.FinishSyncRun(id, "completed", 0, 0, 0, "")
		run.Status = "completed"
		run.FinishedAt = timePtr(time.Now())
		onProgress(Progress{RunID: id, Source: "npm", Total: 0, Status: "completed", Message: "No NPM proxy hosts found"})
		return run
	}

	onProgress(Progress{RunID: id, Source: "npm", Total: len(entries), Status: "logging_in", Message: "Logging into Uptime Kuma..."})

	client := kuma.NewClient(kumaURL)
	if err := client.Login(kumaUser, kumaPass); err != nil {
		database.FinishSyncRun(id, "error", 0, 0, 0, err.Error())
		run.Status = "error"
		run.ErrorMessage = err.Error()
		return run
	}

	onProgress(Progress{RunID: id, Source: "npm", Total: len(entries), Status: "fetching_monitors", Message: "Fetching existing monitors..."})

	existingMonitors, err := client.GetMonitors()
	if err != nil {
		database.FinishSyncRun(id, "error", 0, 0, 0, err.Error())
		run.Status = "error"
		run.ErrorMessage = err.Error()
		return run
	}

	existing := make(map[string]bool)
	for _, m := range existingMonitors {
		existing[m.Name] = true
	}

	added := 0
	skipped := 0
	failed := 0
	current := 0

	for _, entry := range entries {
		current++
		cname := entry.CNAME

		if existing[cname] {
			skipped++
			logging.LogInfo("sync", "Skipping NPM entry (already monitored)",
				slog.String("cname", cname),
				slog.Int("current", current),
				slog.Int("total", len(entries)),
				slog.Int("added", added),
				slog.Int("skipped", skipped),
				slog.Int("failed", failed),
			)
			onProgress(Progress{RunID: id, Source: "npm", Total: len(entries), Current: current,
				Status: "skipping", Message: fmt.Sprintf("Skipping %s (already exists)", cname),
				Added: added, Skipped: skipped, Failed: failed})
			continue
		}

		onProgress(Progress{RunID: id, Source: "npm", Total: len(entries), Current: current,
			Status: "adding", Message: fmt.Sprintf("Adding %s...", cname),
			Added: added, Skipped: skipped, Failed: failed})

		kumaID, err := client.AddMonitor("http", cname, fmt.Sprintf("http://%s", cname), "", 0)

		if err != nil {
			failed++
			logging.LogError("sync", "Failed to add monitor for NPM entry",
				slog.String("cname", cname),
				slog.String("error", err.Error()),
				slog.Int("added", added),
				slog.Int("skipped", skipped),
				slog.Int("failed", failed),
			)
			onProgress(Progress{RunID: id, Source: "npm", Total: len(entries), Current: current,
				Status: "error", Message: fmt.Sprintf("Failed: %s - %v", cname, err),
				Added: added, Skipped: skipped, Failed: failed})
		} else {
			added++
			logging.LogInfo("sync", "Added monitor for NPM entry",
				slog.String("cname", cname),
				slog.Int("added", added),
				slog.Int("skipped", skipped),
				slog.Int("failed", failed),
			)
			database.AddMonitor(&db.Monitor{
				Name:        cname,
				ServiceName: entry.Container,
				MonitorType: "http",
				URL:         fmt.Sprintf("http://%s", cname),
				KumaID:      kumaID,
				CreatedAt:   time.Now(),
			})
			onProgress(Progress{RunID: id, Source: "npm", Total: len(entries), Current: current,
				Status: "added", Message: fmt.Sprintf("Added %s", cname),
				Added: added, Skipped: skipped, Failed: failed})
		}
	}

	status := "completed"
	if failed > 0 {
		status = "completed_with_errors"
	}

	errMsg := ""
	if failed > 0 {
		errMsg = fmt.Sprintf("%d monitor(s) failed to add", failed)
	}

	database.FinishSyncRun(id, status, added, skipped, failed, errMsg)

	run.Status = status
	run.FinishedAt = timePtr(time.Now())
	run.Added = added
	run.Skipped = skipped
	run.Failed = failed
	run.ErrorMessage = errMsg

	logging.LogInfo("sync", "NPM sync complete",
		slog.Int("added", added),
		slog.Int("skipped", skipped),
		slog.Int("failed", failed),
		slog.Duration("duration", time.Since(syncStart)),
	)

	onProgress(Progress{RunID: id, Source: "npm", Total: len(entries), Current: len(entries),
		Status: status, Message: "NPM sync complete",
		Added: added, Skipped: skipped, Failed: failed})

	return run
}

func timePtr(t time.Time) *time.Time {
	return &t
}
