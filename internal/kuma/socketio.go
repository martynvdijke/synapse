package kuma

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"synapse/internal/logging"
)

// Minimal Engine.IO v4 / Socket.IO v4 client — only enough for Uptime Kuma.
//
// Two Engine.IO transports are supported:
//   - WebSocket (preferred): full-duplex, used for local instances.
//   - HTTP long-polling (fallback): used when the WebSocket handshake is
//     rejected, e.g. when Kuma sits behind a reverse proxy that does not
//     forward the Upgrade/Connection headers (nginx returns 400 with
//     {"code":3,"message":"Bad request"} for the WS upgrade in that case).
//     The polling transport keeps the Engine.IO protocol identical from the
//     Socket.IO layer's point of view: packets are read from HTTP GET
//     long-polls (multiple packets per response separated by 0x1e) and
//     written via HTTP POSTs.

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

// eioOpen is the parsed Engine.IO OPEN packet payload.
type eioOpenInfo struct {
	Sid          string `json:"sid"`
	PingInterval int    `json:"pingInterval"`
	PingTimeout  int    `json:"pingTimeout"`
}

// eioTransport is the Engine.IO wire transport underneath the Socket.IO
// client. Implementations: wsTransport (WebSocket) and pollingTransport
// (HTTP long-polling).
type eioTransport interface {
	// ReadPacket returns the next full Engine.IO packet including its type
	// byte, e.g. "40{...}", "42[...]", "2". It blocks until a packet arrives
	// or the connection is closed.
	ReadPacket() ([]byte, error)
	// WritePacket sends one Engine.IO packet, e.g. "40", "42[...]", "2".
	WritePacket(p []byte) error
	// Close tears down the transport and unblocks any pending ReadPacket.
	Close() error
}

type sioClient struct {
	conn        eioTransport
	mu          sync.Mutex
	ackID       int
	pendingAcks map[int]chan []json.RawMessage
	onEvent     func(rawEvent)
	done        chan struct{}
}

// wsTransport implements eioTransport over a gorilla/websocket connection.
type wsTransport struct {
	conn *websocket.Conn
}

func (w *wsTransport) ReadPacket() ([]byte, error) {
	for {
		_, msg, err := w.conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		if len(msg) == 0 {
			continue
		}
		return msg, nil
	}
}

func (w *wsTransport) WritePacket(p []byte) error {
	return w.conn.WriteMessage(websocket.TextMessage, p)
}

func (w *wsTransport) Close() error {
	return w.conn.Close()
}

// pollingTransport implements eioTransport over Engine.IO HTTP long-polling.
// A single in-flight GET long-poll receives server packets (a response may
// carry several packets joined by the 0x1e record separator); client packets
// are sent with POST requests to the same endpoint.
type pollingTransport struct {
	baseURL string
	sid     string
	http    *http.Client
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex // serializes POSTs
	pending [][]byte   // packets decoded from the current GET response
}

func (p *pollingTransport) endpoint() string {
	u := fmt.Sprintf("%s/socket.io/?EIO=4&transport=polling", p.baseURL)
	if p.sid != "" {
		u += "&sid=" + url.QueryEscape(p.sid)
	}
	return u
}

func (p *pollingTransport) ReadPacket() ([]byte, error) {
	for {
		if len(p.pending) > 0 {
			pkt := p.pending[0]
			p.pending = p.pending[1:]
			return pkt, nil
		}
		if err := p.pollOnce(); err != nil {
			return nil, err
		}
	}
}

// pollOnce issues one GET long-poll and buffers any packets in the response.
func (p *pollingTransport) pollOnce() error {
	req, err := http.NewRequestWithContext(p.ctx, http.MethodGet,
		p.endpoint()+"&t="+strconv.FormatInt(time.Now().UnixMilli(), 10), nil)
	if err != nil {
		return err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("polling GET: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	for _, pkt := range bytes.Split(body, []byte{0x1e}) {
		if len(pkt) == 0 {
			continue
		}
		p.pending = append(p.pending, pkt)
	}
	return nil
}

func (p *pollingTransport) WritePacket(pkt []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	req, err := http.NewRequestWithContext(p.ctx, http.MethodPost, p.endpoint(), bytes.NewReader(pkt))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("polling POST: status %d", resp.StatusCode)
	}
	return nil
}

func (p *pollingTransport) Close() error {
	p.cancel()
	return nil
}

// dialSIO connects to a Kuma instance over Socket.IO. It tries the WebSocket
// transport first; if the WebSocket handshake fails (common behind reverse
// proxies without WS upgrade forwarding), it falls back to Engine.IO HTTP
// long-polling, which works through plain HTTP.
func dialSIO(serverURL string) (*sioClient, error) {
	start := time.Now()
	conn, open, err := dialTransport(serverURL)
	if err != nil {
		logging.LogError("kuma", "Socket.IO dial failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return nil, err
	}

	cli := &sioClient{
		conn:        conn,
		pendingAcks: make(map[int]chan []json.RawMessage),
		done:        make(chan struct{}),
	}

	logging.LogInfo("kuma", "Socket.IO connected",
		slog.String("sid", open.Sid),
		slog.Duration("duration", time.Since(start)),
	)

	go cli.readLoop()
	go cli.pingLoop(open.PingInterval)

	return cli, nil
}

// dialTransport establishes the Engine.IO connection (OPEN handshake + SIO
// CONNECT) over WebSocket, falling back to HTTP long-polling if the WebSocket
// dial is rejected.
func dialTransport(serverURL string) (eioTransport, eioOpenInfo, error) {
	wsConn, wsOpen, wsErr := dialWebSocket(serverURL)
	if wsErr == nil {
		return wsConn, wsOpen, nil
	}
	logging.LogWarn("kuma", "Socket.IO WebSocket dial failed, falling back to HTTP long-polling",
		slog.String("url", serverURL),
		slog.String("error", wsErr.Error()),
	)
	pollConn, pollOpen, pollErr := dialPolling(serverURL)
	if pollErr != nil {
		return nil, eioOpenInfo{}, fmt.Errorf("websocket: %w; polling: %w", wsErr, pollErr)
	}
	return pollConn, pollOpen, nil
}

func dialWebSocket(serverURL string) (eioTransport, eioOpenInfo, error) {
	start := time.Now()
	u := fmt.Sprintf("ws://%s/socket.io/?EIO=4&transport=websocket", serverURL[7:])
	if len(serverURL) > 8 && serverURL[:8] == "https://" {
		u = fmt.Sprintf("wss://%s/socket.io/?EIO=4&transport=websocket", serverURL[8:])
	}

	logging.LogDebug("kuma", "Dialing Socket.IO",
		slog.String("url", serverURL),
		slog.String("ws_url", u),
	)

	dialer := &websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	c, _, err := dialer.Dial(u, nil)
	if err != nil {
		return nil, eioOpenInfo{}, fmt.Errorf("ws dial: %w", err)
	}

	t := &wsTransport{conn: c}
	open, err := completeHandshake(t, serverURL, start)
	if err != nil {
		c.Close()
		return nil, eioOpenInfo{}, err
	}
	return t, open, nil
}

func dialPolling(serverURL string) (eioTransport, eioOpenInfo, error) {
	logging.LogDebug("kuma", "Dialing Socket.IO over HTTP long-polling",
		slog.String("url", serverURL),
	)

	ctx, cancel := context.WithCancel(context.Background())
	p := &pollingTransport{
		baseURL: strings.TrimRight(serverURL, "/"),
		http:    &http.Client{Timeout: 60 * time.Second},
		ctx:     ctx,
		cancel:  cancel,
	}

	// Engine.IO handshake: the OPEN packet arrives as the first GET response
	// before any sid is assigned.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		p.endpoint()+"&t="+strconv.FormatInt(time.Now().UnixMilli(), 10), nil)
	if err != nil {
		cancel()
		return nil, eioOpenInfo{}, err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		cancel()
		return nil, eioOpenInfo{}, fmt.Errorf("polling handshake: %w", err)
	}
	body, rerr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if rerr != nil {
		cancel()
		return nil, eioOpenInfo{}, fmt.Errorf("polling handshake: %w", rerr)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		return nil, eioOpenInfo{}, fmt.Errorf("polling handshake: status %d", resp.StatusCode)
	}
	if len(body) == 0 || eioType(body[0]) != eioOpen {
		cancel()
		return nil, eioOpenInfo{}, fmt.Errorf("expected eio open, got %q", string(body))
	}
	var open eioOpenInfo
	if err := json.Unmarshal(body[1:], &open); err != nil {
		cancel()
		return nil, eioOpenInfo{}, fmt.Errorf("parse eio open: %w", err)
	}
	p.sid = open.Sid

	// Send Socket.IO CONNECT and wait for the "40{...}" response, which is
	// delivered on the polling stream. Note: unlike WebSocket, the connect
	// response for polling arrives via the same long-poll GETs as events, so
	// dialPolling pre-reads it and leaves any further packets buffered.
	if err := p.WritePacket([]byte("40")); err != nil {
		cancel()
		return nil, eioOpenInfo{}, fmt.Errorf("send sio connect: %w", err)
	}
	deadline := time.After(15 * time.Second)
	for len(p.pending) == 0 {
		select {
		case <-deadline:
			cancel()
			return nil, eioOpenInfo{}, fmt.Errorf("polling connect: timeout waiting for sio connect")
		default:
		}
		if err := p.pollOnce(); err != nil {
			cancel()
			return nil, eioOpenInfo{}, fmt.Errorf("polling connect: %w", err)
		}
	}
	connect := p.pending[0]
	p.pending = p.pending[1:]
	if len(connect) < 2 || connect[0] != '4' || connect[1] != '0' {
		cancel()
		return nil, eioOpenInfo{}, fmt.Errorf("expected sio connect, got %q", string(connect))
	}
	return p, open, nil
}

// completeHandshake performs the Engine.IO OPEN read + SIO CONNECT exchange
// shared by transports that deliver the OPEN packet synchronously.
func completeHandshake(t eioTransport, serverURL string, start time.Time) (eioOpenInfo, error) {
	msg, err := t.ReadPacket()
	if err != nil {
		logging.LogError("kuma", "Socket.IO read open failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return eioOpenInfo{}, fmt.Errorf("read eio open: %w", err)
	}
	if len(msg) == 0 || eioType(msg[0]) != eioOpen {
		logging.LogError("kuma", "Socket.IO unexpected open message",
			slog.String("msg", string(msg)),
			slog.Duration("duration", time.Since(start)),
		)
		return eioOpenInfo{}, fmt.Errorf("expected eio open, got %q", string(msg))
	}

	var open eioOpenInfo
	if err := json.Unmarshal(msg[1:], &open); err != nil {
		logging.LogError("kuma", "Socket.IO parse open failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return eioOpenInfo{}, fmt.Errorf("parse eio open: %w", err)
	}

	// Send Socket.IO CONNECT: Engine.IO MESSAGE(4) + SIO CONNECT(0) = "40"
	if err := t.WritePacket([]byte("40")); err != nil {
		logging.LogError("kuma", "Socket.IO send connect failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return eioOpenInfo{}, fmt.Errorf("send sio connect: %w", err)
	}

	// Read SIO CONNECT response
	msg, err = t.ReadPacket()
	if err != nil {
		logging.LogError("kuma", "Socket.IO read connect response failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(start)),
		)
		return eioOpenInfo{}, fmt.Errorf("read sio connect: %w", err)
	}
	if len(msg) < 2 || msg[0] != '4' || msg[1] != '0' {
		logging.LogError("kuma", "Socket.IO unexpected connect response",
			slog.String("msg", string(msg)),
			slog.Duration("duration", time.Since(start)),
		)
		return eioOpenInfo{}, fmt.Errorf("expected sio connect, got %q", string(msg))
	}

	logging.LogInfo("kuma", "Socket.IO connected",
		slog.String("sid", open.Sid),
		slog.Duration("duration", time.Since(start)),
	)
	return open, nil
}

func (c *sioClient) readLoop() {
	defer close(c.done)
	for {
		msg, err := c.conn.ReadPacket()
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
			c.conn.WritePacket([]byte("3"))
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

// setOnEvent installs the event handler under the lock. Safe to call after
// dialSIO has already started the readLoop goroutine.
func (c *sioClient) setOnEvent(fn func(rawEvent)) {
	c.mu.Lock()
	c.onEvent = fn
	c.mu.Unlock()
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

	// Read the handler under the lock: readLoop starts dispatching events as
	// soon as dialSIO returns, potentially before the caller has installed
	// onEvent. The mutex makes that handoff race-free.
	c.mu.Lock()
	onEvent := c.onEvent
	c.mu.Unlock()

	if onEvent != nil {
		onEvent(rawEvent{Name: name, Args: raw[1:]})
	}
}

func (c *sioClient) handleAck(data []byte) {
	// Socket.IO protocol v5: ack id is in the packet header as leading
	// digits, followed by the JSON args array. e.g. "1[{"ok":true}]"
	// (full packet: 431[{"ok":true}]).
	i := 0
	for i < len(data) && data[i] >= '0' && data[i] <= '9' {
		i++
	}
	if i == 0 || i == len(data) {
		return
	}
	id, err := strconv.Atoi(string(data[:i]))
	if err != nil {
		return
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(data[i:], &raw); err != nil || len(raw) == 0 {
		return
	}

	c.mu.Lock()
	ch, ok := c.pendingAcks[id]
	delete(c.pendingAcks, id)
	c.mu.Unlock()

	if ok {
		ch <- raw
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
			c.conn.WritePacket([]byte("2"))
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
	c.conn.WritePacket(append([]byte("42"), b...))
	c.mu.Unlock()
}

func (c *sioClient) emitWithAck(event string, data any) <-chan []json.RawMessage {
	c.mu.Lock()
	c.ackID++
	id := c.ackID
	ch := make(chan []json.RawMessage, 1)
	c.pendingAcks[id] = ch

	arr := []any{event}
	if data != nil {
		arr = append(arr, data)
	}
	b, _ := json.Marshal(arr)
	// Socket.IO protocol v5: ack id goes in the packet header as digits
	// between "42" and the JSON args array, e.g. "421["login",{...}]".
	// (Protocol v4 put the id as the first element of the JSON array,
	// which modern servers decode as an event named after the number.)
	packet := append([]byte("42"), []byte(strconv.Itoa(id))...)
	packet = append(packet, b...)
	c.conn.WritePacket(packet)
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

type MonitorStats struct {
	ID       int     `json:"id"`
	Status   int     `json:"status"`
	Uptime24h float64 `json:"uptime_24h"`
	Uptime7d  float64 `json:"uptime_7d"`
	Uptime1y  float64 `json:"uptime_1y"`
	AvgPing   float64 `json:"avg_ping"`
	LastMsg   string  `json:"last_msg,omitempty"`
	CertInfo  string  `json:"cert_info,omitempty"`
}

type MonitorTag struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Value string `json:"value"`
	Color string `json:"color,omitempty"`
}

type KumaMonitor struct {
	ID              int         `json:"id"`
	Name            string      `json:"name"`
	URL             string      `json:"url,omitempty"`
	Type            string      `json:"type"`
	DockerContainer string      `json:"docker_container,omitempty"`
	DockerHost      int         `json:"docker_host,omitempty"`
	Status          int         `json:"status"`
	Uptime24h       float64     `json:"uptime_24h"`
	Uptime7d        float64     `json:"uptime_7d"`
	Uptime1y        float64     `json:"uptime_1y"`
	Ping            float64     `json:"ping"`
	LastMsg         string      `json:"last_msg,omitempty"`
	Interval        int         `json:"interval,omitempty"`
	RetryInterval   int         `json:"retryInterval,omitempty"`
	MaxRetries      int         `json:"maxretries,omitempty"`
	Active          bool        `json:"active"`
	Tags            []MonitorTag `json:"tags,omitempty"`
}

// dataCollectionWindow bounds how long a Socket.IO query collects events
// after login. Empirically the full monitor list and the complete set of
// uptime/avgPing events arrive within ~2s of login (61 monitors → 183
// uptime events = 61 × 3 durations in the first 2s); a 5s window captures
// everything with a 4x speedup over the historical 20s.
// Kept as a var (not const) so tests can shorten the window.
var dataCollectionWindow = 5 * time.Second

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

// sioCommandSession dials Kuma over Socket.IO, performs the JWT login
// handshake, and returns the connected client plus the event channel. Used by
// one-shot command flows (add/delete/edit monitor).
func sioCommandSession(kumaURL, username, password string) (*sioClient, <-chan rawEvent, error) {
	events := make(chan rawEvent, 256)
	cli, err := dialSIO(kumaURL)
	if err != nil {
		return nil, nil, fmt.Errorf("socket.io dial: %w", err)
	}
	cli.setOnEvent(func(ev rawEvent) {
		events <- ev
	})

	loginErr := make(chan error, 1)
	loginSent := false
	loginTimer := time.After(10 * time.Second)

loop:
	for {
		select {
		case ev := <-events:
			if ev.Name == "loginRequired" && !loginSent {
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
					case <-time.After(10 * time.Second):
						loginErr <- fmt.Errorf("login timeout")
					}
				}()
			}
		case err := <-loginErr:
			if err != nil {
				cli.close()
				return nil, nil, fmt.Errorf("login: %w", err)
			}
			break loop
		case <-loginTimer:
			cli.close()
			return nil, nil, fmt.Errorf("login timeout")
		}
	}
	return cli, events, nil
}

// parseOKAck decodes a Socket.IO ack of the shape {ok: bool, msg: string}.
func parseOKAck(resp []json.RawMessage) (bool, string) {
	if len(resp) == 0 {
		return false, ""
	}
	var r struct {
		Ok  bool   `json:"ok"`
		Msg string `json:"msg"`
	}
	if json.Unmarshal(resp[0], &r) == nil {
		return r.Ok, r.Msg
	}
	return false, ""
}

// QueryDockerHostsViaSocketIO fetches the docker host list from Kuma via
// Socket.IO. Kuma pushes a "dockerHostList" event shortly after login (from
// afterLogin in server.js), so we scan the event stream for it. The list is
// used to resolve a valid monitor.docker_host FK id (id must exist in the
// docker_host table, otherwise SQLite rejects the insert with
// "FOREIGN KEY constraint failed").
func QueryDockerHostsViaSocketIO(kumaURL, username, password string) ([]DockerHost, error) {
	logging.LogInfo("kuma", "Querying docker hosts via Socket.IO",
		slog.String("kuma_url", kumaURL),
	)

	type hostEntry struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	// Kuma emits "dockerHostList" inside afterLogin, i.e. BEFORE the login
	// ack callback is delivered. We must therefore process ALL events while
	// waiting for login (not just loginRequired), otherwise the list is
	// dropped before we ever see it.
	events := make(chan rawEvent, 256)
	cli, err := dialSIO(kumaURL)
	if err != nil {
		return nil, fmt.Errorf("socket.io dial: %w", err)
	}
	defer cli.close()

	cli.setOnEvent(func(ev rawEvent) {
		events <- ev
	})

	loginSent := false
	loginErr := make(chan error, 1)

	collectHosts := func(ev rawEvent) ([]DockerHost, bool) {
		if ev.Name != "dockerHostList" || len(ev.Args) < 1 {
			return nil, false
		}
		var entries []hostEntry
		if json.Unmarshal(ev.Args[0], &entries) != nil {
			logging.LogWarn("kuma", "Socket.IO dockerHostList parse error",
				slog.String("parse_error", "failed to parse host list"),
			)
			return nil, false
		}
		hosts := make([]DockerHost, 0, len(entries))
		for _, e := range entries {
			hosts = append(hosts, DockerHost{ID: e.ID, Name: e.Name})
		}
		return hosts, true
	}

	// Phase 1: wait for login while capturing dockerHostList (arrives pre-ack).
	loginTimer := time.After(15 * time.Second)
	for {
		select {
		case ev := <-events:
			if hosts, ok := collectHosts(ev); ok {
				logging.LogInfo("kuma", "Socket.IO docker host list received",
					slog.Int("host_count", len(hosts)),
				)
				return hosts, nil
			}
			if ev.Name == "loginRequired" && !loginSent {
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
		case err := <-loginErr:
			if err != nil {
				logging.LogError("kuma", "Socket.IO login failed",
					slog.String("error", err.Error()),
				)
				return nil, fmt.Errorf("login: %w", err)
			}
			goto collect
		case <-loginTimer:
			logging.LogWarn("kuma", "Socket.IO docker host login timeout")
			return []DockerHost{}, nil
		}
	}

	// Phase 2: post-login fallback collection window in case the list did not
	// arrive during login (ordering variance on the wire).
collect:
	dataTimer := time.After(dataCollectionWindow)
	for {
		select {
		case ev := <-events:
			if hosts, ok := collectHosts(ev); ok {
				logging.LogInfo("kuma", "Socket.IO docker host list received",
					slog.Int("host_count", len(hosts)),
				)
				return hosts, nil
			}
		case <-dataTimer:
			logging.LogWarn("kuma", "Socket.IO docker host list timeout")
			return []DockerHost{}, nil
		}
	}
}

func QueryMonitorsViaSocketIO(kumaURL, username, password string) ([]KumaMonitor, error) {
	queryStart := time.Now()
	logging.LogInfo("kuma", "Querying monitors via Socket.IO",
		slog.String("kuma_url", kumaURL),
	)

	type named struct {
		name, url, mtype, dockerContainer string
		dockerHost                        int
		interval, retryInterval, maxRetries int
		active                            bool
		activeSet                         bool
		tags                              []MonitorTag
	}

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

	events := make(chan rawEvent, 256)
	cli, err := dialSIO(kumaURL)
	if err != nil {
		return nil, fmt.Errorf("socket.io dial: %w", err)
	}
	defer cli.close()

	cli.setOnEvent(func(ev rawEvent) {
		events <- ev
	})

	// Wait for loginRequired and respond
	loginSent := false
	loginTimer := time.After(20 * time.Second)

	handleEvent := func(ev rawEvent) bool {
		snippet := func(raw json.RawMessage) string {
			s := string(raw)
			if len(s) > 100 {
				s = s[:100] + "..."
			}
			return s
		}
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
		case "monitorList":
			if len(ev.Args) >= 1 {
				// Payload is a map keyed by monitor id string, e.g.
				// {"1": {id:1, name:"vandijke.xyz", url:"http://vandijke.xyz",
				//        type:"docker", docker_container:"vandijke",
				//        docker_host:1, active:true, ...}}.
				// It arrives ~130ms after login with the authoritative
				// names/urls/types — far earlier than certInfo events.
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
				if json.Unmarshal(ev.Args[0], &list) == nil {
					for _, m := range list {
						seen[m.ID] = true
						active := true
						activeSet := false
						if m.Active != nil {
							active = *m.Active
							activeSet = true
						}
						names[m.ID] = named{
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
					}
				} else {
					logging.LogWarn("kuma", "Socket.IO monitorList parse error",
						slog.String("event_type", "monitorList"),
						slog.String("parse_error", "failed to parse monitor map"),
						slog.String("raw_snippet", snippet(ev.Args[0])),
					)
				}
			}
		case "uptime":
			if len(ev.Args) >= 3 {
				if id, ok := parseID(ev.Args[0]); ok {
					var dur string
					var val float64
					if d, ok := parseDuration(ev.Args[1]); ok && json.Unmarshal(ev.Args[2], &val) == nil {
						dur = d
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
					} else {
						logging.LogWarn("kuma", "Socket.IO uptime parse error",
							slog.String("event_type", "uptime"),
							slog.String("parse_error", "failed to parse duration or value"),
							slog.String("raw_snippet", snippet(ev.Args[1])+","+snippet(ev.Args[2])),
						)
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
						} else {
							logging.LogWarn("kuma", "Socket.IO avgPing parse error",
								slog.String("event_type", "avgPing"),
								slog.String("parse_error", "failed to parse ping value"),
								slog.String("raw_snippet", snippet(ev.Args[1])),
							)
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
					} else {
						logging.LogWarn("kuma", "Socket.IO certInfo parse error",
							slog.String("event_type", "certInfo"),
							slog.String("parse_error", "failed to parse cert string"),
							slog.String("raw_snippet", snippet(ev.Args[1])),
						)
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
				} else {
					logging.LogWarn("kuma", "Socket.IO heartbeat parse error",
						slog.String("event_type", "heartbeat"),
						slog.String("parse_error", "failed to parse heartbeat struct"),
						slog.String("raw_snippet", snippet(ev.Args[0])),
					)
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

	// Phase 2: collect data for the collection window
	logging.LogDebug("kuma", "Socket.IO collecting monitor data")
	dataTimer := time.After(dataCollectionWindow)
	eventCounts := make(map[string]int)
collectLoop:
	for {
		select {
		case ev := <-events:
			eventCounts[ev.Name]++
			handleEvent(ev)
		case <-dataTimer:
			break collectLoop
		}
	}

	// Log event-type counts on completion
	logging.LogInfo("kuma", "Socket.IO data collection complete",
		slog.Int("event_count_uptime", eventCounts["uptime"]),
		slog.Int("event_count_avgPing", eventCounts["avgPing"]),
		slog.Int("event_count_heartbeat", eventCounts["heartbeat"]),
		slog.Int("event_count_certInfo", eventCounts["certInfo"]),
		slog.Int("event_count_domainInfo", eventCounts["domainInfo"]),
		slog.Int("total_events_received", func() int {
			total := 0
			for _, c := range eventCounts {
				total += c
			}
			return total
		}()),
		slog.Duration("duration", time.Since(queryStart)),
	)

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
			m.DockerContainer = n.dockerContainer
			m.DockerHost = n.dockerHost
			m.Interval = n.interval
			m.RetryInterval = n.retryInterval
			m.MaxRetries = n.maxRetries
			if n.activeSet {
				m.Active = n.active
			} else {
				m.Active = true
			}
			m.Tags = n.tags
		} else {
			m.Name = fmt.Sprintf("Monitor %d", id)
			m.Type = "?"
			m.Active = true
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

func GetMonitorStats(client *Client, monitorID int) (*MonitorStats, error) {
	queryStart := time.Now()
	logging.LogInfo("kuma", "Getting monitor stats via Socket.IO",
		slog.Int("monitor_id", monitorID),
		slog.String("kuma_url", client.url),
	)

	var (
		upt24    float64
		upt7d    float64
		upt1y    float64
		ping     float64
		status   int
		lastMsg  string
		certInfo string
		loginErr = make(chan error, 1)
		loginSent bool
	)

	events := make(chan rawEvent, 256)
	cli, err := dialSIO(client.url)
	if err != nil {
		return nil, fmt.Errorf("socket.io dial: %w", err)
	}
	defer cli.close()

	cli.setOnEvent(func(ev rawEvent) {
		events <- ev
	})

	loginTimer := time.After(20 * time.Second)

	handleEvent := func(ev rawEvent) {
		switch ev.Name {
		case "loginRequired":
			if !loginSent {
				loginSent = true
				ackCh := cli.emitWithAck("login", map[string]string{
					"username": client.username,
					"password": client.password,
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
				if id, ok := parseID(ev.Args[0]); ok && id == monitorID {
					var dur string
					var val float64
					if json.Unmarshal(ev.Args[1], &dur) == nil && json.Unmarshal(ev.Args[2], &val) == nil {
						switch dur {
						case "24":
							upt24 = val
							if val > 0.5 {
								status = 1
							} else if val == 0 {
								status = 0
							}
						case "720":
							upt7d = val
						case "1y":
							upt1y = val
						}
					}
				}
			}
		case "avgPing":
			if len(ev.Args) >= 2 {
				if id, ok := parseID(ev.Args[0]); ok && id == monitorID {
					if string(ev.Args[1]) != "null" {
						var v float64
						if json.Unmarshal(ev.Args[1], &v) == nil {
							ping = v
						}
					}
				}
			}
		case "certInfo":
			if len(ev.Args) >= 2 {
				if id, ok := parseID(ev.Args[0]); ok && id == monitorID {
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
							certInfo = p.CertInfo.Subject.CN
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
				if json.Unmarshal(ev.Args[0], &hb) == nil && hb.MonitorID == monitorID {
					status = hb.Stat
					if hb.Msg != "" {
						lastMsg = hb.Msg
					}
				}
			}
		}
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

	// Phase 2: collect data for the collection window
	logging.LogDebug("kuma", "Socket.IO collecting monitor stats")
	dataTimer := time.After(dataCollectionWindow)
collectLoop:
	for {
		select {
		case ev := <-events:
			handleEvent(ev)
		case <-dataTimer:
			break collectLoop
		}
	}

	logging.LogInfo("kuma", "Socket.IO monitor stats collection complete",
		slog.Int("monitor_id", monitorID),
		slog.Duration("duration", time.Since(queryStart)),
	)

	return &MonitorStats{
		ID:       monitorID,
		Status:   status,
		Uptime24h: upt24,
		Uptime7d:  upt7d,
		Uptime1y:  upt1y,
		AvgPing:   ping,
		LastMsg:   lastMsg,
		CertInfo:  certInfo,
	}, nil
}

// AddMonitorViaSocketIO creates a monitor in Kuma via the Socket.IO "add" event.
// It opens a fresh WebSocket connection, logs in, emits the add event with the
// given monitor configuration, awaits the callback, and disconnects.
func AddMonitorViaSocketIO(kumaURL, username, password string, monitorType, name, url, dockerContainer string, dockerHostID int) (int, error) {
	logging.LogInfo("kuma", "Adding monitor via Socket.IO",
		slog.String("kuma_url", kumaURL),
		slog.String("name", name),
		slog.String("type", monitorType),
	)

	type addResponse struct {
		Ok        bool   `json:"ok"`
		Msg       string `json:"msg"`
		MonitorID int    `json:"monitorID"`
	}
	var (
		loginErr  = make(chan error, 1)
		loginSent bool
	)

	events := make(chan rawEvent, 256)
	cli, err := dialSIO(kumaURL)
	if err != nil {
		return 0, fmt.Errorf("socket.io dial: %w", err)
	}
	defer cli.close()

	cli.setOnEvent(func(ev rawEvent) {
		events <- ev
	})

	loginTimer := time.After(10 * time.Second)

	// Phase 1: wait for loginRequired and respond
	handleEvent := func(ev rawEvent) {
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
							var r struct{ Ok bool `json:"ok"` }
							if json.Unmarshal(resp[0], &r) == nil && r.Ok {
								loginErr <- nil
								return
							}
						}
						loginErr <- fmt.Errorf("login rejected")
					case <-time.After(10 * time.Second):
						loginErr <- fmt.Errorf("login timeout")
					}
				}()
			}
		}
	}

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
				)
				return 0, fmt.Errorf("login: %w", err)
			}
			logging.LogInfo("kuma", "Socket.IO login successful")
			break loop
		case <-loginTimer:
			return 0, fmt.Errorf("login timeout")
		}
	}

	// Phase 2: emit "add" event and wait for ack.
	// accepted_statuscodes must always be present as an array of strings:
	// Kuma's add handler unconditionally calls
	// monitor.accepted_statuscodes.every(...) and rejects non-strings.
	payload := map[string]any{
		"name":                 name,
		"type":                 monitorType,
		"interval":             60,
		"retryInterval":        60,
		"maxretries":           3,
		"conditions":           []any{},
		"accepted_statuscodes": []string{"200-299"},
	}
	switch monitorType {
	case "http":
		payload["url"] = url
		payload["method"] = "GET"
		payload["accepted_statuscodes"] = []string{"200", "201", "204", "301", "302"}
	case "docker":
		payload["docker_container"] = dockerContainer
		// Only include docker_host when it is a valid (>0) FK id. Kuma's
		// monitor.docker_host references docker_host(id); sending 0 trips a
		// SQLite "FOREIGN KEY constraint failed" because id 0 never exists.
		if dockerHostID > 0 {
			payload["docker_host"] = dockerHostID
		}
	}

	ackCh := cli.emitWithAck("add", payload)
	select {
	case resp := <-ackCh:
		if len(resp) > 0 {
			var r addResponse
			if json.Unmarshal(resp[0], &r) == nil {
			if r.Ok {
				// Modern Kuma returns the ID in the monitorID field;
				// older versions embedded it at the start of msg.
				monitorID := r.MonitorID
				if monitorID == 0 {
					fmt.Sscanf(r.Msg, "%d", &monitorID)
				}
				if monitorID > 0 {
					logging.LogInfo("kuma", "Monitor added via Socket.IO",
						slog.String("name", name),
						slog.Int("monitor_id", monitorID),
					)
					return monitorID, nil
				}
				logging.LogWarn("kuma", "Could not parse monitor ID from add response",
					slog.String("msg", r.Msg),
				)
				// Return 0 as ID success — caller can still proceed
				return 0, nil
			}
				return 0, fmt.Errorf("add monitor rejected: %s", r.Msg)
			}
		}
		return 0, fmt.Errorf("add monitor: unexpected response format")
	case <-time.After(10 * time.Second):
		return 0, fmt.Errorf("add monitor response timeout")
	}
}

// DeleteMonitorViaSocketIO removes a monitor from Kuma via Socket.IO. The
// deleteMonitor event takes the monitor id and answers with {ok, msg}.
func DeleteMonitorViaSocketIO(kumaURL, username, password string, monitorID int) error {
	cli, _, err := sioCommandSession(kumaURL, username, password)
	if err != nil {
		return err
	}
	defer cli.close()

	ackCh := cli.emitWithAck("deleteMonitor", monitorID)
	select {
	case resp := <-ackCh:
		ok, msg := parseOKAck(resp)
		if !ok {
			if msg == "" {
				msg = "unexpected response format"
			}
			return fmt.Errorf("delete monitor rejected: %s", msg)
		}
		logging.LogInfo("kuma", "Monitor deleted via Socket.IO",
			slog.Int("monitor_id", monitorID),
		)
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("delete monitor response timeout")
	}
}

// EditMonitorViaSocketIO updates a monitor in Kuma via Socket.IO. payload is
// the full monitor object (name, type, url, docker_container, docker_host,
// interval, retryInterval, maxretries, conditions, ...) as Kuma's editMonitor
// expects. Type change is an edit with a different "type" field.
func EditMonitorViaSocketIO(kumaURL, username, password string, monitorID int, payload map[string]any) error {
	cli, _, err := sioCommandSession(kumaURL, username, password)
	if err != nil {
		return err
	}
	defer cli.close()

	ackCh := cli.emitWithAck("editMonitor", []any{monitorID, payload})
	select {
	case resp := <-ackCh:
		ok, msg := parseOKAck(resp)
		if !ok {
			if msg == "" {
				msg = "unexpected response format"
			}
			return fmt.Errorf("edit monitor rejected: %s", msg)
		}
		logging.LogInfo("kuma", "Monitor edited via Socket.IO",
			slog.Int("monitor_id", monitorID),
		)
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("edit monitor response timeout")
	}
}

// PauseMonitorViaSocketIO pauses a monitor via Socket.IO "pauseMonitor".
func PauseMonitorViaSocketIO(kumaURL, username, password string, monitorID int) error {
	cli, _, err := sioCommandSession(kumaURL, username, password)
	if err != nil {
		return err
	}
	defer cli.close()
	ackCh := cli.emitWithAck("pauseMonitor", monitorID)
	select {
	case resp := <-ackCh:
		ok, msg := parseOKAck(resp)
		if !ok {
			if msg == "" {
				msg = "unexpected response format"
			}
			return fmt.Errorf("pause monitor rejected: %s", msg)
		}
		logging.LogInfo("kuma", "Monitor paused via Socket.IO", slog.Int("monitor_id", monitorID))
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("pause monitor response timeout")
	}
}

// ResumeMonitorViaSocketIO resumes a paused monitor via Socket.IO "resumeMonitor".
func ResumeMonitorViaSocketIO(kumaURL, username, password string, monitorID int) error {
	cli, _, err := sioCommandSession(kumaURL, username, password)
	if err != nil {
		return err
	}
	defer cli.close()
	ackCh := cli.emitWithAck("resumeMonitor", monitorID)
	select {
	case resp := <-ackCh:
		ok, msg := parseOKAck(resp)
		if !ok {
			if msg == "" {
				msg = "unexpected response format"
			}
			return fmt.Errorf("resume monitor rejected: %s", msg)
		}
		logging.LogInfo("kuma", "Monitor resumed via Socket.IO", slog.Int("monitor_id", monitorID))
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("resume monitor response timeout")
	}
}

// AddMonitorTagViaSocketIO applies a tag to a monitor via Socket.IO "addMonitorTag".
func AddMonitorTagViaSocketIO(kumaURL, username, password string, monitorID, tagID int) error {
	cli, _, err := sioCommandSession(kumaURL, username, password)
	if err != nil {
		return err
	}
	defer cli.close()
	// Kuma expects {monitorID, tagID} — try object form first.
	payload := map[string]any{"monitorID": monitorID, "tagID": tagID}
	ackCh := cli.emitWithAck("addMonitorTag", payload)
	select {
	case resp := <-ackCh:
		ok, msg := parseOKAck(resp)
		if !ok {
			if msg == "" {
				msg = "unexpected response format"
			}
			return fmt.Errorf("add monitor tag rejected: %s", msg)
		}
		logging.LogInfo("kuma", "Monitor tag added via Socket.IO", slog.Int("monitor_id", monitorID), slog.Int("tag_id", tagID))
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("add monitor tag response timeout")
	}
}

// DeleteMonitorTagViaSocketIO removes a tag from a monitor via Socket.IO "deleteMonitorTag".
func DeleteMonitorTagViaSocketIO(kumaURL, username, password string, monitorID, tagID int) error {
	cli, _, err := sioCommandSession(kumaURL, username, password)
	if err != nil {
		return err
	}
	defer cli.close()
	payload := map[string]any{"monitorID": monitorID, "tagID": tagID}
	ackCh := cli.emitWithAck("deleteMonitorTag", payload)
	select {
	case resp := <-ackCh:
		ok, msg := parseOKAck(resp)
		if !ok {
			if msg == "" {
				msg = "unexpected response format"
			}
			return fmt.Errorf("delete monitor tag rejected: %s", msg)
		}
		logging.LogInfo("kuma", "Monitor tag deleted via Socket.IO", slog.Int("monitor_id", monitorID), slog.Int("tag_id", tagID))
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("delete monitor tag response timeout")
	}
}
