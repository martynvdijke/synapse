package kuma

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// dataCollectionWindow is the hard ceiling for collecting post-login state.
// Event-aware completion normally closes much earlier (monitorList plus one
// heartbeatList per monitor); this ceiling only bounds a degraded instance.
// Kept as a var (not const) so tests can shorten the window.
var dataCollectionWindow = 8 * time.Second

// heartbeatSettleGrace is how long collection continues after the expected
// monitorList + heartbeatList set has arrived, so trailing uptime/avgPing
// events land too.
var heartbeatSettleGrace = 200 * time.Millisecond

type MonitorStats struct {
	ID          int     `json:"id"`
	Status      int     `json:"status"`
	StatusKnown bool    `json:"-"`
	Uptime24h   float64 `json:"uptime_24h"`
	Uptime7d    float64 `json:"uptime_7d"`
	Uptime1y    float64 `json:"uptime_1y"`
	AvgPing     float64 `json:"avg_ping"`
	LastMsg     string  `json:"last_msg,omitempty"`
	CertInfo    string  `json:"cert_info,omitempty"`
}

type MonitorTag struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Value string `json:"value"`
	Color string `json:"color,omitempty"`
}

type KumaMonitor struct {
	ID              int          `json:"id"`
	Name            string       `json:"name"`
	URL             string       `json:"url,omitempty"`
	Type            string       `json:"type"`
	DockerContainer string       `json:"docker_container,omitempty"`
	DockerHost      int          `json:"docker_host,omitempty"`
	Status          int          `json:"status"`
	StatusKnown     bool         `json:"-"`
	Uptime24h       float64      `json:"uptime_24h"`
	Uptime7d        float64      `json:"uptime_7d"`
	Uptime1y        float64      `json:"uptime_1y"`
	Ping            float64      `json:"ping"`
	LastMsg         string       `json:"last_msg,omitempty"`
	Interval        int          `json:"interval,omitempty"`
	RetryInterval   int          `json:"retryInterval,omitempty"`
	MaxRetries      int          `json:"maxretries,omitempty"`
	Active          bool         `json:"active"`
	Tags            []MonitorTag `json:"tags,omitempty"`
}

func parseID(raw json.RawMessage) (int, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		var n int
		if _, err := fmt.Sscanf(s, "%d", &n); err == nil {
			return n, true
		}
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n, true
	}
	return 0, false
}

// parseDuration normalizes a Kuma uptime duration arg to its canonical
// string form ("24", "720", "1y"). Kuma 2.5.0 sends durations as JSON
// numbers (24, 720, 8760) in the "uptime" event, but some versions/history
// events use strings ("24", "1y"). Accept both.
func parseDuration(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		switch n {
		case 24:
			return "24", true
		case 720:
			return "720", true
		case 8760:
			return "1y", true
		default:
			return fmt.Sprintf("%g", n), true
		}
	}
	return "", false
}

type monitorMeta struct {
	name            string
	url             string
	mtype           string
	dockerContainer string
	dockerHost      int
	interval        int
	retryInterval   int
	maxRetries      int
	active          bool
	activeSet       bool
	tags            []MonitorTag
}

// monitorCollector accumulates the state Kuma pushes after login.
//
// Status is derived ONLY from heartbeat data (the last row of heartbeatList,
// or a live heartbeat). Uptime percentages are not a status source: a monitor
// without any heartbeat is "unknown" (StatusKnown=false), never DOWN.
type monitorCollector struct {
	metas        map[int]monitorMeta
	statuses     map[int]int
	statusKnown  map[int]bool
	msgs         map[int]string
	upt24        map[int]float64
	upt7d        map[int]float64
	upt1y        map[int]float64
	pings        map[int]float64
	seen         map[int]bool
	hbSeen       map[int]bool
	uptSeen      map[int]bool
	listSeen     bool
	listCount    int
	tags         []MonitorTag
	dockerHosts  []DockerHost
	hostListSeen bool
}

func newMonitorCollector() *monitorCollector {
	return &monitorCollector{
		metas:       make(map[int]monitorMeta),
		statuses:    make(map[int]int),
		statusKnown: make(map[int]bool),
		msgs:        make(map[int]string),
		upt24:       make(map[int]float64),
		upt7d:       make(map[int]float64),
		upt1y:       make(map[int]float64),
		pings:       make(map[int]float64),
		seen:        make(map[int]bool),
		hbSeen:      make(map[int]bool),
		uptSeen:     make(map[int]bool),
	}
}

func (c *monitorCollector) handle(ev rawEvent) {
	switch ev.Name {
	case "monitorList":
		if len(ev.Args) < 1 {
			return
		}
		var list map[string]struct {
			ID              int          `json:"id"`
			Name            string       `json:"name"`
			URL             string       `json:"url"`
			Type            string       `json:"type"`
			DockerContainer string       `json:"docker_container"`
			DockerHost      int          `json:"docker_host"`
			Interval        int          `json:"interval"`
			RetryInterval   int          `json:"retryInterval"`
			MaxRetries      int          `json:"maxretries"`
			Active          *bool        `json:"active"`
			Tags            []MonitorTag `json:"tags"`
		}
		if json.Unmarshal(ev.Args[0], &list) != nil {
			return
		}
		c.listSeen = true
		for _, m := range list {
			active := true
			activeSet := false
			if m.Active != nil {
				active = *m.Active
				activeSet = true
			}
			c.metas[m.ID] = monitorMeta{
				name:            m.Name,
				url:             m.URL,
				mtype:           m.Type,
				dockerContainer: m.DockerContainer,
				dockerHost:      m.DockerHost,
				interval:        m.Interval,
				retryInterval:   m.RetryInterval,
				maxRetries:      m.MaxRetries,
				active:          active,
				activeSet:       activeSet,
				tags:            m.Tags,
			}
			c.seen[m.ID] = true
		}
		c.listCount = len(list)

	case "heartbeatList":
		if len(ev.Args) < 2 {
			return
		}
		id, ok := parseID(ev.Args[0])
		if !ok {
			return
		}
		var rows []struct {
			Status int    `json:"status"`
			Msg    string `json:"msg"`
		}
		if json.Unmarshal(ev.Args[1], &rows) != nil {
			return
		}
		c.seen[id] = true
		c.hbSeen[id] = true
		if len(rows) == 0 {
			return
		}
		last := rows[len(rows)-1]
		c.statuses[id] = last.Status
		c.statusKnown[id] = true
		if last.Msg != "" {
			c.msgs[id] = last.Msg
		}

	case "heartbeat":
		if len(ev.Args) < 1 {
			return
		}
		var hb struct {
			MonitorID int    `json:"monitorID"`
			Status    int    `json:"status"`
			Msg       string `json:"msg"`
		}
		if json.Unmarshal(ev.Args[0], &hb) != nil {
			return
		}
		c.seen[hb.MonitorID] = true
		c.statuses[hb.MonitorID] = hb.Status
		c.statusKnown[hb.MonitorID] = true
		if hb.Msg != "" {
			c.msgs[hb.MonitorID] = hb.Msg
		}

	case "uptime":
		if len(ev.Args) < 3 {
			return
		}
		id, ok := parseID(ev.Args[0])
		if !ok {
			return
		}
		dur, ok := parseDuration(ev.Args[1])
		if !ok {
			return
		}
		var val float64
		if json.Unmarshal(ev.Args[2], &val) != nil {
			return
		}
		c.seen[id] = true
		c.uptSeen[id] = true
		switch dur {
		case "24":
			c.upt24[id] = val
		case "720":
			c.upt7d[id] = val
		case "1y":
			c.upt1y[id] = val
		}

	case "avgPing":
		if len(ev.Args) < 2 {
			return
		}
		id, ok := parseID(ev.Args[0])
		if !ok {
			return
		}
		if string(ev.Args[1]) == "null" {
			return
		}
		var v float64
		if json.Unmarshal(ev.Args[1], &v) != nil {
			return
		}
		c.seen[id] = true
		c.pings[id] = v

	case "tagList":
		if len(ev.Args) < 1 {
			return
		}
		var tags []MonitorTag
		if json.Unmarshal(ev.Args[0], &tags) == nil {
			c.tags = tags
		}

	case "dockerHostList":
		if len(ev.Args) < 1 {
			return
		}
		var hosts []DockerHost
		if json.Unmarshal(ev.Args[0], &hosts) == nil {
			c.dockerHosts = hosts
			c.hostListSeen = true
		}
	}
}

// complete reports whether every announced monitor has a heartbeat entry.
func (c *monitorCollector) complete() bool {
	if !c.listSeen {
		return false
	}
	if c.listCount == 0 {
		return true
	}
	for id := range c.metas {
		if !c.hbSeen[id] {
			return false
		}
	}
	return true
}

func (c *monitorCollector) monitors() []KumaMonitor {
	var out []KumaMonitor
	for id := range c.seen {
		m := KumaMonitor{
			ID:          id,
			Uptime24h:   c.upt24[id],
			Uptime7d:    c.upt7d[id],
			Uptime1y:    c.upt1y[id],
			Ping:        c.pings[id],
			Status:      c.statuses[id],
			StatusKnown: c.statusKnown[id],
			LastMsg:     c.msgs[id],
		}
		if meta, ok := c.metas[id]; ok {
			m.Name = meta.name
			m.URL = meta.url
			m.Type = meta.mtype
			m.DockerContainer = meta.dockerContainer
			m.DockerHost = meta.dockerHost
			m.Interval = meta.interval
			m.RetryInterval = meta.retryInterval
			m.MaxRetries = meta.maxRetries
			if meta.activeSet {
				m.Active = meta.active
			} else {
				m.Active = true
			}
			m.Tags = meta.tags
		} else {
			m.Name = fmt.Sprintf("Monitor %d", id)
			m.Type = "?"
			m.Active = true
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// collect runs the collector over buffered + live events until the expected
// post-login state is complete (then a short settle grace) or the hard ceiling
// expires. The session is left in drain mode.
func (s *sioSession) collect(ctx context.Context, col *monitorCollector, complete func(*monitorCollector) bool) error {
	for _, ev := range s.takePending() {
		col.handle(ev)
	}

	var settle <-chan time.Time
	if complete(col) {
		settle = time.After(heartbeatSettleGrace)
	}
	ceiling := time.NewTimer(dataCollectionWindow)
	defer ceiling.Stop()

	for {
		select {
		case <-ctx.Done():
			s.startDrain()
			return ctx.Err()
		case <-s.cli.done:
			s.startDrain()
			return ErrSocketClosed
		case ev := <-s.events:
			col.handle(ev)
			if settle == nil && complete(col) {
				settle = time.After(heartbeatSettleGrace)
			}
		case <-settle:
			s.startDrain()
			return nil
		case <-ceiling.C:
			s.startDrain()
			return nil
		}
	}
}

// QueryMonitorsViaSocketIO fetches the monitor list via Socket.IO.
func QueryMonitorsViaSocketIO(kumaURL, username, password string) ([]KumaMonitor, error) {
	return QueryMonitorsViaSocketIOContext(context.Background(), kumaURL, username, password)
}

// QueryMonitorsViaSocketIOContext fetches the monitor list via Socket.IO,
// honoring ctx cancellation.
func QueryMonitorsViaSocketIOContext(ctx context.Context, kumaURL, username, password string) ([]KumaMonitor, error) {
	return queryMonitorsWithConfig(ctx, connConfig{URL: kumaURL, Username: username, Password: password})
}

// queryMonitorsWithConfig fetches the monitor list using the given connection
// config (including any persisted session token).
func queryMonitorsWithConfig(ctx context.Context, cfg connConfig) ([]KumaMonitor, error) {
	s, err := newSIOSession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer s.close()

	col := newMonitorCollector()
	if err := s.collect(ctx, col, (*monitorCollector).complete); err != nil {
		return nil, err
	}
	return col.monitors(), nil
}

// QueryDockerHostsViaSocketIO fetches the docker host list. Kuma pushes
// dockerHostList from afterLogin, so we look at pre-login buffers first and
// then the live stream up to the collection ceiling.
func QueryDockerHostsViaSocketIO(kumaURL, username, password string) ([]DockerHost, error) {
	return queryDockerHostsContext(context.Background(), kumaURL, username, password)
}

func queryDockerHostsContext(ctx context.Context, kumaURL, username, password string) ([]DockerHost, error) {
	s, err := newSIOSession(ctx, connConfig{URL: kumaURL, Username: username, Password: password})
	if err != nil {
		return nil, err
	}
	defer s.close()

	col := newMonitorCollector()
	if err := s.collect(ctx, col, func(c *monitorCollector) bool { return c.hostListSeen }); err != nil {
		return nil, err
	}
	return col.dockerHosts, nil
}

// GetMonitorStats fetches live-ish stats for a single monitor.
func GetMonitorStats(client *Client, monitorID int) (*MonitorStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	s, err := newSIOSession(ctx, connConfig{
		URL:      client.url,
		Username: client.username,
		Password: client.password,
		Token:    client.sessionToken(),
	})
	if err != nil {
		return nil, err
	}
	defer s.close()

	col := newMonitorCollector()
	if err := s.collect(ctx, col, func(c *monitorCollector) bool {
		return c.listSeen && (c.hbSeen[monitorID] || c.uptSeen[monitorID])
	}); err != nil {
		return nil, err
	}
	return &MonitorStats{
		ID:          monitorID,
		Status:      col.statuses[monitorID],
		StatusKnown: col.statusKnown[monitorID],
		Uptime24h:   col.upt24[monitorID],
		Uptime7d:    col.upt7d[monitorID],
		Uptime1y:    col.upt1y[monitorID],
		AvgPing:     col.pings[monitorID],
		LastMsg:     col.msgs[monitorID],
	}, nil
}
