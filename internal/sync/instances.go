package sync

import (
	"context"
	"fmt"
	"strings"

	"synapse/ent/schema"
	"synapse/internal/kuma"
)

// instanceState tracks one enabled integration instance during a sync or
// reconcile run. Reads are done once per run; write failures accumulate on the
// instance so the final run outcome names the instance that failed rather than
// silently reporting success.
type instanceState struct {
	ID   int
	Name string
	API  kuma.API

	errs []string
	skip bool
	idx  *MonitorIndex

	dockerHostID int
}

// newInstanceStates turns Registry.Snapshot rows into per-instance state,
// preserving connection failures as instance errors instead of dropping them.
func newInstanceStates(results []kuma.InstanceResult) []*instanceState {
	states := make([]*instanceState, 0, len(results))
	for _, res := range results {
		st := &instanceState{ID: res.InstanceID, Name: res.Name, API: res.Client}
		switch {
		case res.Err != nil:
			st.fail("connect: %v", res.Err)
			st.skip = true
		case res.Client == nil:
			st.fail("connect: no API handle returned")
			st.skip = true
		}
		states = append(states, st)
	}
	return states
}

func (st *instanceState) fail(format string, args ...any) {
	st.errs = append(st.errs, fmt.Sprintf(format, args...))
}

// live reports whether the instance may be written to this run. A failed read
// sets skip so no partial state can be mistaken for a clean slate.
func (st *instanceState) live() bool { return !st.skip && st.API != nil }

func (st *instanceState) instanceError() (schema.SyncInstanceError, bool) {
	if len(st.errs) == 0 {
		return schema.SyncInstanceError{}, false
	}
	name := st.Name
	if name == "" {
		name = fmt.Sprintf("instance %d", st.ID)
	}
	return schema.SyncInstanceError{
		InstanceID: st.ID,
		Name:       name,
		Error:      strings.Join(st.errs, "; "),
	}, true
}

// loadMonitors reads the instance's monitor list and builds the external-id
// index. An error, or an empty list while the database still records monitors
// for this instance, skips every write for the instance this run: a partial
// read must never be interpreted as "nothing exists" (which would re-add every
// service and create duplicates).
func (st *instanceState) loadMonitors(ctx context.Context, expected int) *MonitorIndex {
	if !st.live() {
		return nil
	}
	monitors, err := st.API.Monitors(ctx)
	if err != nil {
		st.fail("monitor list: %v", err)
		st.skip = true
		return nil
	}
	if len(monitors) == 0 && expected > 0 {
		st.fail("monitor list came back empty but %d monitor(s) are recorded for this instance", expected)
		st.skip = true
		return nil
	}
	st.idx = BuildMonitorIndex(monitors)
	return st.idx
}

// summariseOutcome derives the truthful run status:
//
//	completed              every enabled instance finished its work
//	completed_with_errors  at least one instance finished, at least one failed
//	failed                 no instance finished (all failed)
func summariseOutcome(states []*instanceState) (string, []schema.SyncInstanceError) {
	if len(states) == 0 {
		return "failed", nil
	}
	okCount := 0
	var instanceErrors []schema.SyncInstanceError
	for _, st := range states {
		if ie, bad := st.instanceError(); bad {
			instanceErrors = append(instanceErrors, ie)
			continue
		}
		okCount++
	}
	switch {
	case okCount == 0:
		return "failed", instanceErrors
	case len(instanceErrors) > 0:
		return "completed_with_errors", instanceErrors
	default:
		return "completed", nil
	}
}

// outcomeMessage summarises a run for the error_message column while the
// per-instance detail lives in instance_errors.
func outcomeMessage(states []*instanceState, instanceErrors []schema.SyncInstanceError, failed int) string {
	var parts []string
	if len(instanceErrors) > 0 {
		parts = append(parts, fmt.Sprintf("%d of %d instance(s) failed", len(instanceErrors), len(states)))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d monitor(s) failed to add", failed))
	}
	return strings.Join(parts, ", ")
}
