package kuma

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Login_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/login" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["username"] != "admin" || body["password"] != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResult{Token: "test-token-123"})
	}))
	defer server.Close()

	c := NewClient(server.URL)
	err := c.Login("admin", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.token != "test-token-123" {
		t.Errorf("expected token test-token-123, got %q", c.token)
	}
}

func TestClient_Login_WrongCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	c := NewClient(server.URL)
	err := c.Login("admin", "wrong")
	if err == nil {
		t.Fatal("expected error for wrong credentials")
	}
}

func TestClient_Login_NoToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(LoginResult{Token: ""})
	}))
	defer server.Close()

	c := NewClient(server.URL)
	err := c.Login("admin", "secret")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestClient_GetMonitors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(LoginResult{Token: "test-token"})
			return
		}
		if r.URL.Path == "/api/monitors" {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string][]Monitor{
				"monitors": {
					{ID: 1, Name: "Web App", Type: "http", URL: "http://app.example.com"},
					{ID: 2, Name: "API", Type: "http", URL: "http://api.example.com"},
					{ID: 3, Name: "DB", Type: "docker", DockerContainer: "postgres", DockerHost: 1},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL)
	if err := c.Login("admin", "secret"); err != nil {
		t.Fatalf("login: %v", err)
	}

	monitors, err := c.GetMonitors()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(monitors) != 3 {
		t.Fatalf("expected 3 monitors, got %d", len(monitors))
	}

	if monitors[0].Name != "Web App" || monitors[0].URL != "http://app.example.com" {
		t.Errorf("unexpected monitor 0: %+v", monitors[0])
	}
	if monitors[2].Type != "docker" || monitors[2].DockerContainer != "postgres" {
		t.Errorf("unexpected monitor 2: %+v", monitors[2])
	}
}

func TestClient_GetMonitors_Unauthenticated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	c := NewClient(server.URL)
	// Not logged in
	_, err := c.GetMonitors()
	if err == nil {
		t.Fatal("expected error for unauthenticated request")
	}
}

func TestClient_GetDockerHosts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(LoginResult{Token: "t"})
			return
		}
		if r.URL.Path == "/api/docker-hosts" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]DockerHost{
				{ID: 1, Name: "docker-host"},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL)
	if err := c.Login("admin", "secret"); err != nil {
		t.Fatalf("login: %v", err)
	}

	hosts, err := c.GetDockerHosts()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].Name != "docker-host" || hosts[0].ID != 1 {
		t.Errorf("unexpected host: %+v", hosts[0])
	}
}

func TestClient_AddMonitor_HTTP(t *testing.T) {
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(LoginResult{Token: "t"})
			return
		}
		if r.URL.Path == "/api/monitors" && r.Method == "POST" {
			json.NewDecoder(r.Body).Decode(&gotPayload)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(Monitor{ID: 42, Name: "test-monitor", Type: "http", URL: "http://example.com"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL)
	if err := c.Login("admin", "secret"); err != nil {
		t.Fatalf("login: %v", err)
	}

	id, err := c.AddMonitor("http", "test-monitor", "http://example.com", "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id != 42 {
		t.Errorf("expected monitor ID 42, got %d", id)
	}

	// Verify payload
	if gotPayload["name"] != "test-monitor" {
		t.Errorf("expected name 'test-monitor', got %v", gotPayload["name"])
	}
	if gotPayload["url"] != "http://example.com" {
		t.Errorf("expected url 'http://example.com', got %v", gotPayload["url"])
	}
	if gotPayload["type"] != "http" {
		t.Errorf("expected type 'http', got %v", gotPayload["type"])
	}
}

func TestClient_AddMonitor_HTTP_DefaultPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(LoginResult{Token: "t"})
			return
		}
		if r.URL.Path == "/api/monitors" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(Monitor{ID: 99, Name: "test-monitor", Type: "http", URL: "http://example.com"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL)
	if err := c.Login("admin", "secret"); err != nil {
		t.Fatalf("login: %v", err)
	}

	id, err := c.AddMonitor("http", "test-monitor", "http://example.com", "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 99 {
		t.Errorf("expected ID 99, got %d", id)
	}
}

func TestClient_AddMonitor_Docker(t *testing.T) {
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(LoginResult{Token: "t"})
			return
		}
		if r.URL.Path == "/api/monitors" && r.Method == "POST" {
			json.NewDecoder(r.Body).Decode(&gotPayload)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(Monitor{ID: 7, Name: "db-monitor", Type: "docker", DockerContainer: "postgres", DockerHost: 1})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(server.URL)
	if err := c.Login("admin", "secret"); err != nil {
		t.Fatalf("login: %v", err)
	}

	id, err := c.AddMonitor("docker", "db-monitor", "", "postgres", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id != 7 {
		t.Errorf("expected monitor ID 7, got %d", id)
	}

	if gotPayload["docker_container"] != "postgres" {
		t.Errorf("expected docker_container 'postgres', got %v", gotPayload["docker_container"])
	}
	if gotPayload["docker_host"] != float64(1) {
		t.Errorf("expected docker_host 1, got %v", gotPayload["docker_host"])
	}
	if gotPayload["type"] != "docker" {
		t.Errorf("expected type 'docker', got %v", gotPayload["type"])
	}
}
