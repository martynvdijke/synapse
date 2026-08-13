package notify

import (
	"fmt"
	"strings"
	"time"
)

const maxGroupItems = 30

// Item is a single entity (docker service or NPM proxy host) to check for
// Kuma coverage.
type Item struct {
	Name     string // display name (service name / CNAME)
	InKuma   bool
	Instance string // source NPM instance name, empty for docker services
}

// MissingReport describes what is not covered by Uptime Kuma at a point in time.
type MissingReport struct {
	Docker    []string  `json:"docker"`
	NPM       []string  `json:"npm"`
	FetchedAt time.Time `json:"fetched_at"`
	Degraded  bool      `json:"degraded"`
	Reasons   []string  `json:"reasons,omitempty"`
}

// Total returns the number of missing items across both groups.
func (r MissingReport) Total() int {
	return len(r.Docker) + len(r.NPM)
}

// ComputeMissing filters docker services and NPM proxy hosts that are not
// covered by Uptime Kuma (InKuma == false), grouping them into Docker and NPM
// lists capped at maxGroupItems per group. A degraded check (zero monitors
// enumerated, compose load failure, etc.) is surfaced via the Degraded flag so
// callers can skip sending misleading "everything is missing" alerts.
func ComputeMissing(docker []Item, npm []Item, degraded bool, reasons []string) MissingReport {
	report := MissingReport{
		FetchedAt: time.Now(),
		Degraded:  degraded,
		Reasons:   reasons,
		Docker:    []string{},
		NPM:       []string{},
	}

	for _, svc := range docker {
		if !svc.InKuma {
			report.Docker = append(report.Docker, svc.Name)
		}
	}
	for _, proxy := range npm {
		if !proxy.InKuma {
			name := proxy.Name
			if proxy.Instance != "" {
				name = fmt.Sprintf("%s (%s)", name, proxy.Instance)
			}
			report.NPM = append(report.NPM, name)
		}
	}

	report.Docker = truncate(report.Docker, maxGroupItems)
	report.NPM = truncate(report.NPM, maxGroupItems)
	return report
}

func truncate(items []string, max int) []string {
	if len(items) <= max {
		return items
	}
	head := make([]string, max, max+1)
	copy(head, items[:max])
	return append(head, fmt.Sprintf("…and %d more", len(items)-max))
}

// FormatMessage renders the notification title and body for a report.
// The title is "Synapse: N items missing from Uptime Kuma"; the body lists
// Docker services and NPM proxy hosts in separate groups.
func FormatMessage(r MissingReport) (title, body string) {
	title = fmt.Sprintf("Synapse: %d items missing from Uptime Kuma", r.Total())

	var b strings.Builder
	if len(r.Docker) > 0 {
		b.WriteString("Docker services:\n")
		for _, name := range r.Docker {
			b.WriteString("- ")
			b.WriteString(name)
			b.WriteString("\n")
		}
	}
	if len(r.NPM) > 0 {
		if len(r.Docker) > 0 {
			b.WriteString("\n")
		}
		b.WriteString("NPM proxy hosts:\n")
		for _, name := range r.NPM {
			b.WriteString("- ")
			b.WriteString(name)
			b.WriteString("\n")
		}
	}
	return title, strings.TrimRight(b.String(), "\n")
}
