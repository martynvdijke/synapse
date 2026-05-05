package kuma

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	url    string
	token  string
	client *http.Client
}

type Monitor struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	URL             string `json:"url,omitempty"`
	DockerContainer string `json:"docker_container,omitempty"`
	DockerHost      int    `json:"docker_host,omitempty"`
}

type DockerHost struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type LoginResult struct {
	Token string `json:"token"`
}

func NewClient(url string) *Client {
	return &Client{
		url: url,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) Login(username, password string) error {
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})

	resp, err := c.client.Post(fmt.Sprintf("%s/api/login", c.url), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed: status %d", resp.StatusCode)
	}

	var result LoginResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.Token == "" {
		return fmt.Errorf("no token received")
	}
	c.token = result.Token
	return nil
}

func (c *Client) doRequest(method, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(method, fmt.Sprintf("%s%s", c.url, path), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.client.Do(req)
}

func (c *Client) GetDockerHosts() ([]DockerHost, error) {
	resp, err := c.doRequest("GET", "/api/docker-hosts", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get docker hosts: status %d", resp.StatusCode)
	}

	var hosts []DockerHost
	if err := json.NewDecoder(resp.Body).Decode(&hosts); err != nil {
		return nil, err
	}
	return hosts, nil
}

func (c *Client) GetMonitors() ([]Monitor, error) {
	resp, err := c.doRequest("GET", "/api/monitors", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get monitors: status %d", resp.StatusCode)
	}

	var result struct {
		Monitors []Monitor `json:"monitors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Monitors, nil
}

func (c *Client) AddMonitor(monitorType, name, url, dockerContainer string, dockerHostID int) (int, error) {
	payload := map[string]any{
		"name":          name,
		"type":          monitorType,
		"interval":      60,
		"retryInterval": 60,
		"maxretries":    3,
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

	body, _ := json.Marshal(payload)
	resp, err := c.doRequest("POST", "/api/monitors", body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("add monitor: status %d", resp.StatusCode)
	}

	var m Monitor
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return 0, err
	}
	return m.ID, nil
}
