package sync

import (
	"testing"
)

//go:fix inline
func strPtr(s string) *string { return new(s) }

func TestParseHealthcheck_Nil(t *testing.T) {
	got := ParseHealthcheck("svc", ServiceDef{})
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestParseHealthcheck_NoTest(t *testing.T) {
	got := ParseHealthcheck("svc", ServiceDef{HealthCheck: &HealthDef{}})
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestParseHealthcheck_CMD_Curl_Localhost(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "my-web",
		HealthCheck: &HealthDef{
			Test: []any{"CMD", "curl", "-f", "http://localhost:8080/health"},
		},
	}
	got := ParseHealthcheck("web", svc)
	want := "http://my-web:8080/health"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestParseHealthcheck_CMD_Curl_Localhost_RootPath(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "my-web",
		HealthCheck: &HealthDef{
			Test: []any{"CMD", "curl", "-f", "http://127.0.0.1/"}, // root path
		},
	}
	got := ParseHealthcheck("web", svc)
	want := "http://my-web:80/"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestParseHealthcheck_CMD_Curl_Localhost_DefaultPath(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "my-web",
		HealthCheck: &HealthDef{
			Test: []any{"CMD", "curl", "-f", "http://localhost:8080"},
		},
	}
	got := ParseHealthcheck("web", svc)
	want := "http://my-web:8080/"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestParseHealthcheck_CMD_Curl_Localhost_DefaultPort(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "my-web",
		HealthCheck: &HealthDef{
			Test: []any{"CMD", "curl", "-f", "http://localhost/health"},
		},
	}
	got := ParseHealthcheck("web", svc)
	want := "http://my-web:80/health"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestParseHealthcheck_CMD_Curl_Localhost_NoContainerName(t *testing.T) {
	svc := ServiceDef{
		HealthCheck: &HealthDef{
			Test: []any{"CMD", "curl", "-f", "http://localhost:3000/ready"},
		},
	}
	got := ParseHealthcheck("api", svc)
	want := "http://api:3000/ready"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestParseHealthcheck_CMD_Curl_Localhost_QueryParams(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "svc-health",
		HealthCheck: &HealthDef{
			Test: []any{"CMD", "curl", "-f", "http://localhost:5000/check?token=abc&format=json"},
		},
	}
	got := ParseHealthcheck("svc", svc)
	want := "http://svc-health:5000/check?token=abc&format=json"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestParseHealthcheck_CMD_Curl_Localhost_DeepPath(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "app",
		HealthCheck: &HealthDef{
			Test: []any{"CMD", "curl", "-f", "http://localhost:9000/api/v2/health"},
		},
	}
	got := ParseHealthcheck("app", svc)
	want := "http://app:9000/api/v2/health"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestParseHealthcheck_CMD_Curl_Localhost_UnderscorePath(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "my-svc",
		HealthCheck: &HealthDef{
			Test: []any{"CMD", "curl", "-f", "http://localhost:8080/live_check"},
		},
	}
	got := ParseHealthcheck("svc", svc)
	want := "http://my-svc:8080/live_check"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// CMDSHELL with shell-wrapped curl
func TestParseHealthcheck_CMDSHELL_Curl(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "api-server",
		HealthCheck: &HealthDef{
			Test: []any{"CMD-SHELL", "curl -f http://localhost:8080/api/health || exit 1"},
		},
	}
	got := ParseHealthcheck("api", svc)
	want := "http://api-server:8080/api/health"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// String-format healthcheck (Docker-compose alternative syntax)
func TestParseHealthcheck_StringFormat(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "worker",
		HealthCheck: &HealthDef{
			Test: "curl -f http://localhost:9090/ready || exit 1",
		},
	}
	got := ParseHealthcheck("worker", svc)
	want := "http://worker:9090/ready"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// wget healthcheck
func TestParseHealthcheck_Wget(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "web",
		HealthCheck: &HealthDef{
			Test: []any{"CMD", "wget", "--spider", "http://localhost:8080/health"},
		},
	}
	got := ParseHealthcheck("web", svc)
	want := "http://web:8080/health"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// String wget healthcheck
func TestParseHealthcheck_Wget_String(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "monitor",
		HealthCheck: &HealthDef{
			Test: "wget --spider http://localhost:9090/ready 2>/dev/null",
		},
	}
	got := ParseHealthcheck("monitor", svc)
	want := "http://monitor:9090/ready"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// Non-localhost URL (service-name URL) should be returned as-is
func TestParseHealthcheck_ServiceNameURL(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "app",
		HealthCheck: &HealthDef{
			Test: []any{"CMD", "curl", "-f", "http://upstream:4000/health"},
		},
	}
	got := ParseHealthcheck("app", svc)
	want := "http://upstream:4000/health"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// Non-localhost URL without explicit port
func TestParseHealthcheck_ServiceNameURL_DefaultPort(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "app",
		HealthCheck: &HealthDef{
			Test: []any{"CMD", "curl", "-f", "http://upstream/health"},
		},
	}
	got := ParseHealthcheck("app", svc)
	want := "http://upstream:80/health"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// Network mode service: should use the referenced service name
func TestParseHealthcheck_NetworkModeService(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "sidecar",
		NetworkMode:   "service:proxy",
		HealthCheck: &HealthDef{
			Test: []any{"CMD", "curl", "-f", "http://localhost:8080/health"},
		},
	}
	got := ParseHealthcheck("my-service", svc)
	want := "http://proxy:8080/health"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// Healthcheck with no HTTP URL (e.g. pg_isready, redis-cli ping)
func TestParseHealthcheck_NoHTTP(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "pg",
		HealthCheck: &HealthDef{
			Test: []any{"CMD-SHELL", "pg_isready -U postgres"},
		},
	}
	got := ParseHealthcheck("db", svc)
	if got != "" {
		t.Errorf("expected empty for non-HTTP healthcheck, got %q", got)
	}
}

// Healthcheck with no HTTP URL (redis-ping)
func TestParseHealthcheck_NoHTTP_Redis(t *testing.T) {
	svc := ServiceDef{
		ContainerName: "redis",
		HealthCheck: &HealthDef{
			Test: []any{"CMD", "redis-cli", "ping"},
		},
	}
	got := ParseHealthcheck("redis", svc)
	if got != "" {
		t.Errorf("expected empty for non-HTTP healthcheck, got %q", got)
	}
}

// LoadServices tests
func TestLoadServices_FileNotFound(t *testing.T) {
	_, err := LoadServices("/nonexistent/path/docker-compose.yml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadServices_Success(t *testing.T) {
	services, err := LoadServices("../../testdata/docker-compose.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name         string
		wantContName string
		wantHasHC    bool
	}{
		{"web", "nginx-web", true},
		{"api", "myapp-api", true},
		{"db", "", true},
		{"redis", "cache-redis", true},
		{"worker", "", true}, // no container_name in compose
	}

	for _, tt := range tests {
		svc, ok := services[tt.name]
		if !ok {
			t.Errorf("service %q not found", tt.name)
			continue
		}
		if svc.ContainerName != tt.wantContName {
			t.Errorf("service %q container_name: expected %q, got %q", tt.name, tt.wantContName, svc.ContainerName)
		}
		if tt.wantHasHC && svc.HealthCheck == nil {
			t.Errorf("service %q: expected healthcheck, got nil", tt.name)
		}
	}
}

func TestParseHealthcheck_FromFile(t *testing.T) {
	services, err := LoadServices("../../testdata/docker-compose.yml")
	if err != nil {
		t.Fatalf("load services: %v", err)
	}

	tests := []struct {
		name string
		want string
	}{
		{"web", "http://nginx-web:80/health"},
		{"api", "http://myapp-api:8080/api/health"},
		{"db", ""},                             // pg_isready, no HTTP
		{"redis", ""},                          // redis-cli ping, no HTTP
		{"worker", "http://worker:9090/ready"}, // no container_name → uses service name
	}

	for _, tt := range tests {
		svc, ok := services[tt.name]
		if !ok {
			t.Errorf("service %q not found", tt.name)
			continue
		}
		got := ParseHealthcheck(tt.name, svc)
		if got != tt.want {
			t.Errorf("ParseHealthcheck(%q): expected %q, got %q", tt.name, tt.want, got)
		}
	}
}

// Test that the extractTestString helper handles all test types
func TestExtractTestString(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"nil", nil, ""},
		{"string", "curl -f http://localhost:8080/health", "curl -f http://localhost:8080/health"},
		{"[]any", []any{"CMD", "curl", "-f", "http://localhost:8080/health"}, "CMD curl -f http://localhost:8080/health"},
		{"int", 42, ""},
	}

	for _, tt := range tests {
		got := extractTestString(tt.input)
		if got != tt.want {
			t.Errorf("extractTestString(%s): expected %q, got %q", tt.name, tt.want, got)
		}
	}
}
