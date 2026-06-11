package kuma

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"synapse/internal/logging"
)

// Minimal Engine.IO v4 / Socket.IO v4 client — only enough for Uptime Kuma.

type eioType byte

const (
	eioOpen    eioType = '0'
	eioClose   eioType = '1'
	eioPing    eioType = '2'
	eioPong    eioType = '3'
	eioMessage eioType = '4'
)

type sioType byte

const (
	sioConnect sioType = '0'
	sioEvent   sioType = '2'
	sioAck     sioType = '3'
)

type rawEvent struct {
	Name string
	Args []json.RawMessage
}

type sioClient struct {
	conn        *websocket.Conn
	mu          sync.Mutex
	ackID       int
	pendingAcks map[int]chan []json.RawMessage
	onEvent     func(rawEvent)
	done        chan struct{}
}

func dialSIO(serverURL string) (*sioClient, error) {
	start := time.Now()
	scheme := "ws"
	if len(serverURL) > 5 && serverURL[:5] == "https" {
		scheme = "wss"
	}
	u := fmt.Sprintf("%s://%s/socket.io/?EIO=4&transport=websocket", scheme, serverURL)
	if len(serverURL) > 7 && serverURL[:7] == "http://" {
		u = fmt.Sprintf("ws://%s/socket.io/?EIO=4&transport=websocket", serverURL[7:])
	} else if len(serverURL) > 8 && serverURL[:8] == "https://" {
		u = fmt.Sprintf("wss://%s/socket.io/?EIO=4&transport=websocket", serverURL[8:])
	}

	logging.LogDebug("kuma", "Dialing Socket.IO",
		slog.String("url", serverURL),
		slog.String("ws_url", u),
	)

	dialer := &websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	c, _, err := dialer.Dial(u, nil)
	if err != nil {
		logging.LogError("kuma", "Socket.IO dial failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, fmt.Errorf("ws dial: %w", err)
	}

	cli := &sioClient{
		conn:        c,
		pendingAcks: make(map[int]chan []json.RawMessage),
		done:        make(chan struct{}),
	}

	// Read Engine.IO OPEN
	_, msg, err := c.ReadMessage()
	if err != nil {
		c.Close()
		logging.LogError("kuma", "Socket.IO read open failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, fmt.Errorf("read eio open: %w", err)
	}
	if len(msg) == 0 || eioType(msg[0]) != eioOpen {
		c.Close()
		logging.LogError("kuma", "Socket.IO unexpected open message",
			slog.String("msg", string(msg)),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, fmt.Errorf("expected eio open, got %q", string(msg))
	}

	var open struct {
		Sid          string `json:"sid"`
		PingInterval int    `json:"pingInterval"`
		PingTimeout  int    `json:"pingTimeout"`
	}
	if err := json.Unmarshal(msg[1:], &open); err != nil {
		c.Close()
		logging.LogError("kuma", "Socket.IO parse open failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, fmt.Errorf("parse eio open: %w", err)
	}

	// Send Socket.IO CONNECT: Engine.IO MESSAGE(4) + SIO CONNECT(0) = "40"
	if err := c.WriteMessage(websocket.TextMessage, []byte("40")); err != nil {
		c.Close()
		logging.LogError("kuma", "Socket.IO send connect failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, fmt.Errorf("send sio connect: %w", err)
	}

	// Read SIO CONNECT response
	_, msg, err = c.ReadMessage()
	if err != nil {
		c.Close()
		logging.LogError("kuma", "Socket.IO read connect response failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, fmt.Errorf("read sio connect: %w", err)
	}
	if len(msg) < 2 || msg[0] != '4' || msg[1] != '0' {
		c.Close()
		logging.LogError("kuma", "Socket.IO unexpected connect response",
			slog.String("msg", string(msg)),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, fmt.Errorf("expected sio connect, got %q", string(msg))
	}

	logging.LogInfo("kuma", "Socket.IO connected",
		slog.String("sid", open.Sid),
		slog.Duration("duration", time.Since(start)),
	)

	go cli.readLoop()
	go cli.pingLoop(open.PingInterval)

	return cli, nil
}

func (c *sioClient) readLoop() {
	defer close(c.done)
	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			logging.LogDebug("kuma", "Socket.IO read loop ended",
				slog.String("error", err.Error()),
			)
			return
		}
		if len(msg) == 0 {
			continue
		}
		switch eioType(msg[0]) {
		case eioPing:
			c.conn.WriteMessage(websocket.TextMessage, []byte("3"))
		case eioMessage:
			c.handleSIO(msg[1:])
		case eioClose:
			logging.LogDebug("kuma", "Socket.IO received close frame")
			return
		}
	}
}

func (c *sioClient) handleSIO(payload []byte) {
	if len(payload) == 0 {
		return
	}
	switch sioType(payload[0]) {
	case sioEvent:
		c.handleEvent(payload[1:])
	case sioAck:
		c.handleAck(payload[1:])
	}
}

func (c *sioClient) handleEvent(data []byte) {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || len(raw) == 0 {
		return
	}

	var name string
	if err := json.Unmarshal(raw[0], &name); err != nil {
		return
	}

	if c.onEvent != nil {
		c.onEvent(rawEvent{Name: name, Args: raw[1:]})
	}
}

func (c *sioClient) handleAck(data []byte) {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || len(raw) == 0 {
		return
	}
	var id int
	if err := json.Unmarshal(raw[0], &id); err != nil {
		return
	}

	c.mu.Lock()
	ch, ok := c.pendingAcks[id]
	delete(c.pendingAcks, id)
	c.mu.Unlock()

	if ok {
		ch <- raw[1:]
	}
}

func (c *sioClient) pingLoop(intervalMs int) {
	if intervalMs <= 0 {
		intervalMs = 25000
	}
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.Lock()
			c.conn.WriteMessage(websocket.TextMessage, []byte("2"))
			c.mu.Unlock()
		}
	}
}

func (c *sioClient) emit(event string, data any) {
	arr := []any{event}
	if data != nil {
		arr = append(arr, data)
	}
	b, _ := json.Marshal(arr)
	c.mu.Lock()
	c.conn.WriteMessage(websocket.TextMessage, append([]byte("42"), b...))
	c.mu.Unlock()
}

func (c *sioClient) emitWithAck(event string, data any) <-chan []json.RawMessage {
	c.mu.Lock()
	c.ackID++
	id := c.ackID
	ch := make(chan []json.RawMessage, 1)
	c.pendingAcks[id] = ch

	arr := []any{id, event}
	if data != nil {
		arr = append(arr, data)
	}
	b, _ := json.Marshal(arr)
	c.conn.WriteMessage(websocket.TextMessage, append([]byte("42"), b...))
	c.mu.Unlock()

	return ch
}

func (c *sioClient) close() {
	logging.LogDebug("kuma", "Closing Socket.IO connection")
	c.conn.Close()
	<-c.done
	logging.LogDebug("kuma", "Socket.IO connection closed")
}

// --- Kuma-specific query ---

type KumaMonitor struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	URL       string  `json:"url,omitempty"`
	Type      string  `json:"type"`
	Status    int     `json:"status"`
	Uptime24h float64 `json:"uptime_24h"`
	Uptime7d  float64 `json:"uptime_7d"`
	Uptime1y  float64 `json:"uptime_1y"`
	Ping      float64 `json:"ping"`
	LastMsg   string  `json:"last_msg,omitempty"`
}

func QueryMonitorsViaSocketIO(kumaURL, username, password string) ([]KumaMonitor, error) {
	queryStart := time.Now()
	logging.LogInfo("kuma", "Querying monitors via Socket.IO",
		slog.String("kuma_url", kumaURL),
	)

	type named struct{ name, url, mtype string }

	var (
		names    = make(map[int]named)
		upt24    = make(map[int]float64)
		upt7d    = make(map[int]float64)
		upt1y    = make(map[int]float64)
		pings    = make(map[int]float64)
		statuses = make(map[int]int)
		msgs     = make(map[int]string)
		seen     = make(map[int]bool)
		loginErr = make(chan error, 1)
	)

	parseID := func(raw json.RawMessage) (int, bool) {
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

	events := make(chan rawEvent, 256)
	cli, err := dialSIO(kumaURL)
	if err != nil {
		return nil, fmt.Errorf("socket.io dial: %w", err)
	}
	defer cli.close()

	cli.onEvent = func(ev rawEvent) {
		events <- ev
	}

	// Wait for loginRequired and respond
	loginSent := false
	loginTimer := time.After(20 * time.Second)

	handleEvent := func(ev rawEvent) bool {
		switch ev.Name {
		case "loginRequired":
			if !loginSent {
				loginSent = true
				ackCh := cli.emitWithAck("login", map[string]string{
					"username": username,
					"password": password,
				})
				go func() {
					select {
					case resp := <-ackCh:
						if len(resp) > 0 {
							var r struct {
								Ok bool `json:"ok"`
							}
							if json.Unmarshal(resp[0], &r) == nil && r.Ok {
								loginErr <- nil
								return
							}
						}
						loginErr <- fmt.Errorf("login rejected")
					case <-time.After(15 * time.Second):
						loginErr <- fmt.Errorf("login timeout")
					}
				}()
			}
		case "uptime":
			if len(ev.Args) >= 3 {
				if id, ok := parseID(ev.Args[0]); ok {
					var dur string
					var val float64
					if json.Unmarshal(ev.Args[1], &dur) == nil && json.Unmarshal(ev.Args[2], &val) == nil {
						seen[id] = true
						switch dur {
						case "24":
							upt24[id] = val
							if val > 0.5 {
								statuses[id] = 1
							} else if val == 0 {
								statuses[id] = 0
							}
						case "720":
							upt7d[id] = val
						case "1y":
							upt1y[id] = val
						}
					}
				}
			}
		case "avgPing":
			if len(ev.Args) >= 2 {
				if id, ok := parseID(ev.Args[0]); ok {
					seen[id] = true
					if string(ev.Args[1]) != "null" {
						var v float64
						if json.Unmarshal(ev.Args[1], &v) == nil {
							pings[id] = v
						}
					}
				}
			}
		case "certInfo":
			if len(ev.Args) >= 2 {
				if id, ok := parseID(ev.Args[0]); ok {
					seen[id] = true
					var certStr string
					if json.Unmarshal(ev.Args[1], &certStr) == nil {
						var p struct {
							CertInfo struct {
								Subject struct {
									CN string `json:"CN"`
								} `json:"subject"`
							} `json:"certInfo"`
						}
						if json.Unmarshal([]byte(certStr), &p) == nil && p.CertInfo.Subject.CN != "" {
							names[id] = named{name: p.CertInfo.Subject.CN, url: "https://" + p.CertInfo.Subject.CN, mtype: "http"}
						}
					}
				}
			}
		case "heartbeat":
			if len(ev.Args) >= 1 {
				var hb struct {
					MonitorID int    `json:"monitorID"`
					Stat      int    `json:"status"`
					Msg       string `json:"msg"`
				}
				if json.Unmarshal(ev.Args[0], &hb) == nil {
					seen[hb.MonitorID] = true
					statuses[hb.MonitorID] = hb.Stat
					if hb.Msg != "" {
						msgs[hb.MonitorID] = hb.Msg
					}
				}
			}
		case "domainInfo":
			if len(ev.Args) >= 1 {
				if id, ok := parseID(ev.Args[0]); ok {
					seen[id] = true
				}
			}
		}
		return false
	}

	// Phase 1: wait for login
	logging.LogDebug("kuma", "Socket.IO waiting for loginRequired event")
loop:
	for {
		select {
		case ev := <-events:
			handleEvent(ev)
		case err := <-loginErr:
			if err != nil {
				logging.LogError("kuma", "Socket.IO login failed",
					slog.String("error", err.Error()),
					slog.Duration("duration", time.Since(queryStart)),
				)
				return nil, fmt.Errorf("login: %w", err)
			}
			logging.LogInfo("kuma", "Socket.IO login successful",
				slog.Duration("duration", time.Since(queryStart)),
			)
			break loop
		case <-loginTimer:
			logging.LogError("kuma", "Socket.IO login timeout",
				slog.Duration("duration", time.Since(queryStart)),
			)
			return nil, fmt.Errorf("login timeout")
		}
	}

	// Phase 2: collect data for 20 seconds
	logging.LogDebug("kuma", "Socket.IO collecting monitor data")
	dataTimer := time.After(20 * time.Second)
collectLoop:
	for {
		select {
		case ev := <-events:
			handleEvent(ev)
		case <-dataTimer:
			break collectLoop
		}
	}

	// Build result
	var out []KumaMonitor
	for id := range seen {
		m := KumaMonitor{
			ID:        id,
			Uptime24h: upt24[id],
			Uptime7d:  upt7d[id],
			Uptime1y:  upt1y[id],
			Ping:      pings[id],
			Status:    statuses[id],
			LastMsg:   msgs[id],
		}
		if n, ok := names[id]; ok {
			m.Name = n.name
			m.URL = n.url
			m.Type = n.mtype
		} else {
			m.Name = fmt.Sprintf("Monitor %d", id)
			m.Type = "?"
		}
		out = append(out, m)
	}

	// Sort by ID
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[i].ID > out[j].ID {
				out[i], out[j] = out[j], out[i]
			}
		}
	}

	logging.LogInfo("kuma", "Socket.IO monitor query complete",
		slog.Int("monitor_count", len(out)),
		slog.Duration("duration", time.Since(queryStart)),
	)

	return out, nil
}
