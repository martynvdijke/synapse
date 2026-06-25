package npm

import (
	"fmt"
	"sync"

	"synapse/internal/db"
)

// InstanceClient pairs a NPM instance ID with its Client.
type InstanceClient struct {
	InstanceID int
	Client     *Client
}

// Registry provides access to NPM instances. Simpler than kuma.Registry
// because NPM uses basic auth per request — no login caching needed.
type Registry struct {
	mu sync.Mutex
	db *db.DB
}

func NewRegistry(database *db.DB) *Registry {
	return &Registry{db: database}
}

// All returns an InstanceClient for each enabled NPM instance.
func (r *Registry) All() ([]InstanceClient, error) {
	instances, err := r.db.GetEnabledNPMInstances()
	if err != nil {
		return nil, err
	}
	result := make([]InstanceClient, 0, len(instances))
	for _, inst := range instances {
		c := NewClient(inst.URL, inst.Username, inst.Password)
		result = append(result, InstanceClient{InstanceID: int(inst.ID), Client: c})
	}
	return result, nil
}

// Get returns a Client for the given NPM instance ID.
func (r *Registry) Get(id int) (*Client, error) {
	inst, err := r.db.GetNPMInstance(int64(id))
	if inst == nil || err != nil {
		return nil, fmt.Errorf("npm instance %d not found", id)
	}
	return NewClient(inst.URL, inst.Username, inst.Password), nil
}

// Invalidate is a no-op — NPM clients are stateless.
func (r *Registry) Invalidate(id int) {}
