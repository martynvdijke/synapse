package kuma

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"synapse/internal/logging"
)

// Timeouts and windows used by the hardened client. Kept as vars so tests can
// shorten them.
var (
	// loginTimeout bounds waiting for loginRequired and the login ack.
	loginTimeout = 15 * time.Second
	// ackTimeout bounds a single command ack.
	ackTimeout = 10 * time.Second
	// pingBudget bounds APIClient.Ping end to end.
	pingBudget = 10 * time.Second
	// loginBudget bounds one registry login probe when the caller's context has
	// no deadline of its own.
	loginBudget = 30 * time.Second
)

// ErrLoginRateLimited is wrapped by password login when Kuma's global
// 20-per-minute login limiter rejects the attempt. Callers must back off, not
// retry immediately.
var ErrLoginRateLimited = errors.New("kuma login rate limited")

// ErrSocketClosed is returned when the socket dies while an operation waits.
var ErrSocketClosed = errors.New("socket.io connection closed")

// ErrWriteUnverified marks a write whose effect could not be confirmed. The
// write may have landed; callers must re-read before retrying.
var ErrWriteUnverified = errors.New("write not verified")

// UnverifiedWriteError describes a mutation that was acknowledged but whose
// result could not be confirmed on the instance (or that timed out after the
// ack was sent). It always satisfies errors.Is(err, ErrWriteUnverified).
type UnverifiedWriteError struct {
	Op        string
	MonitorID int
	Err       error
}

func (e *UnverifiedWriteError) Error() string {
	if e.MonitorID > 0 {
		return fmt.Sprintf("%s monitor %d: %v: %v", e.Op, e.MonitorID, ErrWriteUnverified, e.Err)
	}
	return fmt.Sprintf("%s: %v: %v", e.Op, ErrWriteUnverified, e.Err)
}

func (e *UnverifiedWriteError) Unwrap() error { return e.Err }

func (e *UnverifiedWriteError) Is(target error) bool { return target == ErrWriteUnverified }

// connConfig is everything needed to open one authenticated session.
type connConfig struct {
	URL      string
	Username string
	Password string
	// Token is a previously minted JWT. When set it is tried first because
	// loginByToken is not rate limited and makes Kuma re-push full state.
	Token string
	// PersistToken, when set, receives the freshly minted token after a
	// successful password login ("" clears a rejected token).
	PersistToken func(token string) error
}

// sioSession owns one socket plus the event stream consumers use. All event
// reads go through the events channel so readLoop can never block on a
// consumer that has moved on: callers either collect explicitly or startDrain.
type sioSession struct {
	cli   *sioClient
	cfg   connConfig
	token string

	events  chan rawEvent
	pending []rawEvent

	stop     chan struct{}
	stopOnce sync.Once

	drainOnce sync.Once
}

const maxSessionEvents = 16384

// newSIOSession dials and authenticates one Kuma instance.
func newSIOSession(ctx context.Context, cfg connConfig) (*sioSession, error) {
	cli, err := dialSIOContext(ctx, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("socket.io dial: %w", err)
	}

	s := &sioSession{
		cli:    cli,
		cfg:    cfg,
		token:  cfg.Token,
		events: make(chan rawEvent, 1024),
		stop:   make(chan struct{}),
	}

	cli.setOnEvent(func(ev rawEvent) {
		select {
		case s.events <- ev:
		case <-s.stop:
		case <-cli.done:
		}
	})

	if err := s.authenticate(ctx); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

func (s *sioSession) close() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.cli.close()
}

// startDrain discards further events so readLoop/(emit acks) never block once
// the caller no longer needs the stream. Idempotent.
func (s *sioSession) startDrain() {
	s.drainOnce.Do(func() {
		go func() {
			for {
				select {
				case <-s.stop:
					return
				case <-s.cli.done:
					return
				case <-s.events:
				}
			}
		}()
	})
}

// takePending returns and clears events buffered while waiting for acks.
func (s *sioSession) takePending() []rawEvent {
	p := s.pending
	s.pending = nil
	return p
}

// authenticate logs in with the persisted token when available and falls back
// to password login on rejection. Password login is always acked; the server
// silently drops an un-acked login.
func (s *sioSession) authenticate(ctx context.Context) error {
	loginRequired, err := s.waitLoginRequired(ctx)
	if err != nil {
		return err
	}

	if s.token != "" {
		if err := s.tokenLogin(ctx); err == nil {
			logging.LogDebug("kuma", "Socket.IO token login succeeded")
			return nil
		} else {
			logging.LogWarn("kuma", "Socket.IO token login failed, falling back to password",
				slog.String("error", err.Error()),
			)
		}
		s.token = ""
		if s.cfg.PersistToken != nil {
			_ = s.cfg.PersistToken("")
		}
		if !loginRequired {
			// A rejected token makes Kuma re-emit loginRequired; wait for it
			// briefly so the password login lands in the expected state.
			_, _ = s.waitLoginRequiredTimeout(ctx, 3*time.Second)
		}
	}

	return s.passwordLogin(ctx)
}

// waitLoginRequired waits for the server's loginRequired event, buffering any
// other events for the collector. It reports whether the event was seen.
func (s *sioSession) waitLoginRequired(ctx context.Context) (bool, error) {
	return s.waitLoginRequiredTimeout(ctx, loginTimeout)
}

func (s *sioSession) waitLoginRequiredTimeout(ctx context.Context, timeout time.Duration) (bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-s.cli.done:
			return false, ErrSocketClosed
		case ev := <-s.events:
			if ev.Name == "loginRequired" {
				return true, nil
			}
			s.buffer(ev)
		case <-timer.C:
			// Proceed anyway: some versions/proxies don't emit it before the
			// first event and the login ack is authoritative.
			return false, nil
		}
	}
}

func (s *sioSession) buffer(ev rawEvent) {
	if len(s.pending) < maxSessionEvents {
		s.pending = append(s.pending, ev)
	}
}

func (s *sioSession) tokenLogin(ctx context.Context) error {
	ackCh, err := s.cli.emitWithAck("loginByToken", s.token)
	if err != nil {
		return err
	}
	resp, err := s.awaitAckBuffering(ctx, ackCh, loginTimeout)
	if err != nil {
		return err
	}
	ok, msg := parseOKAck(resp)
	if !ok {
		return fmt.Errorf("loginByToken rejected: %s", msg)
	}
	return nil
}

func (s *sioSession) passwordLogin(ctx context.Context) error {
	ackCh, err := s.cli.emitWithAck("login", map[string]string{
		"username": s.cfg.Username,
		"password": s.cfg.Password,
	})
	if err != nil {
		return err
	}
	resp, err := s.awaitAckBuffering(ctx, ackCh, loginTimeout)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	var r struct {
		Ok    bool   `json:"ok"`
		Msg   string `json:"msg"`
		Token string `json:"token"`
	}
	if len(resp) == 0 || json.Unmarshal(resp[0], &r) != nil {
		return errors.New("login: unexpected response format")
	}
	if !r.Ok {
		if strings.Contains(strings.ToLower(r.Msg), "frequently") {
			return fmt.Errorf("login rejected: %s: %w", r.Msg, ErrLoginRateLimited)
		}
		if r.Msg == "" {
			r.Msg = "invalid credentials"
		}
		return fmt.Errorf("login rejected: %s", r.Msg)
	}

	if r.Token != "" {
		s.token = r.Token
		if s.cfg.PersistToken != nil {
			if err := s.cfg.PersistToken(r.Token); err != nil {
				logging.LogWarn("kuma", "Failed to persist Kuma session token",
					slog.String("error", err.Error()),
				)
			}
		}
	}
	logging.LogInfo("kuma", "Socket.IO login successful")
	return nil
}

// awaitAckBuffering waits for an ack while draining the event stream into
// pending, so state pushed before/with the ack is not lost.
func (s *sioSession) awaitAckBuffering(ctx context.Context, ch <-chan []json.RawMessage, timeout time.Duration) ([]json.RawMessage, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.cli.done:
			return nil, ErrSocketClosed
		case resp, ok := <-ch:
			if !ok || resp == nil {
				return nil, ErrSocketClosed
			}
			return resp, nil
		case ev := <-s.events:
			s.buffer(ev)
		case <-timer.C:
			return nil, fmt.Errorf("ack timeout")
		}
	}
}

// awaitAck waits for an ack without touching the event stream. Only valid once
// startDrain (or a collector) owns the stream.
func awaitAck(ctx context.Context, cli *sioClient, ch <-chan []json.RawMessage, timeout time.Duration) ([]json.RawMessage, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-cli.done:
		return nil, ErrSocketClosed
	case resp, ok := <-ch:
		if !ok || resp == nil {
			return nil, ErrSocketClosed
		}
		return resp, nil
	case <-timer.C:
		return nil, fmt.Errorf("ack timeout")
	}
}

// command performs one acked command on an authenticated session.
func (s *sioSession) command(ctx context.Context, event string, data any) ([]json.RawMessage, error) {
	ackCh, err := s.cli.emitWithAck(event, data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", event, err)
	}
	resp, err := awaitAck(ctx, s.cli, ackCh, ackTimeout)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", event, err)
	}
	return resp, nil
}

// commandOK is command plus the {ok,msg} envelope check.
func (s *sioSession) commandOK(ctx context.Context, event string, data any) (bool, string, error) {
	resp, err := s.command(ctx, event, data)
	if err != nil {
		return false, "", err
	}
	ok, msg := parseOKAck(resp)
	if !ok && msg == "" {
		msg = "unexpected response format"
	}
	return ok, msg, nil
}

// getMonitor fetches the complete current monitor object.
func (s *sioSession) getMonitor(ctx context.Context, monitorID int) (map[string]any, error) {
	resp, err := s.command(ctx, "getMonitor", monitorID)
	if err != nil {
		return nil, err
	}
	var r struct {
		Ok      bool           `json:"ok"`
		Msg     string         `json:"msg"`
		Monitor map[string]any `json:"monitor"`
	}
	if err := json.Unmarshal(resp[0], &r); err != nil {
		return nil, fmt.Errorf("get monitor: %w", err)
	}
	if !r.Ok || r.Monitor == nil {
		if r.Msg == "" {
			r.Msg = "monitor not found"
		}
		return nil, fmt.Errorf("get monitor %d: %s", monitorID, r.Msg)
	}
	return r.Monitor, nil
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

// defaultVerifyLogin is the registry's login probe: a full token-or-password
// login followed by an immediate clean close.
func defaultVerifyLogin(ctx context.Context, cfg connConfig) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, loginBudget)
		defer cancel()
	}
	s, err := newSIOSession(ctx, cfg)
	if err != nil {
		return err
	}
	s.close()
	return nil
}

// verifyLoginFn is overridden in tests to avoid needing a live server.
var verifyLoginFn = defaultVerifyLogin
