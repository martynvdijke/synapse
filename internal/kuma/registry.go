package kuma

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"synapse/internal/db"
	"synapse/internal/logging"
)

// InstanceClient pairs a connected Kuma client with the id of the instance
// it belongs to. Callers (sync, handlers) use this to fan out operations
// across all configured instances and to record which instance a monitor
// was created in.
type InstanceClient struct {
	InstanceID int
	Client     *Client
}

type cachedClient struct {
	client *Client
	user   string
	pass   string
	ready  bool // Socket.IO login verified at least once
}

// Registry manages connected Kuma clients for all configured instances.
// Clients are constructed lazily on first use and cached for the process
// lifetime. Socket.IO login is verified on first access.
type Registry struct {
	mu      sync.Mutex
	db      *db.DB
	clients map[int]*cachedClient
}

// NewRegistry creates an empty registry. The database is read on each
// All/Get call so newly-added instances are picked up without a restart.
func NewRegistry(database *db.DB) *Registry {
	return &Registry{
		db:      database,
		clients: make(map[int]*cachedClient),
	}
}

// All returns connected clients for every enabled instance, verifying
// Socket.IO login as needed. Instances that fail to connect are skipped
// (with a logged warning) so one unreachable instance does not block a sync.
func (r *Registry) All() ([]InstanceClient, error) {
	instances, err := r.db.GetEnabledKumaInstances()
	if err != nil {
		return nil, fmt.Errorf("read kuma instances: %w", err)
	}
	result := make([]InstanceClient, 0, len(instances))
	for _, inst := range instances {
		c, err := r.getOrLogin(int(inst.ID), inst.Username, inst.Password, inst.URL)
		if err != nil {
			logging.LogWarn("kuma", "Failed to connect to Kuma instance, skipping",
				slog.String("instance", inst.Name),
				slog.String("error", err.Error()),
			)
			continue
		}
		result = append(result, InstanceClient{InstanceID: int(inst.ID), Client: c})
	}
	return result, nil
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
	return r.getOrLogin(int(inst.ID), inst.Username, inst.Password, inst.URL)
}

// Invalidate drops the cached client for an instance. Call after settings
// changes (url/user/pass) or a persistent connection failure so the next
// use re-creates and re-verifies.
func (r *Registry) Invalidate(id int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, id)
}

// getOrLogin returns a cached, verified client for the instance, creating
// and verifying Socket.IO login on first use.
func (r *Registry) getOrLogin(id int, user, pass, url string) (*Client, error) {
	r.mu.Lock()
	cc, exists := r.clients[id]
	r.mu.Unlock()

	if exists && cc.ready {
		return cc.client, nil
	}

	c := NewClient(url)
	c.username = user
	c.password = pass

	if err := verifySocketIOLoginFn(url, user, pass); err != nil {
		return nil, fmt.Errorf("socket.io login to kuma instance %d: %w", id, err)
	}

	r.mu.Lock()
	r.clients[id] = &cachedClient{client: c, user: user, pass: pass, ready: true}
	r.mu.Unlock()
	return c, nil
}

// verifySocketIOLoginFn is overridden in tests to avoid needing a live Socket.IO server.
var verifySocketIOLoginFn = verifySocketIOLogin

// SetVerifySocketIOLoginTestHook overrides verifySocketIOLoginFn for testing.
// Returns a restore function. Use with defer or t.Cleanup.
func SetVerifySocketIOLoginTestHook(fn func(url, user, pass string) error) func() {
	orig := verifySocketIOLoginFn
	verifySocketIOLoginFn = fn
	return func() { verifySocketIOLoginFn = orig }
}

// verifySocketIOLogin performs a lightweight Socket.IO login check against a
// Kuma instance. It connects, waits for loginRequired, emits login
// credentials, verifies the success response, and disconnects.
func verifySocketIOLogin(kumaURL, user, pass string) error {
	var (
		loginErr  = make(chan error, 1)
		loginSent bool
	)

	events := make(chan rawEvent, 256)
	cli, err := dialSIO(kumaURL)
	if err != nil {
		return fmt.Errorf("socket.io dial: %w", err)
	}
	defer cli.close()

	cli.onEvent = func(ev rawEvent) {
		events <- ev
	}

	loginTimer := time.After(10 * time.Second)

	for {
		select {
		case ev := <-events:
			if ev.Name == "loginRequired" && !loginSent {
				loginSent = true
				ackCh := cli.emitWithAck("login", map[string]string{
					"username": user,
					"password": pass,
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
		case err := <-loginErr:
			return err
		case <-loginTimer:
			return fmt.Errorf("login timeout")
		}
	}
}
