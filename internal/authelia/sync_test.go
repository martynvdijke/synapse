package authelia

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"synapse/internal/npm"
)

func TestCompareCNAMEs_AllMatched(t *testing.T) {
	npm := []string{"app.example.com", "api.example.com"}
	authelia := []string{"app.example.com", "api.example.com", "other.example.com"}

	matched, missing := CompareCNAMEs(npm, authelia)

	if len(matched) != 2 {
		t.Errorf("expected 2 matched, got %d: %v", len(matched), matched)
	}
	if len(missing) != 0 {
		t.Errorf("expected 0 missing, got %d: %v", len(missing), missing)
	}
}

func TestCompareCNAMEs_SomeMissing(t *testing.T) {
	npm := []string{"app.example.com", "unprotected.example.com", "api.example.com"}
	authelia := []string{"app.example.com", "other.example.com"}

	matched, missing := CompareCNAMEs(npm, authelia)

	if len(matched) != 1 || matched[0] != "app.example.com" {
		t.Errorf("expected [app.example.com] matched, got %v", matched)
	}

	if len(missing) != 2 {
		t.Errorf("expected 2 missing, got %d: %v", len(missing), missing)
	}
}

func TestCompareCNAMEs_WildcardMatch(t *testing.T) {
	npm := []string{"foo.example.com", "bar.example.com", "other.com"}
	authelia := []string{"*.example.com"}

	matched, missing := CompareCNAMEs(npm, authelia)

	if len(matched) != 2 {
		t.Errorf("expected 2 matched, got %d: %v", len(matched), matched)
	}
	if len(missing) != 1 || missing[0] != "other.com" {
		t.Errorf("expected [other.com] missing, got %v", missing)
	}
}

func TestCompareCNAMEs_Empty(t *testing.T) {
	matched, missing := CompareCNAMEs(nil, []string{"a.example.com"})
	if matched == nil {
		matched = []string{}
	}
	if missing == nil {
		missing = []string{}
	}

	if len(matched) != 0 {
		t.Errorf("expected 0 matched, got %d", len(matched))
	}
	if len(missing) != 0 {
		t.Errorf("expected 0 missing, got %d", len(missing))
	}
}

func TestSyncConfig_DryRun(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "configuration.yml")

	cfgContent := `
access_control:
  default_policy: deny
  rules:
    - domain: existing.example.com
      policy: one_factor
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	entries := []npm.ProxyEntry{
		{CNAME: "existing.example.com", Container: "web"},
		{CNAME: "new.example.com", Container: "api"},
	}

	actions, err := SyncConfig(cfgPath, "", entries, "one_factor", nil, true, true)
	if err != nil {
		t.Fatalf("sync config: %v", err)
	}

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}

	// First should be skip
	if actions[0].Action != "skip" {
		t.Errorf("expected action[0]=skip, got %q", actions[0].Action)
	}

	// Second should be add (dry-run)
	if actions[1].Action != "add" {
		t.Errorf("expected action[1]=add, got %q", actions[1].Action)
	}

	// Verify config was NOT modified (dry run)
	data, _ := os.ReadFile(cfgPath)
	if string(data) != cfgContent {
		t.Errorf("config was modified during dry run")
	}
}

func TestSyncConfig_AutoSyncDisabled(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "configuration.yml")

	cfgContent := `
access_control:
  default_policy: deny
  rules:
    - domain: existing.example.com
      policy: one_factor
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	entries := []npm.ProxyEntry{
		{CNAME: "existing.example.com", Container: "web"},
		{CNAME: "new.example.com", Container: "api"},
	}

	actions, err := SyncConfig(cfgPath, "", entries, "one_factor", nil, false, false)
	if err != nil {
		t.Fatalf("sync config: %v", err)
	}

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}

	if actions[1].Action != "alert" {
		t.Errorf("expected action[1]=alert, got %q", actions[1].Action)
	}
}

func TestSyncConfig_WithOverride(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "configuration.yml")

	cfgContent := `
access_control:
  default_policy: deny
  rules: []
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	entries := []npm.ProxyEntry{
		{CNAME: "public.example.com", Container: "static"},
		{CNAME: "admin.example.com", Container: "admin"},
	}

	overrides := map[string]string{
		"public.example.com": "bypass",
	}

	actions, err := SyncConfig(cfgPath, "", entries, "one_factor", overrides, true, true)
	if err != nil {
		t.Fatalf("sync config: %v", err)
	}

	// Find the public.example.com action
	for _, a := range actions {
		if a.CNAME == "public.example.com" {
			if a.Policy != "bypass" {
				t.Errorf("expected policy=bypass for public.example.com, got %q", a.Policy)
			}
		}
		if a.CNAME == "admin.example.com" {
			if a.Policy != "one_factor" {
				t.Errorf("expected policy=one_factor for admin.example.com (default), got %q", a.Policy)
			}
		}
	}
}

func TestSyncConfig_RealWrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "configuration.yml")

	cfgContent := `
access_control:
  default_policy: deny
  rules:
    - domain: existing.example.com
      policy: one_factor
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	entries := []npm.ProxyEntry{
		{CNAME: "existing.example.com", Container: "web"},
		{CNAME: "new.example.com", Container: "api"},
	}

	actions, err := SyncConfig(cfgPath, "", entries, "two_factor", nil, true, false)
	if err != nil {
		t.Fatalf("sync config: %v", err)
	}

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}

	if actions[1].Action != "add" || actions[1].Policy != "two_factor" {
		t.Errorf("expected action[1]=add/two_factor, got %s/%s", actions[1].Action, actions[1].Policy)
	}

	// Read back and verify the new rule was written
	ac, err := ParseConfig(cfgPath)
	if err != nil {
		t.Fatalf("parse config after sync: %v", err)
	}

	domains := GetDomains(ac)
	found := slices.Contains(domains, "new.example.com")
	if !found {
		t.Errorf("new.example.com not found in config after sync. Domains: %v", domains)
	}
}

func writeTestConfig(t *testing.T, dir, content string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "configuration.yml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func TestEnsureDomainRules_AddsMissingWithDefaultPolicy(t *testing.T) {
	cfgPath := writeTestConfig(t, t.TempDir(), `
access_control:
  default_policy: deny
  rules:
    - domain: existing.example.com
      policy: one_factor
`)

	entries := []npm.ProxyEntry{
		{CNAME: "new.example.com", Container: "api"},
	}

	actions, err := EnsureDomainRules(cfgPath, "", entries, "", nil, false)
	if err != nil {
		t.Fatalf("ensure domain rules: %v", err)
	}

	if len(actions) != 1 || actions[0].Action != "add" {
		t.Fatalf("expected 1 add action, got %+v", actions)
	}
	if actions[0].Policy != DefaultPolicy {
		t.Errorf("expected policy %q (default), got %q", DefaultPolicy, actions[0].Policy)
	}

	ac, err := ParseConfig(cfgPath)
	if err != nil {
		t.Fatalf("parse config after ensure: %v", err)
	}
	if !slices.Contains(GetDomains(ac), "new.example.com") {
		t.Errorf("new.example.com not written to config: %v", GetDomains(ac))
	}
}

func TestEnsureDomainRules_RespectsOverride(t *testing.T) {
	cfgPath := writeTestConfig(t, t.TempDir(), `
access_control:
  default_policy: deny
  rules: []
`)

	entries := []npm.ProxyEntry{
		{CNAME: "public.example.com", Container: "static"},
	}

	actions, err := EnsureDomainRules(cfgPath, "", entries, "one_factor", map[string]string{"public.example.com": "bypass"}, false)
	if err != nil {
		t.Fatalf("ensure domain rules: %v", err)
	}

	if len(actions) != 1 || actions[0].Action != "add" {
		t.Fatalf("expected 1 add action, got %+v", actions)
	}
	if actions[0].Policy != "bypass" {
		t.Errorf("expected policy=bypass (override wins), got %q", actions[0].Policy)
	}
}

func TestEnsureDomainRules_SkipsCovered(t *testing.T) {
	cfgContent := `
access_control:
  default_policy: deny
  rules:
    - domain: existing.example.com
      policy: one_factor
`
	cfgPath := writeTestConfig(t, t.TempDir(), cfgContent)

	entries := []npm.ProxyEntry{
		{CNAME: "existing.example.com", Container: "web"},
	}

	actions, err := EnsureDomainRules(cfgPath, "", entries, "one_factor", nil, false)
	if err != nil {
		t.Fatalf("ensure domain rules: %v", err)
	}

	if len(actions) != 1 || actions[0].Action != "skip" {
		t.Fatalf("expected 1 skip action, got %+v", actions)
	}

	data, _ := os.ReadFile(cfgPath)
	if string(data) != cfgContent {
		t.Errorf("config modified although nothing to add")
	}
}

func TestEnsureDomainRules_DryRunWritesNothing(t *testing.T) {
	cfgContent := `
access_control:
  default_policy: deny
  rules:
    - domain: existing.example.com
      policy: one_factor
`
	cfgPath := writeTestConfig(t, t.TempDir(), cfgContent)

	entries := []npm.ProxyEntry{
		{CNAME: "new.example.com", Container: "api"},
	}

	actions, err := EnsureDomainRules(cfgPath, "", entries, "one_factor", nil, true)
	if err != nil {
		t.Fatalf("ensure domain rules (dry run): %v", err)
	}

	if len(actions) != 1 || actions[0].Action != "add" {
		t.Fatalf("expected 1 add action in dry run, got %+v", actions)
	}

	data, _ := os.ReadFile(cfgPath)
	if string(data) != cfgContent {
		t.Errorf("config modified during dry run")
	}
}
