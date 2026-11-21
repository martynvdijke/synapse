// Package kuma: transport contract.
//
// This file pins the seam between Synapse's sync engine and the Uptime Kuma
// transport. The API interface is the single surface callers depend on; the
// implementation beneath it (previously the hand-rolled socketio.go, now the
// breml/go-uptime-kuma-client adapter) may be replaced without touching
// callers. Both migration lanes build against this contract:
//
//   - Lane A (transport swap) implements API with a breml-backed adapter,
//     deletes socketio.go, adds StatusKnown to KumaMonitor (populated from the
//     heartbeatList last row — a monitor with no heartbeat in the collection
//     window is "unknown", NOT down), and adds Registry.Snapshot(ctx) returning
//     one InstanceResult per enabled instance, including failures, so callers
//     can no longer mistake a partial read for success.
//   - Lane B (app behavior) consumes API/InstanceResult for truthful sync
//     outcomes, status inference, and idempotent writes keyed on external ids.
//
// Adapter obligations (documented here so they survive the swap):
//
//   - AddMonitor fills spec defaults exactly as the legacy transport did:
//     Interval 60, RetryInterval 60, MaxRetries 3, AcceptedStatusCodes
//     ["200-299"]. Spec values set by the caller always win over defaults.
//   - EditMonitor must fetch the current monitor state first and merge the
//     spec into it. Kuma's editMonitor event mutates the full object: sending
//     a partial payload nulls omitted fields and can wipe notification links.
//   - After AddMonitor, verify the write landed (e.g. re-list or read the
//     monitor) before reporting success; never blind-retry a create, since
//     the add ack can succeed while the resulting event is missed.
//   - A write timeout while waiting for Kuma's post-write event must be
//     treated as "written but unverified": re-read before any retry.
package kuma

import "context"

// API is the transport surface for one Uptime Kuma instance.
//
// All methods take a context and must respect its cancellation. Calls are
// safe from multiple goroutines for distinct instances; callers serialize
// writes per instance.
type API interface {
	// Monitors returns the instance's monitors. It must return a
	// non-error, populated list for instances that have monitors; a
	// transport error must be reported as Err, never as an empty slice.
	Monitors(ctx context.Context) ([]KumaMonitor, error)

	// DockerHosts returns the instance's configured docker hosts.
	DockerHosts(ctx context.Context) ([]DockerHost, error)

	// AddMonitor creates a monitor from spec and returns the new monitor
	// id. The adapter applies the documented defaults and verifies the
	// write before returning.
	AddMonitor(ctx context.Context, spec MonitorSpec) (int, error)

	// EditMonitor merges spec into the existing monitor id. The adapter
	// must fetch-then-merge (see obligations above).
	EditMonitor(ctx context.Context, id int, spec MonitorSpec) error

	// DeleteMonitor removes the monitor id.
	DeleteMonitor(ctx context.Context, id int) error

	// PauseMonitor deactivates the monitor id.
	PauseMonitor(ctx context.Context, id int) error

	// ResumeMonitor reactivates the monitor id.
	ResumeMonitor(ctx context.Context, id int) error

	// Ping verifies the instance is reachable and credentials are valid.
	// Used by instance "test" endpoints and health surfaces.
	Ping(ctx context.Context) error
}

// MonitorSpec is the caller's desired state for a monitor write. Zero values
// are replaced with the documented defaults by the adapter; callers that
// care set fields explicitly.
type MonitorSpec struct {
	// Type is the Kuma monitor type: "http" or "docker_container".
	Type string
	// Name is the monitor name; display-only, never used for matching.
	Name string
	// URL is the target URL for http monitors.
	URL string
	// DockerContainer names the container for docker_container monitors.
	DockerContainer string
	// DockerHostID is the Kuma docker host id for docker_container
	// monitors; 0 means the adapter picks the instance's first host.
	DockerHostID int
	// Interval, RetryInterval, MaxRetries control checking cadence.
	Interval      int
	RetryInterval int
	MaxRetries    int
	// AcceptedStatusCodes are Kuma status code ranges, e.g. "200-299".
	AcceptedStatusCodes []string
	// Description carries provenance text, including the external id
	// marker (see internal/sync/externalid.go, lane B).
	Description string
	// Tags are tag names; the adapter resolves names to ids.
	Tags []string
}

// InstanceResult is one row of Registry.Snapshot: the API handle for one
// enabled instance, or the error that prevented obtaining it. A nil Err
// with a nil Client is invalid; failures must be surfaced, never skipped.
type InstanceResult struct {
	InstanceID int
	Name       string
	Client     API
	Err        error
}
