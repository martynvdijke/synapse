// Package alerts implements the stateful alert-rule evaluation engine: rules
// are compared against a live state snapshot on each tick; incidents open,
// remind, and auto-resolve as conditions persist and clear. Evaluation is
// strictly read-only with respect to external systems.
package alerts

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"synapse/internal/db"
	"synapse/internal/logging"
)

// Snapshot is one tick's view of live state, gathered by a StateSource
// implementation in main.go from Kuma / Docker / sync history.
type Snapshot struct {
	// MonitorDown maps monitor name → currently reported down by Kuma
	// (status 0). Monitors absent from the map are considered up/unknown.
	MonitorDown map[string]bool
	// ContainerRunning maps container name → running right now. Containers
	// absent from the map are not running.
	ContainerRunning map[string]bool
	// LastSyncSuccess maps sync source ("docker", "npm") → completion time of
	// the last successful run. Sources without an entry have never succeeded.
	LastSyncSuccess map[string]time.Time
	// ReconcileDrift reports whether the most recent reconcile run finished
	// with drift or errors (nil = no reconcile run yet).
	ReconcileDrift *bool
}

// StateSource gathers a Snapshot for one evaluation tick.
type StateSource interface {
	Snapshot() (*Snapshot, error)
}

// Store persists incidents and supplies enabled rules.
type Store interface {
	ListAlertRules() ([]db.AlertRule, error)
	UnresolvedIncident(ruleID int64, subject string) (*db.AlertIncident, error)
	OpenIncident(ruleID int64, subject, message string, now time.Time) (*db.AlertIncident, bool, error)
	AutoResolveIncident(id int64, now time.Time) error
	MarkIncidentNotified(id int64, now time.Time) error
}

// Notifier receives incident transition messages. The main.go adapter wraps
// EventNotifier so cooldown/toggles/fan-out apply unchanged.
type Notifier interface {
	NotifyAlert(event, ruleName, subject, message string)
}

// Engine evaluates rules against snapshots and drives incident transitions.
type Engine struct {
	store    Store
	src      StateSource
	notifier Notifier

	// ReminderInterval > 0 re-notifies open (unacknowledged) incidents when
	// the interval has elapsed since the last notification.
	ReminderInterval time.Duration

	firstSeen map[string]time.Time // "ruleID|subject" → first observed down
	nowFn     func() time.Time
}

// NewEngine builds an Engine. notifier may be nil (transitions recorded but
// silent).
func NewEngine(store Store, src StateSource, notifier Notifier) *Engine {
	return &Engine{
		store:     store,
		src:       src,
		notifier:  notifier,
		firstSeen: make(map[string]time.Time),
		nowFn:     time.Now,
	}
}

// Evaluate runs one tick: gather snapshot, evaluate every enabled rule,
// open/remind/auto-resolve incidents accordingly. Disabled rules are skipped
// entirely (their existing incidents are left untouched per spec).
func (e *Engine) Evaluate() error {
	snap, err := e.src.Snapshot()
	if err != nil {
		return fmt.Errorf("gather alert state: %w", err)
	}
	rules, err := e.store.ListAlertRules()
	if err != nil {
		return fmt.Errorf("list alert rules: %w", err)
	}
	now := e.nowFn()
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		e.evaluateRule(rule, snap, now)
	}
	e.forgetStaleKeys()
	return nil
}

// evaluateRule handles one rule: compute condition subjects that are firing,
// then reconcile incidents against them.
func (e *Engine) evaluateRule(rule db.AlertRule, snap *Snapshot, now time.Time) {
	firing := e.firingSubjects(rule, snap, now)

	// Open new incidents for firing subjects.
	for _, subject := range firing {
		key := incidentKey(rule.ID, subject)
		// Sync staleness is known from the last success timestamp, so the
		// threshold clock starts there rather than at first observation;
		// otherwise an already-stale source would wait another full
		// threshold window before opening an incident.
		firstObserved := now
		if rule.Type == db.AlertTypeSyncStale {
			if ts, ok := snap.LastSyncSuccess[rule.Subject]; ok && !ts.IsZero() && ts.Before(now) {
				firstObserved = ts
			}
		}
		e.firstSeen[key] = firstOf(e.firstSeen, key, firstObserved)
		downFor := now.Sub(e.firstSeen[key])
		if downFor < time.Duration(rule.Threshold)*time.Second {
			continue // threshold not yet reached — no incident
		}
		msg := fmt.Sprintf("%s for %s", humanCondition(rule), humanDuration(downFor))
		inc, created, err := e.store.OpenIncident(rule.ID, subject, msg, now)
		if err != nil {
			logging.LogError("alerts", "Failed to open incident",
				slog.String("rule", rule.Name), slog.String("error", err.Error()))
			continue
		}
		if created {
			e.notify("opened", rule.Name, subject, msg, inc.ID, now)
		} else if inc.Status == "open" && e.ReminderInterval > 0 {
			last := now.Add(-e.ReminderInterval - time.Minute)
			if inc.LastNotifiedAt != nil {
				last = *inc.LastNotifiedAt
			}
			if now.Sub(last) >= e.ReminderInterval {
				e.notify("reminder", rule.Name, subject, msg, inc.ID, now)
			}
		}
	}

	// Auto-resolve incidents whose condition cleared this tick.
	e.resolveCleared(rule, firing, now)
}

// firingSubjects returns the subjects currently satisfying the rule's
// condition (empty slice = healthy).
func (e *Engine) firingSubjects(rule db.AlertRule, snap *Snapshot, now time.Time) []string {
	switch rule.Type {
	case db.AlertTypeMonitorDownFor:
		var out []string
		for name, down := range snap.MonitorDown {
			if !down {
				continue
			}
			if rule.Subject == "" || matchSubject(rule.Subject, name) {
				out = append(out, name)
			}
		}
		return out
	case db.AlertTypeContainerDown:
		var out []string
		for name, running := range snap.ContainerRunning {
			if running {
				continue
			}
			if rule.Subject == "" || matchSubject(rule.Subject, name) {
				out = append(out, name)
			}
		}
		return out
	case db.AlertTypeSyncStale:
		ts, ok := snap.LastSyncSuccess[rule.Subject]
		if !ok && len(snap.LastSyncSuccess) == 0 && rule.Subject == "" {
			return nil
		}
		if ts.IsZero() {
			// Never synced successfully — stale since forever; use now so the
			// threshold clock starts at observation rather than epoch.
			ts = now
		}
		if now.Sub(ts) >= time.Duration(rule.Threshold)*time.Second {
			return []string{rule.Subject}
		}
		return nil
	case db.AlertTypeReconcileDrift:
		if snap.ReconcileDrift != nil && *snap.ReconcileDrift {
			return []string{""}
		}
		return nil
	default:
		return nil
	}
}

// resolveCleared auto-resolves unresolved incidents of the rule whose subject
// is no longer firing.
func (e *Engine) resolveCleared(rule db.AlertRule, firing []string, now time.Time) {
	// Collect unresolved incidents via UnresolvedIncident per known key; we
	// track keys we opened this session plus probe the empty/global subject.
	seen := map[string]bool{}
	for _, s := range firing {
		seen[s] = true
	}
	for key := range e.firstSeen {
		id, subject, ok := splitKey(key)
		if !ok || id != rule.ID || seen[subject] {
			continue
		}
		inc, err := e.store.UnresolvedIncident(rule.ID, subject)
		if err != nil {
			logging.LogError("alerts", "Failed to query incident",
				slog.String("rule", rule.Name), slog.String("error", err.Error()))
			continue
		}
		if inc != nil {
			if err := e.store.AutoResolveIncident(inc.ID, now); err != nil {
				logging.LogError("alerts", "Failed to auto-resolve incident",
					slog.Int64("incident", inc.ID), slog.String("error", err.Error()))
				continue
			}
			e.notify("resolved", rule.Name, subject, "condition cleared", inc.ID, now)
		}
		delete(e.firstSeen, key)
	}
}

func (e *Engine) notify(event, ruleName, subject, message string, incidentID int64, now time.Time) {
	if e.notifier == nil {
		return
	}
	e.notifier.NotifyAlert(event, ruleName, subject, message)
	// Best-effort reminder-clock reset; open events set it too so reminders
	// measure from the last actual notification.
	if event == "opened" || event == "reminder" {
		_ = e.markNotified(incidentID, now)
	}
}

func (e *Engine) markNotified(incidentID int64, now time.Time) bool {
	m, ok := e.store.(interface {
		MarkIncidentNotified(id int64, now time.Time) error
	})
	if !ok {
		return false
	}
	return m.MarkIncidentNotified(incidentID, now) == nil
}

// forgetStaleKeys drops tracking entries older than 24h so restarts and
// deleted rules cannot leak memory.
func (e *Engine) forgetStaleKeys() {
	now := e.nowFn()
	for k, t := range e.firstSeen {
		if now.Sub(t) > 24*time.Hour {
			delete(e.firstSeen, k)
		}
	}
}

func incidentKey(ruleID int64, subject string) string {
	return fmt.Sprintf("%d|%s", ruleID, subject)
}

func splitKey(key string) (int64, string, bool) {
	i := strings.Index(key, "|")
	if i < 0 {
		return 0, "", false
	}
	var id int64
	if _, err := fmt.Sscanf(key[:i], "%d", &id); err != nil {
		return 0, "", false
	}
	return id, key[i+1:], true
}

func firstOf(m map[string]time.Time, key string, now time.Time) time.Time {
	if t, ok := m[key]; ok {
		return t
	}
	return now
}

// matchSubject does exact match or prefix* glob matching.
func matchSubject(pattern, name string) bool {
	if p, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(name, p)
	}
	return pattern == name
}

func humanCondition(rule db.AlertRule) string {
	switch rule.Type {
	case db.AlertTypeMonitorDownFor:
		return fmt.Sprintf("Monitor %q down", displaySubject(rule.Subject))
	case db.AlertTypeContainerDown:
		return fmt.Sprintf("Container %q not running", displaySubject(rule.Subject))
	case db.AlertTypeSyncStale:
		return fmt.Sprintf("Sync source %q stale", displaySubject(rule.Subject))
	case db.AlertTypeReconcileDrift:
		return "Reconcile drift detected"
	default:
		return rule.Type
	}
}

func displaySubject(s string) string {
	if s == "" {
		return "(any)"
	}
	return s
}

func humanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return d.String()
	}
	mins := int(d.Minutes())
	if mins < 60 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dh%02dm", mins/60, mins%60)
}
