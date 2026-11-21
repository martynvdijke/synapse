package kuma

import (
	"context"
	"errors"
	"sync"
	"time"

	"synapse/internal/ttlcache"
)

// APIConfig configures one APIClient. Token is a previously minted JWT; when
// empty the first login uses the password. PersistToken, when set, is called
// with each freshly minted token (and with "" when a stored token is rejected)
// so callers can persist it.
type APIConfig struct {
	URL          string
	Username     string
	Password     string
	Token        string
	PersistToken func(token string) error
}

// APIClient implements API over the hand-rolled Socket.IO transport. It is
// safe for concurrent use: monitor reads are TTL-cached and single-flighted by
// the cache, while writes open short-lived sessions.
type APIClient struct {
	mu       sync.Mutex
	cfg      APIConfig
	monitors *ttlcache.Store[[]KumaMonitor]
}

var _ API = (*APIClient)(nil)

// NewAPIClient builds an API client for one instance.
func NewAPIClient(cfg APIConfig) *APIClient {
	return &APIClient{
		cfg: cfg,
		monitors: ttlcache.New[[]KumaMonitor](
			ttlcache.EnvDuration("UPSTREAM_CACHE_TTL", 60*time.Second),
			ttlcache.EnvDuration("UPSTREAM_RETRY_FLOOR", 5*time.Second),
		),
	}
}

// connConfig snapshots the current credentials for one session.
func (a *APIClient) connConfig() connConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return connConfig{
		URL:          a.cfg.URL,
		Username:     a.cfg.Username,
		Password:     a.cfg.Password,
		Token:        a.cfg.Token,
		PersistToken: a.persistToken,
	}
}

// persistToken records a new token locally and forwards it to the caller.
func (a *APIClient) persistToken(token string) error {
	a.mu.Lock()
	a.cfg.Token = token
	persist := a.cfg.PersistToken
	a.mu.Unlock()
	if persist == nil {
		return nil
	}
	return persist(token)
}

// Monitors returns the instance's monitors, cached for the configured TTL.
// Transport failures are returned as errors (stale data is never reported as
// a fresh success).
func (a *APIClient) Monitors(ctx context.Context) ([]KumaMonitor, error) {
	monitors, _, err := a.monitors.Fetch(func() ([]KumaMonitor, error) {
		s, sessionErr := newSIOSession(ctx, a.connConfig())
		if sessionErr != nil {
			return nil, sessionErr
		}
		defer s.close()
		col := newMonitorCollector()
		if collectErr := s.collect(ctx, col, (*monitorCollector).complete); collectErr != nil {
			return nil, collectErr
		}
		return col.monitors(), nil
	})
	if err != nil {
		return nil, err
	}
	return monitors, nil
}

// DockerHosts returns the instance's configured docker hosts.
func (a *APIClient) DockerHosts(ctx context.Context) ([]DockerHost, error) {
	cfg := a.connConfig()
	s, err := newSIOSession(ctx, cfg)
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

// AddMonitor creates a monitor and verifies it landed.
func (a *APIClient) AddMonitor(ctx context.Context, spec MonitorSpec) (int, error) {
	id, err := addMonitor(ctx, a.connConfig(), spec)
	a.invalidateAfterWrite(err)
	return id, err
}

// EditMonitor fetches the current monitor, merges spec into it and writes the
// complete object back.
func (a *APIClient) EditMonitor(ctx context.Context, id int, spec MonitorSpec) error {
	err := editMonitorSpec(ctx, a.connConfig(), id, spec)
	a.invalidateAfterWrite(err)
	return err
}

// DeleteMonitor removes the monitor id.
func (a *APIClient) DeleteMonitor(ctx context.Context, id int) error {
	err := deleteMonitor(ctx, a.connConfig(), id)
	a.invalidateAfterWrite(err)
	return err
}

// PauseMonitor deactivates the monitor id.
func (a *APIClient) PauseMonitor(ctx context.Context, id int) error {
	err := pauseMonitor(ctx, a.connConfig(), id)
	a.invalidateAfterWrite(err)
	return err
}

// ResumeMonitor reactivates the monitor id.
func (a *APIClient) ResumeMonitor(ctx context.Context, id int) error {
	err := resumeMonitor(ctx, a.connConfig(), id)
	a.invalidateAfterWrite(err)
	return err
}

// Ping performs a fresh dial + login + clean close within a ~10s budget.
func (a *APIClient) Ping(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, pingBudget)
		defer cancel()
	}
	return defaultVerifyLogin(ctx, a.connConfig())
}

// invalidateAfterWrite drops the monitor cache after a successful mutation and
// also after an unverified one: the write may have landed, so cached data must
// not be trusted either way.
func (a *APIClient) invalidateAfterWrite(err error) {
	if err == nil || errors.Is(err, ErrWriteUnverified) {
		a.monitors.Invalidate()
	}
}
