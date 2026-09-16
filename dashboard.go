package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"synapse/internal/authelia"
	synclib "synapse/internal/sync"
)

var validDashboardSections = []string{"status", "services", "proxies", "monitors", "history", "links", "authelia", "npm_hosts"}
var validDashboardSet = func() map[string]bool {
	m := make(map[string]bool, len(validDashboardSections))
	for _, k := range validDashboardSections {
		m[k] = true
	}
	return m
}()

func (app *App) statusPayload(snap *DashboardSnapshot) gin.H {
	lastDocker, _ := app.database.GetLatestSyncRun("docker")
	lastNPM, _ := app.database.GetLatestSyncRun("npm")
	openIncidents := 0
	if n, err := app.database.CountOpenIncidents(); err == nil {
		openIncidents = n
	}
	connectionHealth := gin.H{
		"docker": gin.H{"ok": snap.DockerErr == "", "last_error": snap.DockerErr},
		"npm":    gin.H{"ok": snap.NPMErr == "", "last_error": snap.NPMErr, "instances": snap.NPMHealthList},
		"kuma":   gin.H{"ok": snap.KumaErr == "", "last_error": snap.KumaErr, "instances": snap.KumaHealthList},
	}
	return gin.H{
		"docker_count":      snap.DockerCount,
		"npm_count":         snap.NPMCount,
		"npm_error":         snap.NPMErr,
		"kuma_error":        snap.KumaErr,
		"docker_error":      snap.DockerErr,
		"monitor_count":     snap.MonitorCount,
		"last_docker":       lastDocker,
		"last_npm":          lastNPM,
		"running":           app.running,
		"open_incidents":    openIncidents,
		"connection_health": connectionHealth,
		"snapshot":          snapshotMeta(snap),
	}
}

func (app *App) serviceLinkViews() ([]ServiceLinkView, error) {
	links, err := app.database.GetServiceLinks()
	if err != nil {
		return nil, err
	}
	npmNames, kumaNames := app.instanceNameMaps()
	result := make([]ServiceLinkView, 0, len(links))
	for _, l := range links {
		result = append(result, toServiceLinkView(l, npmNames, kumaNames))
	}
	return result, nil
}

func (app *App) npmProxyHostViews(c *gin.Context) []NPMProxyHostView {
	return app.collectNPMHosts(c)
}

func (app *App) autheliaStatusPayload(c *gin.Context) gin.H {
	s := app.settings()
	if s.AutheliaConfigPath == "" {
		instances, err := app.database.GetEnabledAutheliaInstances()
		if err != nil || len(instances) == 0 {
			return gin.H{"configured": false, "message": "No Authelia instances configured"}
		}
		var allDomains []string
		var allNPMLists []string
		for _, inst := range instances {
			ac, err := authelia.ParseConfig(inst.ConfigPath)
			if err != nil {
				continue
			}
			allDomains = append(allDomains, authelia.GetDomains(ac)...)
			npmEntries := app.resolveNPMEntries(inst.NPMInstanceIDs)
			for _, e := range npmEntries {
				allNPMLists = append(allNPMLists, e.CNAME)
			}
		}
		for _, e := range app.composeAutheliaEntries() {
			allNPMLists = append(allNPMLists, e.CNAME)
		}
		matched, missing := authelia.CompareCNAMEs(allNPMLists, allDomains)
		return gin.H{"configured": true, "domains": allDomains, "npm_cnames": allNPMLists, "matched": matched, "missing": missing, "instance_count": len(instances)}
	}
	ac, err := authelia.ParseConfig(s.AutheliaConfigPath)
	if err != nil {
		return gin.H{"configured": true, "error": err.Error(), "domains": []string{}}
	}
	autheliaDomains := authelia.GetDomains(ac)
	var npmCNAMEs []string
	for _, e := range app.composeAutheliaEntries() {
		npmCNAMEs = append(npmCNAMEs, e.CNAME)
	}
	matched, missing := authelia.CompareCNAMEs(npmCNAMEs, autheliaDomains)
	openAlerts := 0
	if alerts, err := app.database.GetOpenAutheliaAlerts(0); err == nil {
		openAlerts = len(alerts)
	}
	return gin.H{"configured": true, "domains": autheliaDomains, "npm_cnames": npmCNAMEs, "matched": matched, "missing": missing, "open_alerts": openAlerts, "sync_enabled": s.AutheliaSyncEnabled, "default_policy": s.AutheliaDefaultPolicy}
}

func (app *App) syncHistoryPayload() (interface{}, error) {
	runs, err := app.database.GetSyncRuns(100)
	if err != nil {
		return nil, err
	}
	if runs == nil {
		return runs, nil
	}
	return runs, nil
}

func (app *App) Dashboard(c *gin.Context) {
	sectionsParam := c.Query("sections")
	var requested map[string]bool
	if sectionsParam != "" {
		parts := strings.Split(sectionsParam, ",")
		requested = make(map[string]bool, len(parts))
		for _, p := range parts {
			k := strings.TrimSpace(p)
			if k == "" {
				continue
			}
			if !validDashboardSet[k] {
				c.JSON(http.StatusBadRequest, gin.H{"error": "unknown section: " + k, "valid": validDashboardSections})
				return
			}
			requested[k] = true
		}
		if len(requested) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no valid sections", "valid": validDashboardSections})
			return
		}
	}
	include := func(k string) bool {
		if requested == nil {
			return true
		}
		return requested[k]
	}

	snap := app.getSnapshot()
	sections := gin.H{}

	var generatedAt string
	var version uint64
	var stale bool
	var lastError map[string]string

	if snap != nil {
		generatedAt = snap.GeneratedAt.Format(time.RFC3339)
		version = snap.Version
		stale = snapshotStale(snap)
		lastError = snap.LastError
		if include("status") {
			sections["status"] = app.statusPayload(snap)
		}
		if include("services") {
			if snap.Services == nil {
				sections["services"] = []synclib.ServiceInfo{}
			} else {
				sections["services"] = snap.Services
			}
		}
		if include("proxies") {
			if snap.Proxies == nil {
				sections["proxies"] = []synclib.ProxyInfo{}
			} else {
				sections["proxies"] = snap.Proxies
			}
		}
		if include("monitors") {
			if snap.Monitors == nil {
				sections["monitors"] = []KumaMonitorSummary{}
			} else {
				sections["monitors"] = snap.Monitors
			}
		}
	} else {
		app.requestSnapshotRefresh()
		stale = true
		lastError = map[string]string{"snapshot": "snapshot not ready"}
		generatedAt = time.Time{}.Format(time.RFC3339)
	}

	if include("history") {
		runs, err := app.database.GetSyncRuns(100)
		if err == nil {
			if runs == nil {
				sections["history"] = []interface{}{}
			} else {
				sections["history"] = runs
			}
		} else {
			sections["history"] = []interface{}{}
		}
	}
	if include("links") {
		views, err := app.serviceLinkViews()
		if err == nil {
			sections["links"] = views
			if views == nil {
				sections["links"] = []ServiceLinkView{}
			}
		} else {
			sections["links"] = []ServiceLinkView{}
		}
	}
	if include("authelia") {
		sections["authelia"] = app.autheliaStatusPayload(c)
	}
	if include("npm_hosts") {
		sections["npm_hosts"] = app.collectNPMHosts(c)
	}

	c.JSON(http.StatusOK, gin.H{
		"generated_at": generatedAt,
		"version":      version,
		"stale":        stale,
		"last_error":   lastError,
		"sections":     sections,
	})
}

func (app *App) collectNPMHosts(c *gin.Context) []NPMProxyHostView {
	// Bound the upstream fan-out so the aggregate request can never block on a
	// slow NPM instance (the client's own timeout is 30s).
	ctx, cancel := context.WithTimeout(c.Request.Context(), getEnvDuration("UPSTREAM_READ_DEADLINE", 2*time.Second))
	defer cancel()

	instances, _ := app.database.GetNPMInstances()
	nameMap := make(map[int]string)
	for _, inst := range instances {
		nameMap[int(inst.ID)] = inst.Name
	}
	clients, _ := app.npmRegistry.All()
	result := []NPMProxyHostView{}
	for _, ic := range clients {
		hosts, err := ic.Client.GetProxyHostsFull(ctx)
		if err != nil {
			continue
		}
		for _, h := range hosts {
			result = append(result, NPMProxyHostView{InstanceID: ic.InstanceID, InstanceName: nameMap[ic.InstanceID], ProxyHost: h})
		}
	}
	if result == nil {
		result = []NPMProxyHostView{}
	}
	return result
}
