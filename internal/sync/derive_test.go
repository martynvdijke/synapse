package sync

import (
	"testing"

	"synapse/internal/npm"
)

func TestServiceDomainsLabelPriority(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   []string
	}{
		{
			name:   "synapse.domains listed first, synapse.domain appended after",
			labels: map[string]string{"synapse.domains": "a.example.com,b.example.com", "synapse.domain": "c.example.com"},
			want:   []string{"a.example.com", "b.example.com", "c.example.com"},
		},
		{
			name:   "single synapse.domain",
			labels: map[string]string{"synapse.domain": "c.example.com"},
			want:   []string{"c.example.com"},
		},
		{
			name:   "npm.domains fallback",
			labels: map[string]string{"npm.domains": "n1.example.com,n2.example.com"},
			want:   []string{"n1.example.com", "n2.example.com"},
		},
		{
			name:   "traefik Host rule",
			labels: map[string]string{"traefik.http.routers.web.rule": "Host(`x.example.com`)"},
			want:   []string{"x.example.com"},
		},
		{
			name:   "traefik multi-host rule",
			labels: map[string]string{"traefik.http.routers.web.rule": "Host(`x.example.com`,`y.example.com`)"},
			want:   []string{"x.example.com", "y.example.com"},
		},
		{
			name:   "no labels",
			labels: nil,
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ServiceDomains("svc", ServiceDef{Labels: tt.labels})
			if len(got) != len(tt.want) {
				t.Fatalf("ServiceDomains() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("ServiceDomains() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestServiceDomainsDedupe(t *testing.T) {
	got := ServiceDomains("svc", ServiceDef{Labels: map[string]string{
		"synapse.domains": "a.example.com,a.example.com",
	}})
	if len(got) != 1 || got[0] != "a.example.com" {
		t.Fatalf("ServiceDomains() = %v, want [a.example.com]", got)
	}
}

func TestComposeAutheliaEntries(t *testing.T) {
	services := map[string]ServiceDef{
		"web": {
			Labels: map[string]string{"synapse.domains": "b.example.com,a.example.com"},
		},
		"api": {
			Labels: map[string]string{"synapse.domain": "c.example.com"},
		},
		"worker": {},
	}

	got := ComposeAutheliaEntries(services)
	want := []npm.ProxyEntry{
		{CNAME: "a.example.com", Container: "web"},
		{CNAME: "b.example.com", Container: "web"},
		{CNAME: "c.example.com", Container: "api"},
	}
	if len(got) != len(want) {
		t.Fatalf("ComposeAutheliaEntries() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ComposeAutheliaEntries() = %v, want %v", got, want)
		}
	}
}

func TestComposeAutheliaEntriesEmpty(t *testing.T) {
	got := ComposeAutheliaEntries(map[string]ServiceDef{})
	if len(got) != 0 {
		t.Fatalf("ComposeAutheliaEntries() = %v, want empty", got)
	}
}
