package authelia

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "configuration.yml")

	cfgContent := `
access_control:
  default_policy: deny
  rules:
    - domain: app.example.com
      policy: one_factor
    - domain: secure.example.com
      policy: two_factor
    - domain: public.example.com
      policy: bypass
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ac, err := ParseConfig(cfgPath)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	if ac.DefaultPolicy != "deny" {
		t.Errorf("expected default_policy=deny, got %q", ac.DefaultPolicy)
	}

	if len(ac.Rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(ac.Rules))
	}
}

func TestParseConfig_MissingAccessControl(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "configuration.yml")

	cfgContent := `server:
  address: 'tcp://:9091'
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := ParseConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing access_control")
	}
}

func TestParseConfig_FileNotFound(t *testing.T) {
	_, err := ParseConfig("/nonexistent/path/config.yml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestParseConfig_MultiDomain(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "configuration.yml")

	cfgContent := `
access_control:
  default_policy: deny
  rules:
    - domain:
        - a.example.com
        - b.example.com
      policy: one_factor
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ac, err := ParseConfig(cfgPath)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	if len(ac.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(ac.Rules))
	}

	domains := GetDomains(ac)
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d: %v", len(domains), domains)
	}
}

func TestDomainMatches_Exact(t *testing.T) {
	domains := []string{"app.example.com", "secure.example.com"}

	tests := []struct {
		cname string
		want  string
	}{
		{"app.example.com", "app.example.com"},
		{"secure.example.com", "secure.example.com"},
		{"other.example.com", ""},
		{"example.com", ""},
	}

	for _, tt := range tests {
		got := DomainMatches(tt.cname, domains)
		if got != tt.want {
			t.Errorf("DomainMatches(%q) = %q, want %q", tt.cname, got, tt.want)
		}
	}
}

func TestDomainMatches_Wildcard(t *testing.T) {
	domains := []string{"*.example.com", "*.app.example.com"}

	tests := []struct {
		cname string
		want  string
	}{
		{"foo.example.com", "*.example.com"},
		{"bar.example.com", "*.example.com"},
		{"sub.app.example.com", "*.app.example.com"},
		{"example.com", ""},
		{"other.com", ""},
	}

	for _, tt := range tests {
		got := DomainMatches(tt.cname, domains)
		if got != tt.want {
			t.Errorf("DomainMatches(%q) = %q, want %q", tt.cname, got, tt.want)
		}
	}
}

func TestGetDomains(t *testing.T) {
	ac := &AccessControlConfig{
		DefaultPolicy: "deny",
		Rules: []AccessRule{
			{Domain: "one.example.com", Policy: "one_factor"},
			{Domain: []any{"two.example.com", "three.example.com"}, Policy: "bypass"},
		},
	}

	domains := GetDomains(ac)
	expected := []string{"one.example.com", "two.example.com", "three.example.com"}

	if len(domains) != len(expected) {
		t.Fatalf("expected %d domains, got %d: %v", len(expected), len(domains), domains)
	}

	for i := range expected {
		if domains[i] != expected[i] {
			t.Errorf("domain[%d] = %q, want %q", i, domains[i], expected[i])
		}
	}
}

func TestGetDomains_NilConfig(t *testing.T) {
	domains := GetDomains(nil)
	if domains != nil {
		t.Errorf("expected nil, got %v", domains)
	}
}

func TestWriteConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "configuration.yml")

	// Write initial config
	initialCfg := `server:
  address: 'tcp://:9091'
  tls:
    key: /config/key.pem

access_control:
  default_policy: deny
  rules:
    - domain: existing.example.com
      policy: one_factor

session:
  secret: test_secret
`
	if err := os.WriteFile(cfgPath, []byte(initialCfg), 0644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	// Write updated access control
	newAC := &AccessControlConfig{
		DefaultPolicy: "deny",
		Rules: []AccessRule{
			{Domain: "existing.example.com", Policy: "one_factor"},
			{Domain: "new.example.com", Policy: "two_factor"},
		},
	}

	if err := WriteConfig(cfgPath, newAC); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Read back and verify
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read back config: %v", err)
	}

	var parsed AutheliaConfig
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse back config: %v", err)
	}

	if parsed.AccessControl == nil {
		t.Fatal("access_control missing after write")
	}

	if len(parsed.AccessControl.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(parsed.AccessControl.Rules))
	}

	// Verify other sections are preserved
	var full map[string]any
	if err := yaml.Unmarshal(data, &full); err != nil {
		t.Fatalf("parse full config: %v", err)
	}

	if _, ok := full["server"]; !ok {
		t.Error("server section not preserved")
	}
	if _, ok := full["session"]; !ok {
		t.Error("session section not preserved")
	}
}
