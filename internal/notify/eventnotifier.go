package notify

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"synapse/internal/logging"
)

// Category identifies a notification category; toggles and cooldowns are
// tracked per category.
type Category string

const (
	// CatDockerDie notifies about unexpected container deaths.
	CatDockerDie Category = "docker_die"
	// CatDockerHealth notifies about container health-status changes.
	CatDockerHealth Category = "docker_health"
	// CatDockerImage notifies about container image updates.
	CatDockerImage Category = "docker_image"
	// CatReconcile notifies about reconciliation drift/actions.
	CatReconcile Category = "reconcile"
	// CatAlerts notifies about alert-rule incident transitions (open,
	// resolve, reminder).
	CatAlerts Category = "alerts"
)

// AllCategories lists every known category (for settings UI / toggles).
func AllCategories() []Category {
	return []Category{CatDockerDie, CatDockerHealth, CatDockerImage, CatReconcile, CatAlerts}
}

// defaultCooldown bounds how often a given category may notify.
const defaultCooldown = 5 * time.Minute

// EventNotifierOptions configures an EventNotifier.
type EventNotifierOptions struct {
	// Enabled is the master switch for docker/reconcile notifications.
	Enabled bool
	// Cooldown dedups repeated notifications per category (default 5m).
	Cooldown time.Duration
	// Toggles enables/disables individual categories. Nil = all enabled.
	Toggles map[Category]bool
}

// EventNotifier sends docker/reconcile notifications to every configured
// notification channel with per-category toggles and cooldown-based dedup.
// While a category is in cooldown, further notifications are aggregated into a
// suppressed count and reported on the next message that gets through. A
// failing channel is logged and skipped without affecting other channels.
type EventNotifier struct {
	channels []Notifier
	enabled  bool
	cooldown time.Duration
	toggles  map[Category]bool

	mu         sync.Mutex
	lastSent   map[Category]time.Time
	suppressed map[Category]int
	nowFn      func() time.Time
}

// NewEventNotifier builds an EventNotifier that fans out to the given
// channels. An empty slice (or nil channels) makes sends no-op (return false).
// A non-positive cooldown falls back to the 5-minute default.
func NewEventNotifier(channels []Notifier, opts EventNotifierOptions) *EventNotifier {
	cd := opts.Cooldown
	if cd <= 0 {
		cd = defaultCooldown
	}
	return &EventNotifier{
		channels:   channels,
		enabled:    opts.Enabled,
		cooldown:   cd,
		toggles:    opts.Toggles,
		lastSent:   make(map[Category]time.Time),
		suppressed: make(map[Category]int),
		nowFn:      time.Now,
	}
}

// Toggle reports whether a category is enabled (master switch + per-category
// toggle; unknown categories default to enabled).
func (n *EventNotifier) Toggle(cat Category) bool {
	if !n.enabled {
		return false
	}
	if n.toggles == nil {
		return true
	}
	on, known := n.toggles[cat]
	return !known || on
}

// Notify fans a message out to every configured channel for a category
// subject to its toggle and cooldown. It returns true when at least one
// channel accepted the message. When the category is in cooldown, the
// notification is counted as suppressed instead and no channel is called.
func (n *EventNotifier) Notify(ctx context.Context, cat Category, title, message string) bool {
	if !n.Toggle(cat) || len(n.channels) == 0 {
		return false
	}

	n.mu.Lock()
	now := n.nowFn()
	last, sent := n.lastSent[cat]
	if sent && now.Sub(last) < n.cooldown {
		n.suppressed[cat]++
		n.mu.Unlock()
		return false
	}
	n.lastSent[cat] = now
	extra := n.suppressed[cat]
	n.suppressed[cat] = 0
	n.mu.Unlock()

	if extra > 0 {
		message = fmt.Sprintf("%s\n(%d similar notification(s) suppressed)", message, extra)
	}

	delivered := false
	for _, ch := range n.channels {
		if ch == nil || !ch.Enabled() {
			continue
		}
		if err := ch.Send(ctx, cat, title, message); err != nil {
			logging.LogError("notify", "Notification channel failed",
				slog.String("channel", ch.Name()),
				slog.String("category", string(cat)),
				slog.String("error", err.Error()),
			)
			continue
		}
		delivered = true
	}
	return delivered
}

// Suppressed returns the number of suppressed notifications for a category
// (diagnostics/testing).
func (n *EventNotifier) Suppressed(cat Category) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.suppressed[cat]
}

// ContainerStopTracker classifies unexpected container deaths: a die event is
// "unexpected" when no graceful stop was observed for the container within a
// recent window.
type ContainerStopTracker struct {
	mu     sync.Mutex
	stops  map[string]time.Time
	window time.Duration
	nowFn  func() time.Time
}

// NewContainerStopTracker builds a tracker with the given window.
func NewContainerStopTracker(window time.Duration) *ContainerStopTracker {
	if window <= 0 {
		window = 60 * time.Second
	}
	return &ContainerStopTracker{
		stops:  make(map[string]time.Time),
		window: window,
		nowFn:  time.Now,
	}
}

// MarkStop records that a container stopped gracefully at the given time.
func (t *ContainerStopTracker) MarkStop(container string, at time.Time) {
	t.mu.Lock()
	t.stops[container] = at
	t.mu.Unlock()
}

// WasGraceful reports whether a container stopped gracefully within the
// window before the given time. The record is consumed (removed) on lookup so
// each stop explains at most one die.
func (t *ContainerStopTracker) WasGraceful(container string, at time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	stop, ok := t.stops[container]
	if !ok {
		return false
	}
	delete(t.stops, container)
	return !at.Before(stop) && at.Sub(stop) <= t.window
}
