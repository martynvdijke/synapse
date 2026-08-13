package notify

import (
	"strings"
	"testing"
)

func TestComputeMissing_GroupsByType(t *testing.T) {
	docker := []Item{
		{Name: "web", InKuma: true},
		{Name: "api", InKuma: false},
		{Name: "db", InKuma: false},
	}
	npm := []Item{
		{Name: "example.com", InKuma: false, Instance: "npm-edge"},
		{Name: "monitored.example.com", InKuma: true},
	}

	r := ComputeMissing(docker, npm, false, nil)
	if r.Degraded {
		t.Error("expected non-degraded report")
	}
	if len(r.Docker) != 2 {
		t.Errorf("expected 2 missing docker services, got %d: %v", len(r.Docker), r.Docker)
	}
	if len(r.NPM) != 1 {
		t.Errorf("expected 1 missing NPM proxy, got %d: %v", len(r.NPM), r.NPM)
	}
	if r.NPM[0] != "example.com (npm-edge)" {
		t.Errorf("expected instance suffix, got %q", r.NPM[0])
	}
	if r.Total() != 3 {
		t.Errorf("expected total 3, got %d", r.Total())
	}
}

func TestComputeMissing_NoMissing(t *testing.T) {
	docker := []Item{{Name: "web", InKuma: true}}
	npm := []Item{{Name: "example.com", InKuma: true}}

	r := ComputeMissing(docker, npm, false, nil)
	if r.Total() != 0 {
		t.Errorf("expected no missing items, got %d", r.Total())
	}
}

func TestComputeMissing_Degraded(t *testing.T) {
	// Everything appears missing because the check is degraded — the report
	// must carry the flag so callers skip the notification.
	docker := []Item{{Name: "web", InKuma: false}}
	r := ComputeMissing(docker, nil, true, []string{"no Kuma monitors fetched"})
	if !r.Degraded {
		t.Error("expected degraded flag")
	}
	if len(r.Reasons) != 1 || r.Reasons[0] != "no Kuma monitors fetched" {
		t.Errorf("expected degraded reason, got %v", r.Reasons)
	}
}

func TestComputeMissing_CapPerGroup(t *testing.T) {
	var docker []Item
	for i := 0; i < 45; i++ {
		docker = append(docker, Item{Name: "svc", InKuma: false})
	}
	r := ComputeMissing(docker, nil, false, nil)
	if len(r.Docker) != maxGroupItems+1 {
		t.Fatalf("expected %d entries (cap + truncation line), got %d", maxGroupItems+1, len(r.Docker))
	}
	if !strings.Contains(r.Docker[len(r.Docker)-1], "15 more") {
		t.Errorf("expected truncation line mentioning 15 more, got %q", r.Docker[len(r.Docker)-1])
	}
}

func TestFormatMessage(t *testing.T) {
	r := ComputeMissing(
		[]Item{{Name: "api", InKuma: false}, {Name: "db", InKuma: false}},
		[]Item{{Name: "example.com", InKuma: false, Instance: "npm-edge"}},
		false, nil,
	)
	title, body := FormatMessage(r)
	if title != "Synapse: 3 items missing from Uptime Kuma" {
		t.Errorf("unexpected title %q", title)
	}
	for _, want := range []string{"Docker services:", "- api", "- db", "NPM proxy hosts:", "- example.com (npm-edge)"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected body to contain %q, got:\n%s", want, body)
		}
	}
}

func TestFormatMessage_Empty(t *testing.T) {
	r := ComputeMissing(nil, nil, false, nil)
	title, body := FormatMessage(r)
	if title != "Synapse: 0 items missing from Uptime Kuma" {
		t.Errorf("unexpected title %q", title)
	}
	if body != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}
