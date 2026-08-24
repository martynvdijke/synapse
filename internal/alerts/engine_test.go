package alerts

import (
	"testing"
	"time"

	"synapse/internal/db"
)

// Kuma monitor statuses (subset used by tests).
const (
	KumaStatusDown = 0
	KumaStatusUp   = 1
)

// fakeSource is a configurable StateSource for tests.
type fakeSource struct {
	monitors    map[string]int
	containers  map[string]bool
	syncSuccess map[string]time.Time
	reconcileAt time.Time
	reconcileOK bool
}

func (f *fakeSource) Snapshot() (*Snapshot, error) {
	down := make(map[string]bool, len(f.monitors))
	for name, status := range f.monitors {
		down[name] = status == KumaStatusDown
	}
	snap := &Snapshot{
		MonitorDown:      down,
		ContainerRunning: f.containers,
		LastSyncSuccess:  f.syncSuccess,
	}
	if !f.reconcileAt.IsZero() {
		snap.ReconcileDrift = &f.reconcileOK
	}
	return snap, nil
}

// testNotifier records incident transition notifications.
type testNotifier struct {
	notes *[]string
}

func (n testNotifier) NotifyAlert(event, ruleName, subject, message string) {
	*n.notes = append(*n.notes, event+" | "+ruleName+" | "+message)
}

// testHarness wires an engine against a temp DB with a controllable clock.
type testHarness struct {
	engine *Engine
	store  *db.DB
	src    *fakeSource
	now    time.Time
	notes  *[]string
}

func newHarness(t *testing.T, reminder time.Duration) *testHarness {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	src := &fakeSource{
		monitors:    map[string]int{},
		containers:  map[string]bool{},
		syncSuccess: map[string]time.Time{},
	}
	notes := &[]string{}
	h := &testHarness{
		store:  database,
		src:    src,
		now:    time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		notes:  notes,
	}
	h.engine = NewEngine(database, src, testNotifier{notes: notes})
	h.engine.ReminderInterval = reminder
	h.engine.nowFn = func() time.Time { return h.now }
	return h
}

func (h *testHarness) addRule(t *testing.T, typ, subject string, thresholdSeconds int) db.AlertRule {
	t.Helper()
	id, err := h.store.CreateAlertRule(&db.AlertRule{Name: "rule-" + typ + "-" + subject, Type: typ, Subject: subject, Threshold: thresholdSeconds, Enabled: true})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	rule, _ := h.store.GetAlertRule(id)
	return *rule
}

func (h *testHarness) evaluate(t *testing.T) {
	t.Helper()
	if err := h.engine.Evaluate(); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
}

func TestFlappingMonitorDoesNotOpenIncident(t *testing.T) {
	h := newHarness(t, 0)
	rule := h.addRule(t, db.AlertTypeMonitorDownFor, "plex", 600)

	// Down at t0 — tracking starts, threshold not reached.
	h.src.monitors["plex"] = KumaStatusDown
	h.evaluate(t)

	// Up again 5 minutes later — before the 10m threshold.
	h.now = h.now.Add(5 * time.Minute)
	h.src.monitors["plex"] = KumaStatusUp
	h.evaluate(t)

	incidents, err := h.store.ListIncidents("", 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, inc := range incidents {
		if inc.RuleID == rule.ID && inc.Status != "resolved" {
			t.Fatalf("flapping monitor opened incident %+v", inc)
		}
	}
	if len(*h.notes) != 0 {
		t.Errorf("expected no notifications, got %v", *h.notes)
	}
}

func TestSustainedDowntimeOpensAndAutoResolves(t *testing.T) {
	h := newHarness(t, 0)
	rule := h.addRule(t, db.AlertTypeMonitorDownFor, "plex", 600)

	// Down at t0.
	h.src.monitors["plex"] = KumaStatusDown
	h.evaluate(t)

	// Still down after 10 minutes — incident opens.
	h.now = h.now.Add(10 * time.Minute)
	h.evaluate(t)

	inc, err := h.store.UnresolvedIncident(rule.ID, "plex")
	if err != nil || inc == nil {
		t.Fatalf("expected open incident, got %+v err=%v", inc, err)
	}
	if inc.Status != "open" {
		t.Errorf("expected open, got %s", inc.Status)
	}
	if n := len(*h.notes); n != 1 {
		t.Fatalf("expected exactly one open notification, got %d: %v", n, *h.notes)
	}

	// Next tick still down — no duplicate incident, no extra notification.
	h.now = h.now.Add(60 * time.Second)
	h.evaluate(t)
	incs, _ := h.store.ListIncidents("open", 100)
	if len(incs) != 1 {
		t.Errorf("expected single open incident, got %d", len(incs))
	}
	if len(*h.notes) != 1 {
		t.Errorf("expected no duplicate notifications, got %v", *h.notes)
	}

	// Recovery — auto-resolve + resolve notification.
	h.now = h.now.Add(60 * time.Second)
	h.src.monitors["plex"] = KumaStatusUp
	h.evaluate(t)

	resolved, _ := h.store.ListIncidents("resolved", 100)
	if len(resolved) != 1 || resolved[0].ResolvedAt == nil {
		t.Fatalf("expected one resolved incident, got %+v", resolved)
	}
	if resolved[0].ID != inc.ID {
		t.Errorf("resolved wrong incident: %d vs %d", resolved[0].ID, inc.ID)
	}
	if n := len(*h.notes); n != 2 {
		t.Errorf("expected open+resolve notifications, got %d: %v", n, *h.notes)
	}
}

func TestDisabledRuleSkippedAndIncidentUntouched(t *testing.T) {
	h := newHarness(t, 0)
	rule := h.addRule(t, db.AlertTypeMonitorDownFor, "plex", 0)

	// Open an incident while enabled.
	h.src.monitors["plex"] = KumaStatusDown
	h.evaluate(t)
	h.now = h.now.Add(5 * time.Minute)
	h.evaluate(t)
	inc, _ := h.store.UnresolvedIncident(rule.ID, "plex")
	if inc == nil {
		t.Fatal("expected incident to open")
	}

	// Disable the rule; monitor is up but the incident must NOT auto-resolve.
	if err := h.store.UpdateAlertRule(&db.AlertRule{ID: rule.ID, Name: rule.Name, Type: rule.Type, Subject: rule.Subject, Threshold: rule.Threshold, Enabled: false}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	h.now = h.now.Add(5 * time.Minute)
	h.evaluate(t)

	still, _ := h.store.UnresolvedIncident(rule.ID, "plex")
	if still == nil {
		t.Error("disabling a rule must not alter its open incidents")
	}
}

func TestReminderAfterInterval(t *testing.T) {
	h := newHarness(t, 30*time.Minute)
	rule := h.addRule(t, db.AlertTypeMonitorDownFor, "plex", 0)

	h.src.monitors["plex"] = KumaStatusDown
	h.evaluate(t)
	if n := len(*h.notes); n != 1 {
		t.Fatalf("expected open notification, got %d", n)
	}

	// 20 minutes later — below reminder interval, nothing new.
	h.now = h.now.Add(20 * time.Minute)
	h.evaluate(t)
	if n := len(*h.notes); n != 1 {
		t.Fatalf("reminder fired too early: %v", *h.notes)
	}

	// Past 30 minutes since open — reminder fires and clock resets.
	h.now = h.now.Add(15 * time.Minute)
	h.evaluate(t)
	if n := len(*h.notes); n != 2 {
		t.Fatalf("expected reminder notification, got %d: %v", n, *h.notes)
	}

	// Another 20 minutes — clock was reset, no reminder yet.
	h.now = h.now.Add(20 * time.Minute)
	h.evaluate(t)
	if n := len(*h.notes); n != 2 {
		t.Fatalf("reminder did not reset: %v", *h.notes)
	}
	_ = rule
}

func TestSyncStaleOpensIncident(t *testing.T) {
	h := newHarness(t, 0)
	h.addRule(t, db.AlertTypeSyncStale, "docker", 3600)

	// Last success 2 hours ago → stale.
	h.src.syncSuccess["docker"] = h.now.Add(-2 * time.Hour)
	h.evaluate(t)

	incs, _ := h.store.ListIncidents("open", 100)
	if len(incs) != 1 || incs[0].Subject != "docker" {
		t.Fatalf("expected stale-sync incident for docker, got %+v", incs)
	}

	// Fresh sync clears it.
	h.src.syncSuccess["docker"] = h.now
	h.evaluate(t)
	incs, _ = h.store.ListIncidents("open", 100)
	if len(incs) != 0 {
		t.Errorf("expected auto-resolve after fresh sync, got %d open", len(incs))
	}
}

func TestReconcileDriftOpensAndClears(t *testing.T) {
	h := newHarness(t, 0)
	h.addRule(t, db.AlertTypeReconcileDrift, "", 0)

	h.src.reconcileAt = h.now.Add(-time.Hour)
	h.src.reconcileOK = true
	h.evaluate(t)

	incs, _ := h.store.ListIncidents("open", 100)
	if len(incs) != 1 || incs[0].Subject != "" {
		t.Fatalf("expected global drift incident, got %+v", incs)
	}

	h.src.reconcileOK = false // latest run clean
	h.evaluate(t)
	incs, _ = h.store.ListIncidents("open", 100)
	if len(incs) != 0 {
		t.Errorf("expected drift incident to clear, got %d open", len(incs))
	}
}

func TestContainerDownWithGlobSubject(t *testing.T) {
	h := newHarness(t, 0)
	h.addRule(t, db.AlertTypeContainerDown, "web-*", 300)

	h.src.containers = map[string]bool{"web-1": false, "web-2": true, "api": false}
	h.evaluate(t)
	// Threshold not yet reached — nothing open.
	incs, _ := h.store.ListIncidents("open", 100)
	if len(incs) != 0 {
		t.Fatalf("no incidents expected before threshold, got %+v", incs)
	}

	h.now = h.now.Add(6 * time.Minute)
	h.evaluate(t)
	incs, _ = h.store.ListIncidents("open", 100)
	if len(incs) != 1 || incs[0].Subject != "web-1" {
		t.Fatalf("expected one incident for web-1 only, got %+v", incs)
	}

	// web-1 comes back.
	h.src.containers["web-1"] = true
	h.now = h.now.Add(time.Minute)
	h.evaluate(t)
	incs, _ = h.store.ListIncidents("open", 100)
	if len(incs) != 0 {
		t.Errorf("expected web-1 incident resolved, got %d open", len(incs))
	}
}
