// Package alerts implements the alert rule evaluation engine: it compares
// live state (Kuma monitor statuses, Docker container states, sync/reconcile
// history) against enabled alert rules on a periodic schedule and drives the
// incident lifecycle (open, auto-resolve, reminder) with transition
// notifications. Evaluation is read-only with respect to external systems.
package alerts

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"synapse/internal/db"
)

// Kuma monitor status values (see Kuma's heartbeat model).
const (
	KumaStatusDown        = 0
	KumaStatusUp          = 1
	KumaStatusPending     = 2
	KumaStatusMaintenance = 3
)

// StateSource provides the live state the engine evaluates against. Tests
// supply fakes; production wires Kuma/Docker/sync-history readers.
type StateSource interface {
	// MonitorStatuses returns Kuma monitor name -> status for all enabled
	// instances. Called at most once per evaluation tick.
	MonitorStatuses(ctx context.Context) (map[string]int, error)
	// ContainerStates returns container name -> running for all containers
	// (including stopped ones). Called at most once per tick.
	ContainerStates(ctx context.Context) (map[string]bool, error)
	// LastSyncSuccess reports when the named source ("docker"/"npm") last
	// completed successfully; ok=false when it never completed.
	LastSyncSuccess(source string) (time.Time, bool)
	// LastReconcileIssue reports when the most recent reconcile run reported
	// drift or errors; ok=false when the latest run was clean or none ran.
	LastReconcileIssue() (time.Time, bool)
}

// NotifyFunc receives transition notifications (category "alerts").
type NotifyFunc func(title, message string)

// Engine evaluates rules and manages incident transitions.
type Engine struct {
	rules    func() ([]db.AlertRule, error)
	store    *db.DB
	src      StateSource
	notify   NotifyFunc // nil disables notifications
	reminder time.Duration

	nowFn func() time.Time

	mu        sync.Mutex
	downSince map[string]time.Time // ruleID:subject -> first seen down
}

// NewEngine builds an engine. reminder <= 0 disables reminders.
func NewEngine(rules func() ([]db.AlertRule, error), store *db.DB, src StateSource, notify NotifyFunc, reminder time.Duration) *Engine {
	return &Engine{
		rules:     rules,
		store:     store,
		src:       src,
		notify:    notify,
		reminder:  reminder,
		nowFn:     time.Now,
		downSince: make(map[string]time.Time),
	}
}

// downKey namespaces tracking entries per rule and subject.
func downKey(ruleID int64, subject string) string {
	return fmt.Sprintf("%d:%s", ruleID, subject)
}

// trueHits collects this tick's fired conditions: ruleID -> subject -> detail.
type trueHits map[int64]map[string]string

func (t trueHits) add(ruleID int64, subject, detail string) {
	if t[ruleID] == nil {
		t[ruleID] = make(map[string]string)
	}
	t[ruleID][subject] = detail
}

// Evaluate runs one evaluation pass over all enabled rules. Disabled rules
// are skipped entirely — their existing incidents are left untouched.
func (e *Engine) Evaluate(ctx context.Context) error {
	rules, err := e.rules()
	if err != nil {
		return fmt.Errorf("list alert rules: %w", err)
	}

	var (
		monitors                     map[string]int
		containers                   map[string]bool
		needMonitors, needContainers bool
	)
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		switch r.Type {
		case db.AlertTypeMonitorDownFor:
			needMonitors = true
		case db.AlertTypeContainerDown:
			needContainers = true
		}
	}
	if needMonitors {
		if monitors, err = e.src.MonitorStatuses(ctx); err != nil {
			return fmt.Errorf("query monitors: %w", err)
		}
	}
	if needContainers {
		if containers, err = e.src.ContainerStates(ctx); err != nil {
			return fmt.Errorf("list containers: %w", err)
		}
	}

	now := e.nowFn()
	hits := trueHits{}
	liveSubjects := map[int64]map[string]bool{} // ruleID -> subjects currently tracked/true

	e.mu.Lock()
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		threshold := time.Duration(r.Threshold) * time.Second
		switch r.Type {
		case db.AlertTypeMonitorDownFor:
			status, ok := monitors[r.Subject]
			down := ok && status == KumaStatusDown
			key := downKey(r.ID, r.Subject)
			if !down {
				delete(e.downSince, key)
				break
			}
			first := now
			if seen, tracked := e.downSince[key]; tracked {
				first = seen
			} else {
				e.downSince[key] = now
			}
			if now.Sub(first) >= threshold {
				hits.add(r.ID, r.Subject, fmt.Sprintf("down continuously for %s", now.Sub(first).Round(time.Second)))
				liveSubjects[r.ID] = map[string]bool{r.Subject: true}
			}

		case db.AlertTypeContainerDown:
			for name, running := range containers {
				matched, _ := path.Match(r.Subject, name)
				if !matched {
					continue
				}
				key := downKey(r.ID, name)
				if running {
					delete(e.downSince, key)
					continue
				}
				first := now
				if seen, tracked := e.downSince[key]; tracked {
					first = seen
				} else {
					e.downSince[key] = now
				}
				if now.Sub(first) >= threshold {
					hits.add(r.ID, name, fmt.Sprintf("not running for %s", now.Sub(first).Round(time.Second)))
					if liveSubjects[r.ID] == nil {
						liveSubjects[r.ID] = map[string]bool{}
					}
					liveSubjects[r.ID][name] = true
				}
			}

		case db.AlertTypeSyncStale:
			sources := []string{"docker", "npm"}
			if s := strings.TrimSpace(r.Subject); s != "" {
				sources = []string{s}
			}
			for _, src := range sources {
				last, ok := e.src.LastSyncSuccess(src)
				stale := !ok || now.Sub(last) >= threshold
				key := downKey(r.ID, src)
				if !stale {
					delete(e.downSince, key)
					continue
				}
				// Staleness is computed from history, not observed across
				// ticks; fire as soon as the threshold is exceeded.
				since := last
				if !ok {
					since = now.Add(-threshold)
				}
				hits.add(r.ID, src, fmt.Sprintf("no successful %s sync since %s", src, since.Format(time.RFC3339)))
				if liveSubjects[r.ID] == nil {
					liveSubjects[r.ID] = map[string]bool{}
				}
				liveSubjects[r.ID][src] = true
			}

		case db.AlertTypeReconcileDrift:
			issueAt, ok := e.src.LastReconcileIssue()
			fired := ok && (threshold <= 0 || now.Sub(issueAt) >= threshold)
			key := downKey(r.ID, "")
			if !fired {
				delete(e.downSince, key)
				break
			}
			detail := "most recent reconcile run reported drift or errors"
			if threshold > 0 {
				detail = fmt.Sprintf("%s for %s", detail, now.Sub(issueAt).Round(time.Second))
			}
			hits.add(r.ID, "", detail)
			liveSubjects[r.ID] = map[string]bool{"": true}
		}
	}
	e.mu.Unlock()

	// Transition pass: open new incidents, remind stale ones, auto-resolve
	// conditions that cleared.
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		trueSet := hits[r.ID]
		for subject, detail := range trueSet {
			msg := fmt.Sprintf("%s (%s)", r.Name, detail)
			inc, created, err := e.store.OpenIncident(r.ID, subject, msg, now)
			if err != nil {
				return fmt.Errorf("open incident for rule %d/%s: %w", r.ID, subject, err)
			}
			if created {
				e.emit(fmt.Sprintf("[Alert] %s", r.Name), fmt.Sprintf("%s: %s", subjectLabel(subject), msg))
				_ = e.store.MarkIncidentNotified(inc.ID, now)
				continue
			}
			// Reminder clock: base is last notification, falling back to open time.
			base := inc.OpenedAt
			if inc.LastNotifiedAt != nil {
				base = *inc.LastNotifiedAt
			}
			if e.reminder > 0 && now.Sub(base) >= e.reminder {
				e.emit(fmt.Sprintf("[Alert] %s (reminder)", r.Name),
					fmt.Sprintf("%s: still firing — %s", subjectLabel(subject), msg))
				_ = e.store.MarkIncidentNotified(inc.ID, now)
			}
		}
		// Auto-resolve unresolved incidents whose condition cleared.
		unresolved, err := e.store.UnresolvedIncidentsByRule(r.ID)
		if err != nil {
			return fmt.Errorf("list unresolved incidents for rule %d: %w", r.ID, err)
		}
		for _, inc := range unresolved {
			if trueSet[inc.Subject] != "" {
				continue
			}
			if err := e.store.AutoResolveIncident(inc.ID, now); err != nil {
				return fmt.Errorf("auto-resolve incident %d: %w", inc.ID, err)
			}
			e.emit(fmt.Sprintf("[Resolved] %s", r.Name),
				fmt.Sprintf("%s: condition cleared after %s", subjectLabel(inc.Subject), now.Sub(inc.OpenedAt).Round(time.Second)))
		}
	}
	return nil
}

func subjectLabel(subject string) string {
	if subject == "" {
		return "global"
	}
	return subject
}

func (e *Engine) emit(title, message string) {
	if e.notify != nil {
		e.notify(title, message)
	}
}
