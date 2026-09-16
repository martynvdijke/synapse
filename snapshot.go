package main

import (
	"context"
	"encoding/json"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	synclib "synapse/internal/sync"
)

type DashboardSnapshot struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Version     uint64            `json:"version"`
	LastError   map[string]string `json:"last_error,omitempty"`

	DockerCount    int     `json:"-"`
	DockerErr      string  `json:"-"`
	NPMCount       int     `json:"-"`
	NPMErr         string  `json:"-"`
	KumaErr        string  `json:"-"`
	MonitorCount   int     `json:"-"`
	NPMHealthList  []gin.H `json:"-"`
	KumaHealthList []gin.H `json:"-"`

	Services []synclib.ServiceInfo `json:"-"`
	Proxies  []synclib.ProxyInfo   `json:"-"`
	Monitors []KumaMonitorSummary  `json:"-"`
}

func (app *App) getSnapshot() *DashboardSnapshot {
	return app.snapshot.Load()
}

func snapshotStale(snap *DashboardSnapshot) bool {
	if snap == nil {
		return false
	}
	interval := getEnvDuration("SNAPSHOT_INTERVAL", 60*time.Second)
	threshold := getEnvDuration("SNAPSHOT_STALE_AFTER", 2*interval)
	return time.Since(snap.GeneratedAt) > threshold
}

func snapshotMeta(snap *DashboardSnapshot) gin.H {
	if snap == nil {
		return gin.H{}
	}
	m := gin.H{
		"generated_at": snap.GeneratedAt.Format(time.RFC3339),
		"version":      snap.Version,
		"stale":        snapshotStale(snap),
	}
	if len(snap.LastError) > 0 {
		m["last_error"] = snap.LastError
	}
	return m
}

func setSnapshotHeaders(c *gin.Context, snap *DashboardSnapshot) {
	if snap == nil {
		return
	}
	c.Header("X-Snapshot-Generated-At", snap.GeneratedAt.Format(time.RFC3339))
	c.Header("X-Snapshot-Version", strconv.FormatUint(snap.Version, 10))
	if snapshotStale(snap) {
		c.Header("X-Snapshot-Stale", "true")
	} else {
		c.Header("X-Snapshot-Stale", "false")
	}
	if len(snap.LastError) > 0 {
		if b, err := json.Marshal(snap.LastError); err == nil {
			c.Header("X-Snapshot-Last-Error", string(b))
		}
	}
}

func (app *App) queryKumaMonitors(ctx context.Context) ([]KumaMonitorSummary, bool) {
	clients, _ := app.kumaRegistry.All()
	instances, _ := app.database.GetKumaInstances()
	nameMap := make(map[int]string)
	for _, inst := range instances {
		nameMap[int(inst.ID)] = inst.Name
	}
	ok := len(clients) == 0 // no instances is a valid empty result, not a failure
	result := make([]KumaMonitorSummary, 0)
	for _, ic := range clients {
		monitors, err := ic.Client.QueryMonitorsViaSocketIOContext(ctx)
		if err != nil {
			continue
		}
		ok = true
		instanceName := nameMap[ic.InstanceID]
		for _, m := range monitors {
			result = append(result, KumaMonitorSummary{
				ID:              m.ID,
				Name:            m.Name,
				Type:            m.Type,
				URL:             m.URL,
				DockerContainer: m.DockerContainer,
				Status:          m.Status,
				Uptime24h:       m.Uptime24h,
				Uptime7d:        m.Uptime7d,
				Uptime1y:        m.Uptime1y,
				AvgPing:         m.Ping,
				LastMsg:         m.LastMsg,
				Interval:        m.Interval,
				RetryInterval:   m.RetryInterval,
				MaxRetries:      m.MaxRetries,
				Active:          m.Active,
				Tags:            m.Tags,
				InstanceID:      ic.InstanceID,
				InstanceName:    instanceName,
			})
		}
	}
	if result == nil {
		result = []KumaMonitorSummary{}
	}
	return result, ok
}

func (app *App) buildSnapshot(ctx context.Context) *DashboardSnapshot {
	prev := app.getSnapshot()
	snap := &DashboardSnapshot{}
	if prev != nil {
		snap.Services = prev.Services
		snap.Proxies = prev.Proxies
		snap.Monitors = prev.Monitors
		snap.DockerCount = prev.DockerCount
		snap.DockerErr = prev.DockerErr
		snap.NPMCount = prev.NPMCount
		snap.NPMErr = prev.NPMErr
		snap.KumaErr = prev.KumaErr
		snap.MonitorCount = prev.MonitorCount
		snap.NPMHealthList = prev.NPMHealthList
		snap.KumaHealthList = prev.KumaHealthList
	}
	snap.LastError = make(map[string]string)

	s := app.settings()
	clients, _ := app.kumaRegistry.All()
	npmClients, _ := app.npmRegistry.All()
	kumaInstances, _ := app.database.GetEnabledKumaInstances()
	npmInstances, _ := app.database.GetEnabledNPMInstances()

	var wg sync.WaitGroup
	var mu sync.Mutex

	wg.Add(1)
	go func() {
		defer wg.Done()
		services, err := synclib.LoadServices(s.ComposePath)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			snap.LastError["docker"] = err.Error()
		} else {
			snap.DockerCount = len(services)
			snap.DockerErr = ""
			delete(snap.LastError, "docker")
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		proxies, err := synclib.GetNPMProxiesWithStatus(ctx, npmClients, clients)
		mu.Lock()
		defer mu.Unlock()
		if err != nil && len(proxies) == 0 {
			snap.LastError["npm"] = err.Error()
		} else {
			if len(proxies) > 0 {
				snap.Proxies = proxies
				snap.NPMCount = len(proxies)
			} else if err == nil {
				snap.Proxies = []synclib.ProxyInfo{}
				snap.NPMCount = 0
			}
			if err != nil {
				snap.LastError["npm"] = err.Error()
				snap.NPMErr = err.Error()
			} else {
				snap.NPMErr = ""
				delete(snap.LastError, "npm")
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		list := make([]gin.H, 0, len(kumaInstances))
		errStr := ""
		for _, inst := range kumaInstances {
			instErr := ""
			if _, err := app.kumaRegistry.Get(int(inst.ID)); err != nil {
				instErr = err.Error()
			}
			list = append(list, gin.H{"id": inst.ID, "name": inst.Name, "ok": instErr == "", "last_error": instErr})
			if instErr != "" {
				if errStr == "" {
					errStr = instErr
				} else {
					errStr = inst.Name + ": " + instErr
				}
			}
		}
		if len(kumaInstances) == 0 {
			errStr = "no Kuma instances configured"
		}
		mu.Lock()
		snap.KumaHealthList = list
		snap.KumaErr = errStr
		mu.Unlock()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		total, _ := app.kumaMonitorCountWithContext(ctx, clients)
		mu.Lock()
		snap.MonitorCount = total
		mu.Unlock()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		list := make([]gin.H, 0, len(npmInstances))
		for _, inst := range npmInstances {
			errMsg := ""
			cl, err := app.npmRegistry.Get(int(inst.ID))
			if err != nil {
				errMsg = err.Error()
			} else if _, err := cl.GetProxyHosts(ctx); err != nil {
				errMsg = err.Error()
			}
			list = append(list, gin.H{"id": inst.ID, "name": inst.Name, "ok": errMsg == "", "last_error": errMsg})
		}
		mu.Lock()
		snap.NPMHealthList = list
		mu.Unlock()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		result, err := synclib.GetDockerServicesWithStatus(ctx, s.ComposePath, clients)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			snap.LastError["services"] = err.Error()
			return
		}
		if result == nil {
			result = []synclib.ServiceInfo{}
		}
		app.enrichWithContainerState(ctx, result)
		snap.Services = result
		delete(snap.LastError, "services")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		monitors, ok := app.queryKumaMonitors(ctx)
		mu.Lock()
		defer mu.Unlock()
		if ok {
			snap.Monitors = monitors
			delete(snap.LastError, "monitors")
		} else {
			// Keep the previous monitor list rather than wiping it.
			snap.LastError["monitors"] = "all Kuma monitor queries failed"
		}
	}()

	wg.Wait()

	if len(snap.LastError) == 0 {
		snap.LastError = nil
	}
	if snap.Services == nil {
		snap.Services = []synclib.ServiceInfo{}
	}
	if snap.Proxies == nil {
		snap.Proxies = []synclib.ProxyInfo{}
	}
	if snap.Monitors == nil {
		snap.Monitors = []KumaMonitorSummary{}
	}
	if snap.NPMHealthList == nil {
		snap.NPMHealthList = []gin.H{}
	}
	if snap.KumaHealthList == nil {
		snap.KumaHealthList = []gin.H{}
	}

	// Section failures retained their previous values above. If any section
	// failed, keep the last good snapshot's data, version, and timestamp so
	// `stale` reflects the last fully-successful build, and expose the error.
	if len(snap.LastError) > 0 && prev != nil {
		retained := *prev
		retained.LastError = snap.LastError
		app.snapshot.Store(&retained)
		return &retained
	}

	var ver uint64
	if prev != nil {
		ver = prev.Version + 1
	} else {
		ver = 1
	}
	snap.Version = ver
	snap.GeneratedAt = time.Now()
	app.snapshot.Store(snap)
	atomic.AddUint64(&app.snapshotVersion, 1)
	return snap
}

func (app *App) requestSnapshotRefresh() {
	select {
	case app.snapshotRefreshCh <- struct{}{}:
	default:
	}
}

func (app *App) startSnapshotRefresher(ctx context.Context) {
	runBuild := func() {
		if !app.snapshotMu.TryLock() {
			return
		}
		bctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		app.buildSnapshot(bctx)
		cancel()
		app.snapshotMu.Unlock()
	}

	runBuild() // initial build (runs under the lock; honors shutdown ctx)

	interval := getEnvDuration("SNAPSHOT_INTERVAL", 60*time.Second)
	if interval <= 0 {
		interval = 60 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			jitter := time.Duration(rand.Int63n(int64(interval)/10 + 1))
			if jitter > 0 {
				timer := time.NewTimer(jitter)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			runBuild()
		case <-app.snapshotRefreshCh:
			// Sync-triggered refresh: wait for any in-flight build rather than
			// dropping the request, so post-sync data is never missed.
			app.snapshotMu.Lock()
			bctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			app.buildSnapshot(bctx)
			cancel()
			app.snapshotMu.Unlock()
		}
	}
}
