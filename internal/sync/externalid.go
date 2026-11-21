package sync

import (
	"strings"

	"synapse/internal/kuma"
)

// SynapseTagName is the tag every Synapse-managed monitor carries. It is the
// read-side signal that a monitor belongs to Synapse: Uptime Kuma's monitor
// list does not expose the description, so the tag is what survives a round
// trip. The description marker below is the precise key when it is available.
const SynapseTagName = "synapse"

const markerPrefix = "synapse:"

// ExternalID is the stable identity of a Synapse-managed monitor: a monitor
// type and the service it monitors. Name matching is deliberately not used as
// the primary key — two services on different instances may share a display
// name, and a renamed container should not create a duplicate monitor.
type ExternalID struct {
	Type    string
	Service string
}

// Marker renders the external-id marker embedded in a monitor description.
func Marker(monitorType, service string) string {
	return markerPrefix + monitorType + ":" + service
}

// ParseMarker extracts an external id from provenance text. The marker may be
// surrounded by other description lines; the first marker line wins.
func ParseMarker(text string) (ExternalID, bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, markerPrefix) {
			continue
		}
		rest := strings.TrimPrefix(line, markerPrefix)
		typ, service, ok := strings.Cut(rest, ":")
		if !ok {
			continue
		}
		typ = strings.TrimSpace(typ)
		service = strings.TrimSpace(service)
		if typ == "" || service == "" {
			continue
		}
		return ExternalID{Type: typ, Service: service}, true
	}
	return ExternalID{}, false
}

// ExternalIDFromMonitor recovers the external id from a monitor's metadata.
// The precise marker lives in the description (when the transport exposes it);
// the synapse tag is the fallback and also carries the marker value on
// instances where tag values are preserved. managed reports whether the
// monitor is Synapse-managed at all.
func ExternalIDFromMonitor(m kuma.KumaMonitor) (id ExternalID, managed bool) {
	for _, t := range m.Tags {
		if t.Name != SynapseTagName {
			continue
		}
		if parsed, ok := ParseMarker(t.Value); ok {
			return parsed, true
		}
		return ExternalID{}, true
	}
	return ExternalID{}, false
}

// MonitorIndex is the read-before-create lookup built from one instance's
// monitor list. It replaces name-only matching in sync and reconcile.
type MonitorIndex struct {
	monitors     []kuma.KumaMonitor
	byID         map[int]int
	byExternal   map[ExternalID]int
	byService    map[string]int
	byTaggedName map[string]int
	byLegacyName map[string]int
}

// BuildMonitorIndex indexes a monitor list. The slice is copied so later Adds
// cannot invalidate entries already indexed.
func BuildMonitorIndex(monitors []kuma.KumaMonitor) *MonitorIndex {
	ix := &MonitorIndex{
		monitors:     make([]kuma.KumaMonitor, 0, len(monitors)),
		byID:         make(map[int]int, len(monitors)),
		byExternal:   make(map[ExternalID]int, len(monitors)),
		byService:    make(map[string]int, len(monitors)),
		byTaggedName: make(map[string]int, len(monitors)),
		byLegacyName: make(map[string]int, len(monitors)),
	}
	for _, m := range monitors {
		ix.Add(m)
	}
	return ix
}

// Add indexes one monitor (used after a successful create).
func (ix *MonitorIndex) Add(m kuma.KumaMonitor) {
	idx := len(ix.monitors)
	ix.monitors = append(ix.monitors, m)
	ix.byID[m.ID] = idx

	id, managed := ExternalIDFromMonitor(m)
	if managed {
		if _, exists := ix.byTaggedName[m.Name]; !exists {
			ix.byTaggedName[m.Name] = idx
		}
		if id.Service != "" {
			if _, exists := ix.byExternal[id]; !exists {
				ix.byExternal[id] = idx
			}
			if _, exists := ix.byService[id.Service]; !exists {
				ix.byService[id.Service] = idx
			}
		}
		return
	}
	if _, exists := ix.byLegacyName[m.Name]; !exists {
		ix.byLegacyName[m.Name] = idx
	}
}

// All returns the indexed monitors in their original order.
func (ix *MonitorIndex) All() []kuma.KumaMonitor {
	out := make([]kuma.KumaMonitor, len(ix.monitors))
	copy(out, ix.monitors)
	return out
}

// Len reports how many monitors are indexed.
func (ix *MonitorIndex) Len() int { return len(ix.monitors) }

// ByID looks up a monitor by Kuma id.
func (ix *MonitorIndex) ByID(kumaID int) *kuma.KumaMonitor {
	if i, ok := ix.byID[kumaID]; ok {
		return &ix.monitors[i]
	}
	return nil
}

// Find resolves a monitor by external id first, then by the service name of a
// Synapse-tagged monitor, then by display name (tagged monitors before
// unmanaged ones so an upgraded instance adopts its own monitor rather than
// matching an unrelated one).
func (ix *MonitorIndex) Find(id ExternalID, name string) *kuma.KumaMonitor {
	if i, ok := ix.byExternal[id]; ok {
		return &ix.monitors[i]
	}
	if id.Service != "" {
		if i, ok := ix.byService[id.Service]; ok {
			return &ix.monitors[i]
		}
	}
	if i, ok := ix.byTaggedName[name]; ok {
		return &ix.monitors[i]
	}
	if i, ok := ix.byLegacyName[name]; ok {
		return &ix.monitors[i]
	}
	return nil
}
