package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"synapse/ent/schema"
	"synapse/internal/db"
	"synapse/internal/kuma"
	"synapse/internal/logging"
	"synapse/internal/npm"
)

// ReconcileOptions controls a reconciliation run.
type ReconcileOptions struct {
	// DryRun reports intended changes without applying them.
	DryRun bool
	// OnlyService restricts the run to a single compose service. Empty means
	// all linked services.
	OnlyService string
}

// ReconcileChange is a single intended or applied change.
type ReconcileChange struct {
	Service string `json:"service"`
	Target  string `json:"target"` // "npm" or "kuma"
	Action  string `json:"action"` // "created", "updated", "skipped", "error"
	Detail  string `json:"detail,omitempty"`
}

// ReconcileResult is the outcome of a reconcile run.
type ReconcileResult struct {
	Run     db.SyncRun        `json:"run"`
	Changes []ReconcileChange `json:"changes"`
	DryRun  bool              `json:"dry_run"`
}

// RunReconcile compares the desired state derived from the compose file against
// the live NPM proxy hosts and Kuma monitors of linked services, creating and
// updating targets that drift. Only services with a ServiceLink reconcile —
// unlinked services keep the existing coverage-based alerting.
func RunReconcile(ctx context.Context, composePath string, npmClients []npm.InstanceClient, kumaInstances []kuma.InstanceResult, database *db.DB, opts ReconcileOptions, onProgress ProgressFn) ReconcileResult {
	_, span := tracer.Start(ctx, "RunReconcile",
		trace.WithAttributes(attribute.String("compose_path", composePath)),
	)
	defer span.End()

	logging.LogInfo("sync", "Starting reconcile",
		slog.String("compose_path", composePath),
		slog.Bool("dry_run", opts.DryRun),
		slog.String("only_service", opts.OnlyService),
	)

	run := db.SyncRun{
		Source:    "reconcile",
		Status:    "running",
		StartedAt: time.Now(),
		DryRun:    opts.DryRun,
	}
	id, err := database.CreateSyncRun(&run)
	if err != nil {
		logging.LogError("sync", "Failed to create reconcile run",
			slog.String("error", err.Error()),
		)
		return ReconcileResult{Run: db.SyncRun{Source: "reconcile", Status: "error", ErrorMessage: err.Error()}, DryRun: opts.DryRun}
	}
	run.ID = id

	result := ReconcileResult{Run: run, DryRun: opts.DryRun}
	var added, updated, skipped, failed int
	var changes []ReconcileChange
	var kumaInstanceErrors []schema.SyncInstanceError
	// Progress reporting is optional: the HTTP and scheduled callers pass nil.
	emit := func(p Progress) {
		if onProgress != nil {
			onProgress(p)
		}
	}
	addChange := func(service, target, action, detail string) {
		changes = append(changes, ReconcileChange{Service: service, Target: target, Action: action, Detail: detail})
	}
	finish := func(status, msg string) {
		if err := database.FinishReconcileRunWithErrors(id, status, added, updated, skipped, failed, msg, kumaInstanceErrors); err != nil {
			logging.LogWarn("sync", "Failed to finish reconcile run",
				slog.String("error", err.Error()),
			)
		}
		now := time.Now()
		result.Run.Status = status
		result.Run.FinishedAt = &now
		result.Run.ErrorMessage = msg
		result.Run.Added = added
		result.Run.Updated = updated
		result.Run.Skipped = skipped
		result.Run.Failed = failed
		result.Run.InstanceErrors = kumaInstanceErrors
		result.Changes = changes
	}

	services, err := LoadServices(composePath)
	if err != nil {
		finish("error", err.Error())
		return result
	}

	links, err := database.GetServiceLinks()
	if err != nil {
		finish("error", err.Error())
		return result
	}

	linked := make([]db.ServiceLink, 0, len(links))
	for _, l := range links {
		if opts.OnlyService != "" && l.ServiceName != opts.OnlyService {
			continue
		}
		linked = append(linked, l)
	}
	run.TotalServices = len(linked)
	result.Run.TotalServices = len(linked)

	if len(linked) == 0 {
		msg := "no service links configured"
		if opts.OnlyService != "" {
			msg = "service not linked"
			skipped = 1
		}
		finish("completed", msg)
		return result
	}

	emit(Progress{RunID: id, Source: "reconcile", Total: len(linked), Status: "fetching", Message: "Fetching live NPM hosts and Kuma monitors..."})

	// Fetch live state once per instance, tolerating per-instance failures.
	npmByInst := map[int]*npm.Client{}
	npmLive := map[int][]npm.ProxyHost{}
	for _, ic := range npmClients {
		npmByInst[ic.InstanceID] = ic.Client
		hosts, err := ic.Client.GetProxyHostsFull(ctx)
		if err != nil {
			logging.LogWarn("sync", "Failed to fetch NPM proxy hosts",
				slog.Int("instance_id", ic.InstanceID),
				slog.String("error", err.Error()),
			)
			continue
		}
		npmLive[ic.InstanceID] = hosts
	}
	kumaAPI := map[int]kuma.API{}
	kumaIndex := map[int]*MonitorIndex{}
	kumaErr := map[int]string{}
	kumaName := map[int]string{}
	kumaExpected := monitorCountByInstance(database)
	for _, res := range kumaInstances {
		kumaName[res.InstanceID] = res.Name
		if res.Err != nil {
			kumaErr[res.InstanceID] = res.Err.Error()
			continue
		}
		if res.Client == nil {
			kumaErr[res.InstanceID] = "no API handle returned"
			continue
		}
		monitors, err := res.Client.Monitors(ctx)
		if err != nil {
			logging.LogWarn("sync", "Failed to fetch Kuma monitors",
				slog.Int("instance_id", res.InstanceID),
				slog.String("error", err.Error()),
			)
			kumaErr[res.InstanceID] = err.Error()
			continue
		}
		if len(monitors) == 0 && kumaExpected[res.InstanceID] > 0 {
			// A partial/failed read must not be mistaken for a clean slate, or
			// reconcile would recreate every monitor on the instance.
			msg := fmt.Sprintf("monitor list came back empty but %d monitor(s) are recorded", kumaExpected[res.InstanceID])
			logging.LogWarn("sync", "Suspicious empty Kuma monitor list",
				slog.Int("instance_id", res.InstanceID),
				slog.String("detail", msg),
			)
			kumaErr[res.InstanceID] = msg
			continue
		}
		kumaAPI[res.InstanceID] = res.Client
		kumaIndex[res.InstanceID] = BuildMonitorIndex(monitors)
	}
	reportedKumaErr := map[int]bool{}
	noteKumaUnavailable := func(instanceID int) string {
		msg := kumaErr[instanceID]
		if msg == "" {
			msg = "instance not configured"
		}
		if !reportedKumaErr[instanceID] {
			reportedKumaErr[instanceID] = true
			kumaInstanceErrors = append(kumaInstanceErrors, schema.SyncInstanceError{
				InstanceID: instanceID,
				Name:       kumaName[instanceID],
				Error:      msg,
			})
		}
		return msg
	}

	for i, link := range linked {
		svc, ok := services[link.ServiceName]
		if !ok {
			skipped++
			addChange(link.ServiceName, "npm", "skipped", "service not in compose file")
			emit(Progress{RunID: id, Source: "reconcile", Total: len(linked), Current: i + 1, Status: "skipped", Message: link.ServiceName, Skipped: skipped})
			continue
		}

		// NPM proxy host side.
		if link.NPMInstanceID > 0 {
			cfg, ok := desiredNPMHost(link.ServiceName, svc)
			if !ok {
				skipped++
				addChange(link.ServiceName, "npm", "skipped", "no synapse.domains label")
			} else {
				live := findProxyHost(npmLive[link.NPMInstanceID], link, cfg)
				switch {
				case live == nil:
					addChange(link.ServiceName, "npm", "created", strings.Join(cfg.DomainNames, ", "))
					if !opts.DryRun {
						c := npmByInst[link.NPMInstanceID]
						if c == nil {
							failed++
							changes[len(changes)-1].Action = "error"
							changes[len(changes)-1].Detail = "npm instance not configured"
						} else if _, err := c.CreateProxyHost(cfg); err != nil {
							failed++
							changes[len(changes)-1].Action = "error"
							changes[len(changes)-1].Detail = err.Error()
						} else {
							added++
						}
					} else {
						added++
					}
				default:
					drift := npmHostDrift(*live, cfg)
					if len(drift) > 0 {
						addChange(link.ServiceName, "npm", "updated", strings.Join(drift, ", "))
						if !opts.DryRun {
							c := npmByInst[link.NPMInstanceID]
							if c == nil {
								failed++
								changes[len(changes)-1].Action = "error"
								changes[len(changes)-1].Detail = "npm instance not configured"
							} else if _, err := c.UpdateProxyHost(live.ID, cfg); err != nil {
								failed++
								changes[len(changes)-1].Action = "error"
								changes[len(changes)-1].Detail = err.Error()
							} else {
								updated++
							}
						} else {
							updated++
						}
					}
				}
			}
		}

		// Kuma monitor side.
		if link.KumaInstanceID > 0 {
			monitorType, url, container, interval := desiredMonitor(link.ServiceName, svc)
			display := svc.ContainerName
			if display == "" {
				display = link.ServiceName
			}
			ix, available := kumaIndex[link.KumaInstanceID]
			if !available {
				// No trustworthy monitor list for this instance: never create
				// (that is exactly how a partial read becomes a duplicate storm).
				failed++
				addChange(link.ServiceName, "kuma", "error", "kuma instance unavailable: "+noteKumaUnavailable(link.KumaInstanceID))
			} else {
				eid := ExternalID{Type: monitorType, Service: link.ServiceName}
				live := findKumaMonitor(ix, eid, link, display)
				if live != nil && !live.Active {
					skipped++
					addChange(link.ServiceName, "kuma", "skipped", "paused")
					continue
				}
				switch {
				case live == nil:
					addChange(link.ServiceName, "kuma", "created", display)
					if !opts.DryRun {
						c := kumaAPI[link.KumaInstanceID]
						spec := kuma.MonitorSpec{
							Type:        monitorType,
							Name:        display,
							Interval:    interval,
							Description: Marker(monitorType, link.ServiceName),
							Tags:        []string{SynapseTagName},
						}
						if monitorType == "http" {
							spec.URL = url
						} else {
							spec.DockerContainer = container
						}
						newID, err := c.AddMonitor(ctx, spec)
						if err != nil {
							failed++
							changes[len(changes)-1].Action = "error"
							changes[len(changes)-1].Detail = err.Error()
						} else {
							added++
							if uerr := database.UpsertMonitor(&db.Monitor{
								Name:            display,
								ServiceName:     link.ServiceName,
								MonitorType:     monitorType,
								URL:             url,
								DockerContainer: container,
								KumaID:          newID,
								KumaInstanceID:  link.KumaInstanceID,
								CreatedAt:       time.Now(),
							}); uerr != nil {
								logging.LogWarn("sync", "Failed to persist reconciled monitor",
									slog.String("service", link.ServiceName),
									slog.String("error", uerr.Error()),
								)
							}
							applyDefaultKumaTags(database, link.KumaInstanceID, newID)
						}
					} else {
						added++
					}
				default:
					drift := monitorDrift(*live, monitorType, url, container, interval)
					if len(drift) > 0 {
						addChange(link.ServiceName, "kuma", "updated", strings.Join(drift, ", "))
						if !opts.DryRun {
							c := kumaAPI[link.KumaInstanceID]
							spec := kuma.MonitorSpec{
								Type:        monitorType,
								Interval:    interval,
								Description: Marker(monitorType, link.ServiceName),
							}
							if monitorType == "http" {
								spec.URL = url
							} else {
								spec.DockerContainer = container
							}
							if err := c.EditMonitor(ctx, live.ID, spec); err != nil {
								failed++
								changes[len(changes)-1].Action = "error"
								changes[len(changes)-1].Detail = err.Error()
							} else {
								updated++
								if uerr := database.UpsertMonitor(&db.Monitor{
									Name:            display,
									ServiceName:     link.ServiceName,
									MonitorType:     monitorType,
									URL:             url,
									DockerContainer: container,
									KumaID:          live.ID,
									KumaInstanceID:  link.KumaInstanceID,
									CreatedAt:       time.Now(),
								}); uerr != nil {
									logging.LogWarn("sync", "Failed to persist reconciled monitor",
										slog.String("service", link.ServiceName),
										slog.String("error", uerr.Error()),
									)
								}
							}
						} else {
							updated++
						}
					}
				}
			}
		}

		emit(Progress{RunID: id, Source: "reconcile", Total: len(linked), Current: i + 1, Status: "processing", Message: link.ServiceName, Added: added, Skipped: skipped, Failed: failed})
	}

	status := "completed"
	if failed > 0 {
		status = "completed_with_errors"
	}
	finish(status, "")
	return result
}

// desiredNPMHost derives the NPM proxy host a service should have from the
// compose definition and synapse.* labels:
//
//	domains:  synapse.domains (comma-separated) or synapse.domain
//	scheme:   synapse.scheme (default "http")
//	port:     synapse.port label, else the first published port
//	host:     container_name, else the service name
//
// ok is false when no domain label is present — the target is not derivable.
func desiredNPMHost(name string, svc ServiceDef) (npm.ProxyHostCreate, bool) {
	var domains []string
	if d := strings.TrimSpace(svc.Labels["synapse.domains"]); d != "" {
		for _, dd := range strings.Split(d, ",") {
			if dd = strings.TrimSpace(dd); dd != "" {
				domains = append(domains, dd)
			}
		}
	}
	if d := strings.TrimSpace(svc.Labels["synapse.domain"]); d != "" {
		domains = append(domains, d)
	}
	if len(domains) == 0 {
		return npm.ProxyHostCreate{}, false
	}

	scheme := strings.TrimSpace(svc.Labels["synapse.scheme"])
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

// desiredMonitor derives the Kuma monitor spec for a service. HTTP monitors
// are used when the healthcheck exposes a URL; otherwise a docker monitor
// targets the container. interval is the healthcheck interval in seconds
// (default 60).
func desiredMonitor(name string, svc ServiceDef) (monitorType, url, container string, interval int) {
	display := svc.ContainerName
	if display == "" {
		display = name
	}
	interval = 60
	if svc.HealthCheck != nil {
		if s := intervalSeconds(svc.HealthCheck.Interval); s > 0 {
			interval = s
		}
		if u := ParseHealthcheck(name, svc); u != "" {
			return "http", u, "", interval
		}
	}
	return "docker", "", display, interval
}

// intervalSeconds converts a compose duration string ("30s", "1m") to seconds.
// Returns 0 when empty or unparsable.
func intervalSeconds(spec string) int {
	if spec == "" {
		return 0
	}
	d, err := time.ParseDuration(spec)
	if err != nil {
		return 0
	}
	secs := int(d.Seconds())
	if secs < 1 {
		return 0
	}
	return secs
}

var portRangeRe = regexp.MustCompile(`^(\d+)-\d+$`)

// parseFirstPort extracts the published (host) port from a compose port spec.
// Handles "80:80", "443:8443/tcp", long-syntax "published:target" (already
// normalized) and ranges ("8000-8005:80" → 8000). Returns 0 when unparsable.
func parseFirstPort(spec string) int {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0
	}
	if i := strings.IndexByte(spec, '/'); i >= 0 {
		spec = spec[:i]
	}
	if i := strings.IndexByte(spec, ':'); i >= 0 {
		spec = spec[:i]
	}
	if m := portRangeRe.FindStringSubmatch(spec); m != nil {
		spec = m[1]
	}
	n, err := strconv.Atoi(spec)
	if err != nil {
		return 0
	}
	return n
}

// npmHostDrift lists fields where the live NPM host differs from the desired
// configuration. A zero desired forward_port is treated as "no opinion".
func npmHostDrift(live npm.ProxyHost, cfg npm.ProxyHostCreate) []string {
	var d []string
	if live.ForwardHost != cfg.ForwardHost {
		d = append(d, "forward_host")
	}
	if cfg.ForwardPort != 0 && live.ForwardPort != cfg.ForwardPort {
		d = append(d, "forward_port")
	}
	if live.ForwardScheme != cfg.ForwardScheme {
		d = append(d, "forward_scheme")
	}
	if live.Enabled != cfg.Enabled {
		d = append(d, "enabled")
	}
	if !domainSetsEqual(live.DomainNames, cfg.DomainNames) {
		d = append(d, "domain_names")
	}
	return d
}

// monitorDrift lists fields where the live Kuma monitor differs from the
// desired spec. A non-positive desired interval is treated as "no opinion".
func monitorDrift(live kuma.KumaMonitor, monitorType, url, container string, interval int) []string {
	var d []string
	if live.Type != monitorType {
		d = append(d, "type")
	}
	if monitorType == "http" && live.URL != url {
		d = append(d, "url")
	}
	if monitorType == "docker" && live.DockerContainer != container {
		d = append(d, "docker_container")
	}
	if interval > 0 && live.Interval != interval {
		d = append(d, "interval")
	}
	return d
}

func domainSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, d := range a {
		seen[d] = true
	}
	for _, d := range b {
		if !seen[d] {
			return false
		}
	}
	return true
}

// findProxyHost locates the live NPM host matching a link. When the link pins
// NPMHostName, only that domain name matches; otherwise the first desired
// domain name is used.
func findProxyHost(hosts []npm.ProxyHost, link db.ServiceLink, cfg npm.ProxyHostCreate) *npm.ProxyHost {
	for i := range hosts {
		h := &hosts[i]
		if link.NPMHostName != "" {
			for _, dn := range h.DomainNames {
				if dn == link.NPMHostName {
					return h
				}
			}
			continue
		}
		if len(cfg.DomainNames) > 0 {
			for _, dn := range h.DomainNames {
				if dn == cfg.DomainNames[0] {
					return h
				}
			}
		}
	}
	return nil
}

// parseKumaDefaultTags parses kuma_default_tags setting into tag IDs.
func parseKumaDefaultTags(raw string) []int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var arr []int
		if err := json.Unmarshal([]byte(raw), &arr); err == nil {
			return arr
		}
		var sarr []string
		if err := json.Unmarshal([]byte(raw), &sarr); err == nil {
			var out []int
			for _, s := range sarr {
				if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
					out = append(out, n)
				}
			}
			return out
		}
		return nil
	}
	var out []int
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if n, err := strconv.Atoi(p); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// findKumaMonitor locates the live Kuma monitor matching a link. An explicit
// KumaMonitorID wins, then a pinned name, then the external-id index (with the
// derived display name as the final fallback).
func findKumaMonitor(ix *MonitorIndex, id ExternalID, link db.ServiceLink, displayName string) *kuma.KumaMonitor {
	if ix == nil {
		return nil
	}
	if link.KumaMonitorID > 0 {
		return ix.ByID(link.KumaMonitorID)
	}
	if link.KumaMonitorName != "" {
		for i := range ix.monitors {
			if ix.monitors[i].Name == link.KumaMonitorName {
				return &ix.monitors[i]
			}
		}
		return nil
	}
	return ix.Find(id, displayName)
}

// applyDefaultKumaTags applies the kuma_default_tags setting (tag ids) to a
// freshly created monitor. The API seam exposes tag names, not ids, so this
// compatibility shim keeps the existing setting working by reading the
// instance credentials and using the tag-id command path. Best-effort.
func applyDefaultKumaTags(database *db.DB, instanceID, monitorID int) {
	tagIDs := parseKumaDefaultTags(database.GetSettings(db.Settings{}).KumaDefaultTags)
	if len(tagIDs) == 0 || monitorID <= 0 {
		return
	}
	inst, err := database.GetKumaInstance(int64(instanceID))
	if err != nil || inst == nil {
		return
	}
	for _, tagID := range tagIDs {
		if err := kuma.AddMonitorTagViaSocketIO(inst.URL, inst.Username, inst.Password, monitorID, tagID); err != nil {
			logging.LogWarn("sync", "Failed to add default tag to monitor",
				slog.Int("monitor_id", monitorID),
				slog.Int("tag_id", tagID),
				slog.String("error", err.Error()),
			)
		}
	}
}
