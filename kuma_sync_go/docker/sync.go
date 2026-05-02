package docker

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"

	"kuma-sync/kumasync"
)

type ServiceData struct {
	ContainerName string       `yaml:"container_name"`
	HealthCheck  HealthCheck  `yaml:"healthcheck"`
	NetworkMode string        `yaml:"network_mode"`
}

type HealthCheck struct {
	Test any `yaml:"test"`
}

type ComposeFile struct {
	Services map[string]ServiceData `yaml:"services"`
}

func ParseHealthcheck(serviceName string, serviceData ServiceData) string {
	healthcheck := serviceData.HealthCheck
	test := healthcheck.Test

	var testStr string
	switch v := test.(type) {
	case []any:
		testStr = ""
		for _, t := range v {
			testStr += fmt.Sprintf("%v ", t)
		}
		testStr = testStr[:len(testStr)-1]
	case string:
		testStr = v
	default:
		testStr = ""
	}

	re := regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1)(?::(\d+))?(/[\w/.-]*)?`)
	matches := re.FindStringSubmatch(testStr)
	if matches == nil {
		return ""
	}

	port := "80"
	if matches[1] != "" {
		port = matches[1]
	}
	path := "/"
	if matches[2] != "" {
		path = matches[2]
	}

	hostname := serviceData.ContainerName
	if serviceName == "" {
		hostname = serviceName
	}

	networkMode := serviceData.NetworkMode
	if len(networkMode) > 8 && networkMode[:8] == "service:" {
		hostname = networkMode[8:]
	} else if serviceData.ContainerName != "" {
		hostname = serviceData.ContainerName
	}

	return fmt.Sprintf("http://%s:%s%s", hostname, port, path)
}

func GetServices(composePath string) (map[string]ServiceData, error) {
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("docker-compose.yml not found: %s", composePath)
	}

	data, err := os.ReadFile(composePath)
	if err != nil {
		return nil, err
	}

	var compose ComposeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, err
	}

	return compose.Services, nil
}

func SyncContainers(composePath, kumaURL, kumaUser, kumaPass, dockerHostName string,
	dockerSocket string, dryRun bool, gotifyURL, gotifyToken string) kumasync.SyncResult {

	services, err := GetServices(composePath)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return kumasync.SyncResult{}
	}

	if len(services) == 0 {
		fmt.Println("No services found in docker-compose.yml")
		return kumasync.SyncResult{}
	}

	fmt.Printf("Found %d services in %s\n", len(services), composePath)

	if dryRun || kumaURL == "" {
		added := 0
		for serviceName, serviceData := range services {
			displayName := serviceData.ContainerName
			if displayName == "" {
				displayName = serviceName
			}
			url := ParseHealthcheck(serviceName, serviceData)
			if url != "" {
				fmt.Printf("[+] %s: would add HTTP monitor -> %s\n", displayName, url)
			} else {
				fmt.Printf("[+] %s: would add Docker monitor\n", displayName)
			}
			added++
		}
		return kumasync.SyncResult{Added: added}
	}

	api := kumasync.NewKumaAPI(kumaURL)
	if err := api.Login(kumaUser, kumaPass); err != nil {
		fmt.Printf("Login failed: %v\n", err)
		return kumasync.SyncResult{}
	}

	dockerHosts, err := api.GetDockerHosts()
	if err != nil {
		fmt.Printf("Failed to get docker hosts: %v\n", err)
		return kumasync.SyncResult{}
	}

	dockerHostID := 0
	if dockerHostName != "" {
		for _, host := range dockerHosts {
			if host.Name == dockerHostName {
				dockerHostID = host.ID
				break
			}
		}
	}

	if dockerHostID == 0 && len(dockerHosts) > 0 {
		dockerHostID = dockerHosts[0].ID
		fmt.Printf("Using Docker host: %s (ID: %d)\n", dockerHosts[0].Name, dockerHostID)
	} else if dockerHostID == 0 {
		fmt.Println("No Docker host found in Uptime Kuma")
		fmt.Println("Please add a Docker host first via the Uptime Kuma UI")
		return kumasync.SyncResult{}
	}

	existingMonitors, err := api.GetMonitors()
	if err != nil {
		fmt.Printf("Failed to get monitors: %v\n", err)
		return kumasync.SyncResult{}
	}

	existing := make(map[string]bool)
	for _, m := range existingMonitors {
		existing[m.Name] = true
	}
	fmt.Printf("Existing monitors: %d\n", len(existing))

	added := 0
	skipped := 0
	newMonitors := []string{}

	for serviceName, serviceData := range services {
		displayName := serviceData.ContainerName
		if displayName == "" {
			displayName = serviceName
		}

		if existing[displayName] {
			fmt.Printf("[-] %s: already exists, skipping\n", displayName)
			skipped++
			continue
		}

		url := ParseHealthcheck(serviceName, serviceData)

		tryAdd := true
		if dryRun {
			tryAdd = false
		}

		if tryAdd {
			if url != "" {
				fmt.Printf("[+] %s: adding HTTP monitor -> %s\n", displayName, url)
				api.AddMonitor("http", displayName, url, "", dockerHostID, 60, 60, 3)
				newMonitors = append(newMonitors, displayName)
				added++
			} else {
				containerID := serviceData.ContainerName
				if containerID == "" {
					containerID = serviceName
				}
				fmt.Printf("[+] %s: adding Docker monitor (container: %s)\n", displayName, containerID)
				api.AddMonitor("docker", displayName, "", containerID, dockerHostID, 60, 60, 3)
				newMonitors = append(newMonitors, displayName)
				added++
			}
		} else {
			if url != "" {
				fmt.Printf("[+] %s: would add HTTP monitor -> %s\n", displayName, url)
			} else {
				fmt.Printf("[+] %s: would add Docker monitor\n", displayName)
			}
			newMonitors = append(newMonitors, displayName)
			added++
		}
	}

	result := kumasync.SyncResult{
		Added:       added,
		Skipped:     skipped,
		NewMonitors: newMonitors,
	}

	if gotifyURL != "" && gotifyToken != "" && added > 0 {
		msg := fmt.Sprintf("Added %d new monitors to Uptime Kuma:\n", added)
		for i, m := range newMonitors {
			if i >= 10 {
				break
			}
			msg += fmt.Sprintf("• %s\n", m)
		}
		if len(newMonitors) > 10 {
			msg += fmt.Sprintf("... and %d more\n", len(newMonitors)-10)
		}
		gotify := kumasync.NewGotifyClient(gotifyURL, gotifyToken)
		gotify.SendAlert("New Monitors Added", msg, 5)
	}

	return result
}