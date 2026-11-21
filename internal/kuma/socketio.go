package kuma

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// maxBufferedEvents bounds the events retained between dialSIO and the
// caller installing its handler via setOnEvent.
const maxBufferedEvents = 256

type sioClient struct {
	conn        eioTransport
	mu          sync.Mutex // guards the fields below
	writeMu     sync.Mutex // serializes transport writes (gorilla allows one writer)
	ackID       int
	pendingAcks map[int]chan []json.RawMessage
	onEvent     func(rawEvent)
	// bufferedEvents holds events that arrived after dialSIO started the
	// readLoop but before the caller installed onEvent. Without it an event
	// sent immediately after connect (Kuma's loginRequired) can be dropped.
	bufferedEvents []rawEvent
	done           chan struct{}
	closed         bool
	closeOnce      sync.Once
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
	u := p.baseURL + "/socket.io/?EIO=4&transport=polling"
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

// parseKumaURL validates a configured Kuma server URL and returns it parsed.
// Accepts http/https and explicit ws/wss, path prefixes and trailing slashes.
// Returns an error (never panics) for empty, malformed or host-less URLs.
func parseKumaURL(raw string) (*url.URL, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, errors.New("empty kuma url")
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("parse kuma url %q: %w", s, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "ws", "wss":
	default:
		return nil, fmt.Errorf("kuma url %q: unsupported scheme %q", s, u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("kuma url %q: missing host", s)
	}
	return u, nil
}

// pathPrefix returns the configured path prefix with any trailing slashes
// removed, so "/kuma/" and "/kuma" both become "/kuma".
func pathPrefix(u *url.URL) string {
	return strings.TrimRight(u.Path, "/")
}

func wsEndpoint(u *url.URL) string {
	scheme := "ws"
	if strings.EqualFold(u.Scheme, "https") || strings.EqualFold(u.Scheme, "wss") {
		scheme = "wss"
	}
	return scheme + "://" + u.Host + pathPrefix(u) + "/socket.io/?EIO=4&transport=websocket"
}

func pollingBase(u *url.URL) string {
	scheme := "http"
	if strings.EqualFold(u.Scheme, "https") || strings.EqualFold(u.Scheme, "wss") {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: u.Host, Path: pathPrefix(u)}).String()
}

// dialSIO connects to a Kuma instance over Socket.IO with a background ctx.
func dialSIO(serverURL string) (*sioClient, error) {
	return dialSIOContext(context.Background(), serverURL)
}

// dialSIOContext connects to a Kuma instance over Socket.IO. It tries the
// WebSocket transport first; if the WebSocket handshake fails (common behind
// reverse proxies without WS upgrade forwarding), it falls back to Engine.IO
// HTTP long-polling, which works through plain HTTP.
func dialSIOContext(ctx context.Context, serverURL string) (*sioClient, error) {
	start := time.Now()
	conn, open, err := dialTransport(ctx, serverURL)
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
func dialTransport(ctx context.Context, serverURL string) (eioTransport, eioOpenInfo, error) {
	wsConn, wsOpen, wsErr := dialWebSocket(ctx, serverURL)
	if wsErr == nil {
		return wsConn, wsOpen, nil
	}
	if ctx.Err() != nil {
		return nil, eioOpenInfo{}, ctx.Err()
	}
	logging.LogWarn("kuma", "Socket.IO WebSocket dial failed, falling back to HTTP long-polling",
		slog.String("url", serverURL),
		slog.String("error", wsErr.Error()),
	)
	pollConn, pollOpen, pollErr := dialPolling(ctx, serverURL)
	if pollErr != nil {
		return nil, eioOpenInfo{}, fmt.Errorf("websocket: %w; polling: %w", wsErr, pollErr)
	}
	return pollConn, pollOpen, nil
}

func dialWebSocket(ctx context.Context, serverURL string) (eioTransport, eioOpenInfo, error) {
	start := time.Now()
	u, err := parseKumaURL(serverURL)
	if err != nil {
		return nil, eioOpenInfo{}, err
	}
	endpoint := wsEndpoint(u)

	logging.LogDebug("kuma", "Dialing Socket.IO",
		slog.String("url", serverURL),
		slog.String("ws_url", endpoint),
	)

	dialer := &websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	c, _, err := dialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return nil, eioOpenInfo{}, fmt.Errorf("ws dial: %w", err)
	}
	// Bound the OPEN/CONNECT exchange: ReadMessage otherwise has no deadline
	// and a half-open peer would hang the login forever.
	_ = c.SetReadDeadline(time.Now().Add(20 * time.Second))

	t := &wsTransport{conn: c}
	open, err := completeHandshake(ctx, t, start)
	if err != nil {
		c.Close()
		return nil, eioOpenInfo{}, err
	}
	_ = c.SetReadDeadline(time.Time{})
	return t, open, nil
}

func dialPolling(ctx context.Context, serverURL string) (eioTransport, eioOpenInfo, error) {
	logging.LogDebug("kuma", "Dialing Socket.IO over HTTP long-polling",
		slog.String("url", serverURL),
	)

	u, err := parseKumaURL(serverURL)
	if err != nil {
		return nil, eioOpenInfo{}, err
	}

	pctx, cancel := context.WithCancel(ctx)
	p := &pollingTransport{
		baseURL: pollingBase(u),
		http:    &http.Client{Timeout: 60 * time.Second},
		ctx:     pctx,
		cancel:  cancel,
	}

	// Engine.IO handshake: the OPEN packet arrives as the first GET response
	// before any sid is assigned.
	req, err := http.NewRequestWithContext(pctx, http.MethodGet,
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
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for len(p.pending) == 0 {
		select {
		case <-ctx.Done():
			cancel()
			return nil, eioOpenInfo{}, ctx.Err()
		case <-deadline.C:
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

// readPacketCtx reads one Engine.IO packet, aborting (and closing the
// transport) when ctx is cancelled.
func readPacketCtx(ctx context.Context, t eioTransport) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		packet []byte
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		p, err := t.ReadPacket()
		ch <- result{packet: p, err: err}
	}()
	select {
	case <-ctx.Done():
		t.Close()
		return nil, ctx.Err()
	case r := <-ch:
		return r.packet, r.err
	}
}

// completeHandshake performs the Engine.IO OPEN read + SIO CONNECT exchange
// shared by transports that deliver the OPEN packet synchronously.
func completeHandshake(ctx context.Context, t eioTransport, start time.Time) (eioOpenInfo, error) {
	msg, err := readPacketCtx(ctx, t)
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
	msg, err = readPacketCtx(ctx, t)
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
			_ = c.writePacket([]byte("3"))
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
// dialSIO has already started the readLoop goroutine; any events that arrived
// in the meantime are replayed to the handler in order.
func (c *sioClient) setOnEvent(fn func(rawEvent)) {
	c.mu.Lock()
	c.onEvent = fn
	buffered := c.bufferedEvents
	c.bufferedEvents = nil
	c.mu.Unlock()

	for _, ev := range buffered {
		fn(ev)
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

	ev := rawEvent{Name: name, Args: raw[1:]}

	// Read the handler under the lock: readLoop starts dispatching events as
	// soon as dialSIO returns, potentially before the caller has installed
	// onEvent. The mutex makes that handoff race-free, and events seen before
	// the handler exists are buffered so the caller still receives them.
	c.mu.Lock()
	onEvent := c.onEvent
	if onEvent == nil {
		if len(c.bufferedEvents) < maxBufferedEvents {
			c.bufferedEvents = append(c.bufferedEvents, ev)
		}
	}
	c.mu.Unlock()

	if onEvent != nil {
		onEvent(ev)
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
		// Non-blocking: a slow/failed consumer must never stall readLoop.
		select {
		case ch <- raw:
		default:
		}
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
			if err := c.writePacket([]byte("2")); err != nil {
				logging.LogDebug("kuma", "Socket.IO ping write failed",
					slog.String("error", err.Error()),
				)
				c.conn.Close()
				return
			}
		}
	}
}

// writePacket serializes writes to the underlying transport.
func (c *sioClient) writePacket(p []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WritePacket(p)
}

// emit sends an event without waiting for an ack. A write failure is returned
// to the caller so a dead socket is never mistaken for a successful command.
func (c *sioClient) emit(event string, data any) error {
	arr := []any{event}
	if data != nil {
		arr = append(arr, data)
	}
	b, err := json.Marshal(arr)
	if err != nil {
		return err
	}
	return c.writePacket(append([]byte("42"), b...))
}

// emitWithAck sends an event and returns the channel that receives its ack.
// A write failure is returned immediately (and the pending ack removed).
func (c *sioClient) emitWithAck(event string, data any) (<-chan []json.RawMessage, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("socket.io connection closed")
	}
	c.ackID++
	id := c.ackID
	ch := make(chan []json.RawMessage, 1)
	c.pendingAcks[id] = ch
	c.mu.Unlock()

	arr := []any{event}
	if data != nil {
		arr = append(arr, data)
	}
	b, err := json.Marshal(arr)
	if err != nil {
		c.dropAck(id)
		return nil, err
	}
	// Socket.IO protocol v5: ack id goes in the packet header as digits
	// between "42" and the JSON args array, e.g. "421["login",{...}]".
	packet := append([]byte("42"), []byte(strconv.Itoa(id))...)
	packet = append(packet, b...)
	if err := c.writePacket(packet); err != nil {
		c.dropAck(id)
		return nil, err
	}
	return ch, nil
}

func (c *sioClient) dropAck(id int) {
	c.mu.Lock()
	delete(c.pendingAcks, id)
	c.mu.Unlock()
}

// close tears the connection down, waits for readLoop to exit and fails any
// outstanding acks so no caller is left waiting on a dead socket. Safe to
// call more than once.
func (c *sioClient) close() {
	c.closeOnce.Do(func() {
		logging.LogDebug("kuma", "Closing Socket.IO connection")
		c.conn.Close()
		<-c.done
		c.mu.Lock()
		c.closed = true
		for id, ch := range c.pendingAcks {
			delete(c.pendingAcks, id)
			select {
			case ch <- nil:
			default:
			}
		}
		c.mu.Unlock()
		logging.LogDebug("kuma", "Socket.IO connection closed")
	})
}
