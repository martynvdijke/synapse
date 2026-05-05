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
	ID              int      `json:"id"`
	Name            string   `json:"name"`
	Type            string   `json:"type"`
	URL             string   `json:"url,omitempty"`
	DockerContainer string   `json:"docker_container,omitempty"`
	DockerHost      int      `json:"docker_host,omitempty"`
	Interval        int      `json:"interval"`
	RetryInterval   int      `json:"retryInterval"`
	MaxRetries      int      `json:"maxretries"`
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

func (c *Client) GetDockerHosts() ([]DockerHost, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/docker-hosts", c.url), nil)
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
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
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/monitors", c.url), nil)
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
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

func (c *Client) AddMonitor(monitorType, name, url, dockerContainer string, dockerHostID int) error {
	payload := map[string]any{
		"name":          name,
		"type":          monitorType,
		"interval":     60,
		"retryInterval": 60,
		"maxretries":   3,
		"conditions":   []any{},
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
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/monitors", c.url), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("add monitor: status %d", resp.StatusCode)
	}
	return nil
}
