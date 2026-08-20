package main

import (
	"testing"

	"synapse/internal/db"
	synclib "synapse/internal/sync"
)

// TestBuildMissingItemsRespectsLinks verifies that a docker service or NPM proxy
// with a service link pointing at a Kuma monitor is treated as covered even when
// its container/CNAME name does not match the Kuma monitor name, and that
// NPM-only links (no Kuma monitor) do not count as Kuma coverage.
func TestBuildMissingItemsRespectsLinks(t *testing.T) {
	services := []synclib.ServiceInfo{
		{Name: "web", ContainerName: "myproject-web-1", InKuma: false},
		{Name: "api", ContainerName: "myproject-api-1", InKuma: true},
		{Name: "db", ContainerName: "myproject-db-1", InKuma: false},
	}
	proxies := []synclib.ProxyInfo{
		{CNAME: "example.com", SourceInstanceID: 1, InKuma: false},
		{CNAME: "api.example.com", SourceInstanceID: 1, InKuma: false},
	}
	links := []db.ServiceLink{
		{ServiceName: "web", KumaMonitorID: 5, NPMHostName: "example.com"}, // linked to Kuma
		{ServiceName: "api", KumaMonitorID: 0, NPMHostName: "api.example.com"}, // NPM only, no Kuma
	}
	npmNameMap := map[int]string{1: "npm1"}

	docker, npm := buildMissingItems(services, proxies, links, npmNameMap)

	if !docker[0].InKuma {
		t.Errorf("expected linked service %q to be covered", docker[0].Name)
	}
	if !docker[1].InKuma {
		t.Errorf("expected %q to remain covered", docker[1].Name)
	}
	if docker[2].InKuma {
		t.Errorf("expected unlinked service %q to be missing", docker[2].Name)
	}
	if !npm[0].InKuma {
		t.Errorf("expected linked proxy %q to be covered", npm[0].Name)
	}
	if npm[1].InKuma {
		t.Errorf("expected NPM-only proxy %q to remain missing", npm[1].Name)
	}
}
