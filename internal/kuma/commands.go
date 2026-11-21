package kuma

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"synapse/internal/logging"
)

// Defaults applied by AddMonitor when a MonitorSpec leaves them zero. They
// mirror the legacy transport and are documented in api.go.
const (
	defaultInterval       = 60
	defaultRetryInterval  = 60
	defaultMaxRetries     = 3
	defaultAcceptedCodes  = "200-299"
	commandSessionTimeout = 60 * time.Second
)

func applyMonitorDefaults(spec MonitorSpec) MonitorSpec {
	if spec.Interval == 0 {
		spec.Interval = defaultInterval
	}
	if spec.RetryInterval == 0 {
		spec.RetryInterval = defaultRetryInterval
	}
	if spec.MaxRetries == 0 {
		spec.MaxRetries = defaultMaxRetries
	}
	if len(spec.AcceptedStatusCodes) == 0 {
		spec.AcceptedStatusCodes = []string{defaultAcceptedCodes}
	}
	return spec
}

// kumaMonitorType maps the caller-facing spec type to Kuma's wire type.
func kumaMonitorType(t string) string {
	switch t {
	case "docker_container", "docker":
		return "docker"
	default:
		return t
	}
}

// buildAddPayload renders a full add payload. accepted_statuscodes must always
// be a string array: Kuma's add handler unconditionally calls .every().
func buildAddPayload(spec MonitorSpec, dockerHostID int) map[string]any {
	mtype := kumaMonitorType(spec.Type)
	payload := map[string]any{
		"name":                 spec.Name,
		"type":                 mtype,
		"interval":             spec.Interval,
		"retryInterval":        spec.RetryInterval,
		"maxretries":           spec.MaxRetries,
		"conditions":           []any{},
		"accepted_statuscodes": spec.AcceptedStatusCodes,
	}
	if spec.Description != "" {
		payload["description"] = spec.Description
	}
	switch mtype {
	case "http":
		payload["url"] = spec.URL
		payload["method"] = "GET"
	case "docker":
		payload["docker_container"] = spec.DockerContainer
		if dockerHostID > 0 {
			payload["docker_host"] = dockerHostID
		}
	}
	return payload
}

// buildEditSpecPayload merges the non-zero spec fields into the complete
// current monitor object. editMonitor replaces the whole row, so every field
// we are not changing must be carried over verbatim.
func buildEditSpecPayload(current map[string]any, id int, spec MonitorSpec) map[string]any {
	payload := make(map[string]any, len(current)+8)
	for k, v := range current {
		payload[k] = v
	}
	payload["id"] = id

	if spec.Type != "" {
		payload["type"] = kumaMonitorType(spec.Type)
	}
	if spec.Name != "" {
		payload["name"] = spec.Name
	}
	if spec.URL != "" {
		payload["url"] = spec.URL
	}
	if spec.DockerContainer != "" {
		payload["docker_container"] = spec.DockerContainer
	}
	if spec.DockerHostID > 0 {
		payload["docker_host"] = spec.DockerHostID
	}
	if spec.Interval > 0 {
		payload["interval"] = spec.Interval
	}
	if spec.RetryInterval > 0 {
		payload["retryInterval"] = spec.RetryInterval
	}
	if spec.MaxRetries > 0 {
		payload["maxretries"] = spec.MaxRetries
	}
	if len(spec.AcceptedStatusCodes) > 0 {
		payload["accepted_statuscodes"] = spec.AcceptedStatusCodes
	}
	if spec.Description != "" {
		payload["description"] = spec.Description
	}

	ensureAcceptedCodes(payload)
	return payload
}

// ensureAcceptedCodes guarantees the edit payload carries a string array.
func ensureAcceptedCodes(payload map[string]any) {
	v, ok := payload["accepted_statuscodes"]
	if !ok || v == nil {
		payload["accepted_statuscodes"] = []string{defaultAcceptedCodes}
		return
	}
	switch codes := v.(type) {
	case []string:
		if len(codes) == 0 {
			payload["accepted_statuscodes"] = []string{defaultAcceptedCodes}
		}
	case []any:
		out := make([]string, 0, len(codes))
		for _, c := range codes {
			if s, ok := c.(string); ok {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			out = []string{defaultAcceptedCodes}
		}
		payload["accepted_statuscodes"] = out
	}
}

// stateWarmup bounds how long a command session waits for Kuma's post-login
// tagList/dockerHostList pushes before proceeding.
var stateWarmup = 300 * time.Millisecond

// sessionForCommand opens an authenticated session and switches it to drain
// mode so command acks can never be blocked by an undrained event stream.
// Buffered post-login events are replayed into the collector first, and a
// short warm-up captures tagList/dockerHostList when they arrive after the
// login ack.
func sessionForCommand(ctx context.Context, cfg connConfig) (*sioSession, *monitorCollector, error) {
	s, err := newSIOSession(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	col := newMonitorCollector()
	for _, ev := range s.takePending() {
		col.handle(ev)
	}
	if !col.hostListSeen || col.tags == nil {
		timer := time.NewTimer(stateWarmup)
		defer timer.Stop()
	warm:
		for {
			select {
			case <-ctx.Done():
				break warm
			case <-s.cli.done:
				break warm
			case ev := <-s.events:
				col.handle(ev)
				if col.hostListSeen && col.tags != nil {
					break warm
				}
			case <-timer.C:
				break warm
			}
		}
	}
	s.startDrain()
	return s, col, nil
}

// pickDockerHost resolves the docker host FK. 0 means "first configured host".
func pickDockerHost(col *monitorCollector, requested int) int {
	if requested > 0 {
		return requested
	}
	if len(col.dockerHosts) > 0 {
		return col.dockerHosts[0].ID
	}
	return 0
}

// resolveTagID maps a tag name to its id, creating the tag when Kuma does not
// have it yet. Returns 0 when the tag cannot be resolved.
func resolveTagID(ctx context.Context, s *sioSession, col *monitorCollector, name string) int {
	for _, t := range col.tags {
		if t.Name == name {
			return t.ID
		}
	}
	resp, err := s.command(ctx, "addTag", map[string]any{"name": name})
	if err != nil {
		logging.LogWarn("kuma", "Failed to create Kuma tag",
			slog.String("tag", name), slog.String("error", err.Error()))
		return 0
	}
	var r struct {
		Ok  bool       `json:"ok"`
		Msg string     `json:"msg"`
		Tag MonitorTag `json:"tag"`
	}
	if len(resp) == 0 || json.Unmarshal(resp[0], &r) != nil || !r.Ok || r.Tag.ID == 0 {
		logging.LogWarn("kuma", "Kuma tag create rejected",
			slog.String("tag", name), slog.String("msg", r.Msg))
		return 0
	}
	col.tags = append(col.tags, r.Tag)
	return r.Tag.ID
}

// addMonitor creates a monitor and verifies it landed. A timeout or a failed
// verification after the ack is reported as UnverifiedWriteError: the write
// may have happened, so callers must re-read rather than blind-retry.
func addMonitor(ctx context.Context, cfg connConfig, spec MonitorSpec) (int, error) {
	if spec.Name == "" {
		return 0, errors.New("add monitor: name is required")
	}
	if kumaMonitorType(spec.Type) == "" {
		return 0, errors.New("add monitor: type is required")
	}
	spec = applyMonitorDefaults(spec)

	s, col, err := sessionForCommand(ctx, cfg)
	if err != nil {
		return 0, err
	}
	defer s.close()

	payload := buildAddPayload(spec, pickDockerHost(col, spec.DockerHostID))

	ackCh, err := s.cli.emitWithAck("add", payload)
	if err != nil {
		return 0, fmt.Errorf("add monitor: %w", err)
	}
	resp, err := awaitAck(ctx, s.cli, ackCh, ackTimeout)
	if err != nil {
		// The add may have been applied server-side; never invite a retry.
		return 0, &UnverifiedWriteError{Op: "add monitor", Err: err}
	}

	var r struct {
		Ok        bool   `json:"ok"`
		Msg       string `json:"msg"`
		MonitorID int    `json:"monitorID"`
	}
	if len(resp) == 0 || json.Unmarshal(resp[0], &r) != nil {
		return 0, &UnverifiedWriteError{Op: "add monitor", Err: errors.New("unexpected response format")}
	}
	if !r.Ok {
		if r.Msg == "" {
			r.Msg = "add monitor rejected"
		}
		return 0, fmt.Errorf("add monitor rejected: %s", r.Msg)
	}

	monitorID := r.MonitorID
	if monitorID == 0 {
		// Older Kuma embedded the id at the start of msg.
		fmt.Sscanf(r.Msg, "%d", &monitorID)
	}
	if monitorID <= 0 {
		return 0, &UnverifiedWriteError{Op: "add monitor", Err: errors.New("ack did not return a monitor id")}
	}

	// Verify the create landed before reporting success.
	if _, err := s.getMonitor(ctx, monitorID); err != nil {
		return monitorID, &UnverifiedWriteError{Op: "add monitor", MonitorID: monitorID, Err: err}
	}

	for _, name := range spec.Tags {
		tagID := resolveTagID(ctx, s, col, name)
		if tagID == 0 {
			continue
		}
		if ok, msg, err := s.commandOK(ctx, "addMonitorTag", map[string]any{"monitorID": monitorID, "tagID": tagID}); err != nil {
			logging.LogWarn("kuma", "Failed to tag new monitor",
				slog.Int("monitor_id", monitorID), slog.String("tag", name), slog.String("error", err.Error()))
		} else if !ok {
			logging.LogWarn("kuma", "Kuma rejected tag on new monitor",
				slog.Int("monitor_id", monitorID), slog.String("tag", name), slog.String("msg", msg))
		}
	}

	logging.LogInfo("kuma", "Monitor added via Socket.IO",
		slog.String("name", spec.Name), slog.Int("monitor_id", monitorID))
	return monitorID, nil
}

// editMonitorPayload is the legacy map-payload path: fetch the current object,
// merge the caller's fields, send the complete object.
func editMonitorPayload(ctx context.Context, cfg connConfig, monitorID int, payload map[string]any) error {
	s, _, err := sessionForCommand(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.close()

	current, err := s.getMonitor(ctx, monitorID)
	if err != nil {
		return err
	}
	for k, v := range payload {
		current[k] = v
	}
	current["id"] = monitorID
	ensureAcceptedCodes(current)

	ok, msg, err := s.commandOK(ctx, "editMonitor", current)
	if err != nil {
		return &UnverifiedWriteError{Op: "edit monitor", MonitorID: monitorID, Err: err}
	}
	if !ok {
		return fmt.Errorf("edit monitor rejected: %s", msg)
	}
	logging.LogInfo("kuma", "Monitor edited via Socket.IO", slog.Int("monitor_id", monitorID))
	return nil
}

// editMonitorSpec merges a MonitorSpec into the current monitor and applies
// tag changes (spec.Tags non-nil means "exactly these tag names").
func editMonitorSpec(ctx context.Context, cfg connConfig, monitorID int, spec MonitorSpec) error {
	s, col, err := sessionForCommand(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.close()

	current, err := s.getMonitor(ctx, monitorID)
	if err != nil {
		return err
	}
	payload := buildEditSpecPayload(current, monitorID, spec)

	ok, msg, err := s.commandOK(ctx, "editMonitor", payload)
	if err != nil {
		return &UnverifiedWriteError{Op: "edit monitor", MonitorID: monitorID, Err: err}
	}
	if !ok {
		return fmt.Errorf("edit monitor rejected: %s", msg)
	}

	if spec.Tags != nil {
		if err := syncMonitorTags(ctx, s, col, monitorID, current, spec.Tags); err != nil {
			logging.LogWarn("kuma", "Failed to sync monitor tags",
				slog.Int("monitor_id", monitorID), slog.String("error", err.Error()))
		}
	}

	logging.LogInfo("kuma", "Monitor edited via Socket.IO", slog.Int("monitor_id", monitorID))
	return nil
}

// syncMonitorTags adds missing tags and removes tags not requested. Best
// effort: a tag failure never turns a successful edit into a retryable error.
func syncMonitorTags(ctx context.Context, s *sioSession, col *monitorCollector, monitorID int, current map[string]any, names []string) error {
	desired := make(map[int]bool, len(names))
	for _, name := range names {
		if id := resolveTagID(ctx, s, col, name); id != 0 {
			desired[id] = true
		}
	}
	for id := range tagIDsFromMonitor(current) {
		if !desired[id] {
			_, _, _ = s.commandOK(ctx, "deleteMonitorTag", map[string]any{"monitorID": monitorID, "tagID": id})
		}
	}
	for id := range desired {
		if tagIDsFromMonitor(current)[id] {
			continue
		}
		_, _, _ = s.commandOK(ctx, "addMonitorTag", map[string]any{"monitorID": monitorID, "tagID": id})
	}
	return nil
}

// tagIDsFromMonitor extracts tag ids from a raw Kuma monitor object.
func tagIDsFromMonitor(m map[string]any) map[int]bool {
	out := map[int]bool{}
	raw, ok := m["tags"]
	if !ok {
		return out
	}
	entries, ok := raw.([]any)
	if !ok {
		return out
	}
	for _, e := range entries {
		obj, ok := e.(map[string]any)
		if !ok {
			continue
		}
		switch id := obj["id"].(type) {
		case float64:
			out[int(id)] = true
		case json.Number:
			if n, err := id.Int64(); err == nil {
				out[int(n)] = true
			}
		}
	}
	return out
}

func deleteMonitor(ctx context.Context, cfg connConfig, monitorID int) error {
	s, _, err := sessionForCommand(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.close()
	ok, msg, err := s.commandOK(ctx, "deleteMonitor", monitorID)
	if err != nil {
		if errors.Is(err, ErrSocketClosed) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return &UnverifiedWriteError{Op: "delete monitor", MonitorID: monitorID, Err: err}
		}
		return err
	}
	if !ok {
		return fmt.Errorf("delete monitor rejected: %s", msg)
	}
	logging.LogInfo("kuma", "Monitor deleted via Socket.IO", slog.Int("monitor_id", monitorID))
	return nil
}

func pauseMonitor(ctx context.Context, cfg connConfig, monitorID int) error {
	s, _, err := sessionForCommand(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.close()
	ok, msg, err := s.commandOK(ctx, "pauseMonitor", monitorID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("pause monitor rejected: %s", msg)
	}
	logging.LogInfo("kuma", "Monitor paused via Socket.IO", slog.Int("monitor_id", monitorID))
	return nil
}

func resumeMonitor(ctx context.Context, cfg connConfig, monitorID int) error {
	s, _, err := sessionForCommand(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.close()
	ok, msg, err := s.commandOK(ctx, "resumeMonitor", monitorID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("resume monitor rejected: %s", msg)
	}
	logging.LogInfo("kuma", "Monitor resumed via Socket.IO", slog.Int("monitor_id", monitorID))
	return nil
}

func addMonitorTag(ctx context.Context, cfg connConfig, monitorID, tagID int) error {
	s, _, err := sessionForCommand(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.close()
	ok, msg, err := s.commandOK(ctx, "addMonitorTag", map[string]any{"monitorID": monitorID, "tagID": tagID})
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("add monitor tag rejected: %s", msg)
	}
	logging.LogInfo("kuma", "Monitor tag added via Socket.IO", slog.Int("monitor_id", monitorID), slog.Int("tag_id", tagID))
	return nil
}

func deleteMonitorTag(ctx context.Context, cfg connConfig, monitorID, tagID int) error {
	s, _, err := sessionForCommand(ctx, cfg)
	if err != nil {
		return err
	}
	defer s.close()
	ok, msg, err := s.commandOK(ctx, "deleteMonitorTag", map[string]any{"monitorID": monitorID, "tagID": tagID})
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("delete monitor tag rejected: %s", msg)
	}
	logging.LogInfo("kuma", "Monitor tag deleted via Socket.IO", slog.Int("monitor_id", monitorID), slog.Int("tag_id", tagID))
	return nil
}

// --- Back-compat one-shot wrappers (lane B migrates callers) ---

func oneShotCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), commandSessionTimeout)
}

// AddMonitorViaSocketIO creates a monitor in Kuma via the Socket.IO "add"
// event and verifies it landed.
func AddMonitorViaSocketIO(kumaURL, username, password string, monitorType, name, url, dockerContainer string, dockerHostID int) (int, error) {
	ctx, cancel := oneShotCtx()
	defer cancel()
	return addMonitor(ctx, connConfig{URL: kumaURL, Username: username, Password: password}, MonitorSpec{
		Type:            monitorType,
		Name:            name,
		URL:             url,
		DockerContainer: dockerContainer,
		DockerHostID:    dockerHostID,
	})
}

// DeleteMonitorViaSocketIO removes a monitor via Socket.IO.
func DeleteMonitorViaSocketIO(kumaURL, username, password string, monitorID int) error {
	ctx, cancel := oneShotCtx()
	defer cancel()
	return deleteMonitor(ctx, connConfig{URL: kumaURL, Username: username, Password: password}, monitorID)
}

// EditMonitorViaSocketIO merges payload into the current monitor definition.
func EditMonitorViaSocketIO(kumaURL, username, password string, monitorID int, payload map[string]any) error {
	ctx, cancel := oneShotCtx()
	defer cancel()
	return editMonitorPayload(ctx, connConfig{URL: kumaURL, Username: username, Password: password}, monitorID, payload)
}

// PauseMonitorViaSocketIO pauses a monitor via Socket.IO.
func PauseMonitorViaSocketIO(kumaURL, username, password string, monitorID int) error {
	ctx, cancel := oneShotCtx()
	defer cancel()
	return pauseMonitor(ctx, connConfig{URL: kumaURL, Username: username, Password: password}, monitorID)
}

// ResumeMonitorViaSocketIO resumes a paused monitor via Socket.IO.
func ResumeMonitorViaSocketIO(kumaURL, username, password string, monitorID int) error {
	ctx, cancel := oneShotCtx()
	defer cancel()
	return resumeMonitor(ctx, connConfig{URL: kumaURL, Username: username, Password: password}, monitorID)
}

// AddMonitorTagViaSocketIO applies a tag to a monitor via Socket.IO.
func AddMonitorTagViaSocketIO(kumaURL, username, password string, monitorID, tagID int) error {
	ctx, cancel := oneShotCtx()
	defer cancel()
	return addMonitorTag(ctx, connConfig{URL: kumaURL, Username: username, Password: password}, monitorID, tagID)
}

// DeleteMonitorTagViaSocketIO removes a tag from a monitor via Socket.IO.
func DeleteMonitorTagViaSocketIO(kumaURL, username, password string, monitorID, tagID int) error {
	ctx, cancel := oneShotCtx()
	defer cancel()
	return deleteMonitorTag(ctx, connConfig{URL: kumaURL, Username: username, Password: password}, monitorID, tagID)
}
