package kuma

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"synapse/internal/db"
	"synapse/internal/logging"
)

// InstanceClient pairs a connected Kuma client with the id of the instance
// it belongs to. Callers (sync, handlers) use this to fan out operations
// across all configured instances and to record which instance a monitor
// was created in.
//
// Deprecated: use Registry.Snapshot, which returns API handles and never
// silently drops a failed instance. Kept so existing callers keep compiling.
type InstanceClient struct {
	InstanceID int
	Client     *Client
}

type cachedClient struct {
	client *Client
	api    *APIClient
	user   string
	pass   string
	url    string
	ready  bool // Socket.IO login verified at least once
	// failedAt records when the last login attempt failed. While a failure is
	// within loginRetryCooldown the registry skips the instance instead of
	// paying the full Socket.IO login timeout again on every call.
	failedAt time.Time
}

// loginRetryCooldown bounds how often a failed login is retried. Without it a
// broken instance (bad handshake, auth wall, unreachable) makes every
// Snapshot/All caller pay the login timeout, and password login also has a
// global 20-per-minute rate limit, so reconnect storms self-DoS.
var loginRetryCooldown = 60 * time.Second

// ErrLoginCooldown is wrapped by getOrLogin when an instance is skipped
// because its previous login attempt failed within loginRetryCooldown.
var ErrLoginCooldown = errors.New("login retry cooldown active")

// Registry manages connected Kuma clients for all configured instances.
// Clients are constructed lazily on first use and cached for the process
// lifetime. Socket.IO login is verified on first access and de-duplicated per
// instance so concurrent callers share one login attempt.
type Registry struct {
	mu      sync.Mutex
	db      *db.DB
	clients map[int]*cachedClient
	sf      singleflight.Group
}

// NewRegistry creates an empty registry. The database is read on each
// Snapshot/All call so newly-added instances are picked up without a restart.
func NewRegistry(database *db.DB) *Registry {
	return &Registry{
		db:      database,
		clients: make(map[int]*cachedClient),
	}
}

// Snapshot returns one row per ENABLED instance: the API handle, or the error
// that prevented obtaining it. Failed instances are never skipped. A database
// read failure is surfaced as a single row with Err set.
func (r *Registry) Snapshot(ctx context.Context) []InstanceResult {
	results, err := r.snapshot(ctx)
	if err != nil {
		return []InstanceResult{{Err: err}}
	}
	return results
}

func (r *Registry) snapshot(ctx context.Context) ([]InstanceResult, error) {
	instances, err := r.db.GetEnabledKumaInstances()
	if err != nil {
		return nil, fmt.Errorf("read kuma instances: %w", err)
	}
	results := make([]InstanceResult, 0, len(instances))
	for i := range instances {
		inst := &instances[i]
		cc, loginErr := r.getOrLogin(ctx, inst)
		if loginErr != nil {
			results = append(results, InstanceResult{
				InstanceID: int(inst.ID),
				Name:       inst.Name,
				Err:        loginErr,
			})
			continue
		}
		results = append(results, InstanceResult{
			InstanceID: int(inst.ID),
			Name:       inst.Name,
			Client:     cc.api,
		})
	}
	return results, nil
}

// All returns connected clients for every enabled instance, skipping
// instances that fail to log in.
//
// Deprecated: use Snapshot (lane B migrates callers). It delegates to the
// same snapshot path but keeps the legacy []InstanceClient shape.
func (r *Registry) All() ([]InstanceClient, error) {
	results, err := r.snapshot(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]InstanceClient, 0, len(results))
	for _, res := range results {
		if res.Err != nil {
			if errors.Is(res.Err, ErrLoginCooldown) {
				logging.LogDebug("kuma", "Skipping Kuma instance during login retry cooldown",
					slog.Int("instance_id", res.InstanceID),
					slog.String("error", res.Err.Error()),
				)
			} else {
				logging.LogWarn("kuma", "Failed to connect to Kuma instance, skipping",
					slog.Int("instance_id", res.InstanceID),
					slog.String("instance", res.Name),
					slog.String("error", res.Err.Error()),
				)
			}
			continue
		}
		r.mu.Lock()
		cc := r.clients[res.InstanceID]
		r.mu.Unlock()
		if cc == nil || cc.client == nil {
			// A successful snapshot always caches a client; guard anyway.
			continue
		}
		out = append(out, InstanceClient{InstanceID: res.InstanceID, Client: cc.client})
	}
	return out, nil
}

// Get returns a connected client for a single instance by id.
func (r *Registry) Get(id int) (*Client, error) {
	inst, err := r.db.GetKumaInstance(int64(id))
	if err != nil {
		return nil, fmt.Errorf("read kuma instance %d: %w", id, err)
	}
	if inst == nil {
		return nil, fmt.Errorf("kuma instance %d not found", id)
	}
	cc, err := r.getOrLogin(context.Background(), inst)
	if err != nil {
		return nil, err
	}
	return cc.client, nil
}

// Invalidate drops the cached client for an instance. Call after settings
// changes (url/user/pass) or a persistent connection failure so the next use
// re-creates and re-verifies.
func (r *Registry) Invalidate(id int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, id)
}

// matches reports whether the cached client still belongs to this config.
func (c *cachedClient) matches(inst *db.KumaInstance) bool {
	return c.user == inst.Username && c.pass == inst.Password && c.url == inst.URL
}

// getOrLogin returns a cached, verified client for the instance, creating and
// verifying Socket.IO login on first use. Concurrent callers for the same
// instance are coalesced into one login attempt.
func (r *Registry) getOrLogin(ctx context.Context, inst *db.KumaInstance) (*cachedClient, error) {
	id := int(inst.ID)
	now := time.Now()

	r.mu.Lock()
	cc, exists := r.clients[id]
	if exists && cc.matches(inst) {
		if cc.ready {
			r.mu.Unlock()
			return cc, nil
		}
		if remaining := loginRetryCooldown - now.Sub(cc.failedAt); remaining > 0 {
			failedAgo := now.Sub(cc.failedAt).Round(time.Second)
			r.mu.Unlock()
			return nil, fmt.Errorf("socket.io login to kuma instance %d failed %s ago: retrying in %s: %w",
				id, failedAgo, remaining.Round(time.Second), ErrLoginCooldown)
		}
	}
	r.mu.Unlock()

	value, err, _ := r.sf.Do(strconv.Itoa(id), func() (any, error) {
		return r.login(ctx, inst)
	})
	if err != nil {
		return nil, err
	}
	cc, ok := value.(*cachedClient)
	if !ok || cc == nil {
		return nil, fmt.Errorf("kuma instance %d: login produced no client", id)
	}
	return cc, nil
}

// login performs one verified login and installs the cache entry.
func (r *Registry) login(ctx context.Context, inst *db.KumaInstance) (*cachedClient, error) {
	id := int(inst.ID)

	// Another goroutine may have completed the login while we waited for the
	// single-flight slot.
	r.mu.Lock()
	if cc, ok := r.clients[id]; ok && cc.matches(inst) && cc.ready {
		r.mu.Unlock()
		return cc, nil
	}
	r.mu.Unlock()

	cli := NewClient(inst.URL)
	cli.username = inst.Username
	cli.password = inst.Password
	cli.setSessionToken(inst.SessionToken)

	latestToken := inst.SessionToken
	cfg := connConfig{
		URL:      inst.URL,
		Username: inst.Username,
		Password: inst.Password,
		Token:    inst.SessionToken,
		PersistToken: func(token string) error {
			cli.setSessionToken(token)
			if err := r.db.UpdateKumaInstanceToken(inst.ID, token); err != nil {
				logging.LogWarn("kuma", "Failed to persist Kuma session token",
					slog.Int("instance_id", id),
					slog.String("error", err.Error()),
				)
				return err
			}
			return nil
		},
	}

	if err := verifyLoginFn(ctx, cfg); err != nil {
		r.mu.Lock()
		r.clients[id] = &cachedClient{
			user:     inst.Username,
			pass:     inst.Password,
			url:      inst.URL,
			failedAt: time.Now(),
		}
		r.mu.Unlock()
		return nil, fmt.Errorf("socket.io login to kuma instance %d: %w", id, err)
	}

	// Pick up a token minted during verification so the API client can use
	// loginByToken from its first call.
	if fresh, err := r.db.GetKumaInstance(inst.ID); err == nil && fresh != nil && fresh.SessionToken != "" {
		latestToken = fresh.SessionToken
	}

	api := NewAPIClient(APIConfig{
		URL:          inst.URL,
		Username:     inst.Username,
		Password:     inst.Password,
		Token:        latestToken,
		PersistToken: cfg.PersistToken,
	})

	cc := &cachedClient{
		client: cli,
		api:    api,
		user:   inst.Username,
		pass:   inst.Password,
		url:    inst.URL,
		ready:  true,
	}
	r.mu.Lock()
	r.clients[id] = cc
	r.mu.Unlock()
	return cc, nil
}

// SetVerifySocketIOLoginTestHook overrides the registry's login probe for
// testing. Returns a restore function. Use with defer or t.Cleanup.
func SetVerifySocketIOLoginTestHook(fn func(url, user, pass string) error) func() {
	orig := verifyLoginFn
	verifyLoginFn = func(ctx context.Context, cfg connConfig) error {
		return fn(cfg.URL, cfg.Username, cfg.Password)
	}
	return func() { verifyLoginFn = orig }
}
