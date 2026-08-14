package sync

import (
	"regexp"
	"strings"

	"synapse/internal/npm"
)

// traefikHostRuleRe matches Host(...) host lists inside traefik router rule
// label values, e.g. Host(`example.com`) or Host(`a.example.com`,`b.example.com`).
var traefikHostRuleRe = regexp.MustCompile("(?i)Host\\(`([^`]+)`\\)")

// DeriveNPMHost derives the NPM proxy host configuration a compose service
// should have from its definition and labels:
//
//	domains:  synapse.domains (comma-separated), synapse.domain, npm.domains,
//	          npm.domain, then any traefik.http.routers.*.rule Host(...) labels
//	scheme:   synapse.scheme or npm.scheme (default "http")
//	port:     synapse.port label, else the first published port
//	host:     container_name, else the service name
//
// ok is false when no domain can be derived — the target is not derivable.
func DeriveNPMHost(name string, svc ServiceDef) (npm.ProxyHostCreate, bool) {
	domains := extractDomains(svc.Labels)
	if len(domains) == 0 {
		return npm.ProxyHostCreate{}, false
	}

	scheme := strings.TrimSpace(svc.Labels["synapse.scheme"])
	if scheme == "" {
		scheme = strings.TrimSpace(svc.Labels["npm.scheme"])
	}
	if scheme == "" {
		scheme = "http"
	}
	port := 0
	if p := strings.TrimSpace(svc.Labels["synapse.port"]); p != "" {
		port = parseFirstPort(p)
	}
	if port == 0 && len(svc.Ports) > 0 {
		port = parseFirstPort(svc.Ports[0])
	}
	host := svc.ContainerName
	if host == "" {
		host = name
	}
	return npm.ProxyHostCreate{
		DomainNames:   domains,
		ForwardScheme: scheme,
		ForwardHost:   host,
		ForwardPort:   port,
		Enabled:       true,
	}, true
}

// extractDomains collects candidate proxy-host domains from a service's
// labels, in priority order: synapse.domains, synapse.domain, npm.domains,
// npm.domain, then Host(...) rules in traefik.http.routers.*.rule labels.
// Duplicates are removed, order is preserved.
func extractDomains(labels map[string]string) []string {
	var domains []string
	seen := make(map[string]bool)
	add := func(d string) {
		if d = strings.TrimSpace(d); d != "" && !seen[d] {
			seen[d] = true
			domains = append(domains, d)
		}
	}

	if d := labels["synapse.domains"]; d != "" {
		for _, dd := range strings.Split(d, ",") {
			add(dd)
		}
	}
	add(labels["synapse.domain"])
	if d := labels["npm.domains"]; d != "" {
		for _, dd := range strings.Split(d, ",") {
			add(dd)
		}
	}
	add(labels["npm.domain"])

	for k, v := range labels {
		if strings.HasPrefix(k, "traefik.http.routers.") && strings.HasSuffix(k, ".rule") {
			for _, m := range traefikHostRuleRe.FindAllStringSubmatch(v, -1) {
				for _, dd := range strings.Split(m[1], ",") {
					add(strings.Trim(dd, "`'\""))
				}
			}
		}
	}
	return domains
}

// DeriveKumaMonitor derives the Kuma monitor spec for a service. HTTP monitors
// are used when the healthcheck exposes a URL; otherwise a docker monitor
// targets the container. interval is the healthcheck interval in seconds
// (default 60).
func DeriveKumaMonitor(name string, svc ServiceDef) (monitorType, url, container string, interval int) {
	return desiredMonitor(name, svc)
}
