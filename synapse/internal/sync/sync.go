package sync

import (
	"fmt"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"

	"synapse/internal/db"
	"synapse/internal/kuma"
)

type ServiceDef struct {
	ContainerName string       `yaml:"container_name"`
	HealthCheck  *HealthDef   `yaml:"healthcheck"`
	NetworkMode string        `yaml:"network_mode"`
}

type HealthDef struct {
	Test any `yaml:"test"`
}

type Compose struct {
	Services map[string]ServiceDef `yaml:"services"`
}

type Progress struct {
	Total     int
	Current   int
	Status    string
	Message   string
	Added     int
	Skipped   int
	Failed    int
	RunID     int64
}

type SyncFn func(p Progress)

func ParseHealthcheck(name string, svc ServiceDef) string {
	if svc.HealthCheck == nil {
		return ""
	}
	var testStr string
	switch v := svc.HealthCheck.Test.(type) {
	case []any:
		for _, t := range v {
			testStr += fmt.Sprintf("%v ", t)
		}
		if len(testStr) > 0 {
			testStr = testStr[:len(testStr)-1]
		}
	case string:
		testStr = v
	default:
		return ""
	}

	re := regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1)(?::(\d+))?(/[\w/.-]*)?`)
	m := re.FindStringSubmatch(testStr)
	if m == nil {
		return ""
	}

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

func LoadServices(path string) (map[string]ServiceDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Compose
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return c.Services, nil
}

func RunSync(composePath, kumaURL, kumaUser, kumaPass string, database *db.DB, onProgress SyncFn) db.SyncRun {
	run := db.SyncRun{
		Status:    "running",
		StartedAt: time.Now(),
	}
	id, err := database.CreateSyncRun(&run)
	if err != nil {
		return db.SyncRun{Status: "error", ErrorMessage: err.Error()}
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

	onProgress(Progress{RunID: id, Total: len(services), Status: "logging_in", Message: "Logging into Uptime Kuma..."})

	client := kuma.NewClient(kumaURL)
	if err := client.Login(kumaUser, kumaPass); err != nil {
		database.FinishSyncRun(id, "error", 0, 0, 0, err.Error())
		run.Status = "error"
		run.ErrorMessage = err.Error()
		return run
	}

	onProgress(Progress{RunID: id, Total: len(services), Status: "fetching_docker_hosts", Message: "Fetching Docker hosts..."})

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

	onProgress(Progress{RunID: id, Total: len(services), Status: "fetching_monitors", Message: "Fetching existing monitors..."})

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
			onProgress(Progress{RunID: id, Total: len(services), Current: current,
				Status: "skipping", Message: fmt.Sprintf("Skipping %s (already exists)", displayName),
				Added: added, Skipped: skipped, Failed: failed})
			continue
		}

		url := ParseHealthcheck(name, svc)

		onProgress(Progress{RunID: id, Total: len(services), Current: current,
			Status: "adding", Message: fmt.Sprintf("Adding %s...", displayName),
			Added: added, Skipped: skipped, Failed: failed})

		var err error
		if url != "" {
			err = client.AddMonitor("http", displayName, url, "", dockerHostID)
		} else {
			containerID := svc.ContainerName
			if containerID == "" {
				containerID = name
			}
			err = client.AddMonitor("docker", displayName, "", containerID, dockerHostID)
		}

		if err != nil {
			failed++
			onProgress(Progress{RunID: id, Total: len(services), Current: current,
				Status: "error", Message: fmt.Sprintf("Failed: %s - %v", displayName, err),
				Added: added, Skipped: skipped, Failed: failed})
		} else {
			added++
			database.AddMonitor(&db.Monitor{
				Name:           displayName,
				ServiceName:    name,
				MonitorType:    func() string { if url != "" { return "http" }; return "docker" }(),
				URL:            url,
				DockerContainer: svc.ContainerName,
				CreatedAt:      time.Now(),
			})
			onProgress(Progress{RunID: id, Total: len(services), Current: current,
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

	onProgress(Progress{RunID: id, Total: len(services), Current: len(services),
		Status: status, Message: "Sync complete",
		Added: added, Skipped: skipped, Failed: failed})

	return run
}

func timePtr(t time.Time) *time.Time {
	return &t
}
