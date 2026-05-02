package kumasync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

type KumaAPI struct {
	url    string
	token  string
	client *http.Client
}

type LoginResponse struct {
	Token string `json:"token"`
}

type Monitor struct {
	ID               int      `json:"id"`
	Name             string   `json:"name"`
	Type             string   `json:"type"`
	URL              string   `json:"url,omitempty"`
	DockerContainer string   `json:"docker_container,omitempty"`
	DockerHost      int      `json:"docker_host,omitempty"`
	Interval        int      `json:"interval"`
	RetryInterval   int      `json:"retryInterval"`
	MaxRetries      int      `json:"maxretries"`
	Conditions      []any    `json:"conditions"`
	Method          string   `json:"method,omitempty"`
	AcceptedStatusCodes []int  `json:"accepted_statuscodes,omitempty"`
}

type MonitorsResponse struct {
	Monitors []Monitor `json:"monitors"`
}

type DockerHost struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type SyncResult struct {
	Added       int
	Skipped     int
	NewMonitors []string
}

func NewKumaAPI(url string) *KumaAPI {
	return &KumaAPI{
		url: url,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (k *KumaAPI) login(username, password string) error {
	loginData := map[string]string{
		"username": username,
		"password": password,
	}
	jsonData, _ := json.Marshal(loginData)

	resp, err := http.Post(
		fmt.Sprintf("%s/api/login", k.url),
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed with status: %d", resp.StatusCode)
	}

	var loginResp LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return err
	}

	k.token = loginResp.Token
	if k.token == "" {
		return fmt.Errorf("no token received from login")
	}

	return nil
}

func (k *KumaAPI) Login(username, password string) error {
	return k.login(username, password)
}

func (k *KumaAPI) headers() http.Header {
	headers := make(http.Header)
	headers.Set("Authorization", fmt.Sprintf("Bearer %s", k.token))
	return headers
}

func (k *KumaAPI) GetMonitors() ([]Monitor, error) {
	req, err := http.NewRequest(
		"GET",
		fmt.Sprintf("%s/api/monitors", k.url),
		nil,
	)
	if err != nil {
		return nil, err
	}
	req.Header = k.headers()

	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get monitors failed with status: %d", resp.StatusCode)
	}

	var monitorsResp MonitorsResponse
	if err := json.NewDecoder(resp.Body).Decode(&monitorsResp); err != nil {
		return nil, err
	}

	return monitorsResp.Monitors, nil
}

func (k *KumaAPI) GetDockerHosts() ([]DockerHost, error) {
	req, err := http.NewRequest(
		"GET",
		fmt.Sprintf("%s/api/docker-hosts", k.url),
		nil,
	)
	if err != nil {
		return nil, err
	}
	req.Header = k.headers()

	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get docker hosts failed with status: %d", resp.StatusCode)
	}

	var hosts []DockerHost
	if err := json.NewDecoder(resp.Body).Decode(&hosts); err != nil {
		return nil, err
	}

	return hosts, nil
}

func (k *KumaAPI) AddMonitor(monitorType, name, url, dockerContainer string, dockerHostID, interval, retryInterval, maxRetries int) (*Monitor, error) {
	payload := map[string]any{
		"name":          name,
		"type":          monitorType,
		"interval":     interval,
		"retryInterval": retryInterval,
		"maxretries":    maxRetries,
		"conditions":    []any{},
	}

	switch monitorType {
	case "http":
		payload["url"] = url
		payload["method"] = "GET"
		payload["accepted_statuscodes"] = []int{200, 201, 204, 301, 302}
	case "docker":
		payload["docker_container"] = dockerContainer
		payload["docker_host"] = dockerHostID
	}

	jsonData, _ := json.Marshal(payload)

	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("%s/api/monitors", k.url),
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return nil, err
	}
	req.Header = k.headers()
	req.Header.Set("Content-Type", "application/json")

	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("add monitor failed with status: %d", resp.StatusCode)
	}

	var monitor Monitor
	if err := json.NewDecoder(resp.Body).Decode(&monitor); err != nil {
		return nil, err
	}

	return &monitor, nil
}

type GotifyClient struct {
	url   string
	token string
}

func NewGotifyClient(url, token string) *GotifyClient {
	return &GotifyClient{url: url, token: token}
}

func (g *GotifyClient) SendAlert(title, message string, priority int) bool {
	data := map[string]string{
		"title":    title,
		"message":  message,
		"priority": fmt.Sprintf("%d", priority),
	}
	jsonData, _ := json.Marshal(data)

	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("%s/message", g.url),
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to send Gotify alert: %v\n", err)
		return false
	}
	req.Header.Set("X-Token", g.token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to send Gotify alert: %v\n", err)
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}