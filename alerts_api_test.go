package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"synapse/internal/db"
)

// createRule posts a rule and returns the response recorder.
func createRule(t *testing.T, r http.Handler, sessionID, body string) *httptest.ResponseRecorder {
	req := authRequest(t, "POST", "/api/alert-rules", body, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAlertRule_CreateWithDurationThreshold(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	w := createRule(t, r, sessionID, `{"name":"kuma-down","type":"monitor_down_for","subject":"plex","threshold":"10m"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var rule db.AlertRule
	if err := json.Unmarshal(w.Body.Bytes(), &rule); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rule.ID == 0 {
		t.Errorf("expected persisted id, got %d", rule.ID)
	}
	if !rule.Enabled {
		t.Errorf("expected enabled=true by default")
	}
	if rule.Threshold != 600 {
		t.Errorf("expected threshold_seconds=600, got %d", rule.Threshold)
	}

	// List shows it.
	req := authRequest(t, "GET", "/api/alert-rules", "", sessionID)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req)
	var rules []db.AlertRule
	if err := json.Unmarshal(w2.Body.Bytes(), &rules); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(rules) != 1 || rules[0].Name != "kuma-down" {
		t.Errorf("expected one kuma-down rule, got %+v", rules)
	}
}

func TestAlertRule_DuplicateNameRejected(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	body := `{"name":"dup","type":"reconcile_drift"}`
	if w := createRule(t, r, sessionID, body); w.Code != http.StatusCreated {
		t.Fatalf("first create expected 201, got %d", w.Code)
	}
	w := createRule(t, r, sessionID, body)
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAlertRule_InvalidTypeRejected(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	w := createRule(t, r, sessionID, `{"name":"bad","type":"nope"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid type expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAlertRule_SubjectAndThresholdValidation(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	cases := []struct {
		name string
		body string
	}{
		{"missing subject", `{"name":"a","type":"monitor_down_for","threshold":"5m"}`},
		{"missing threshold", `{"name":"b","type":"container_down","subject":"web"}`},
		{"bad sync subject", `{"name":"c","type":"sync_stale","subject":"kuma","threshold":"1h"}`},
		{"zero threshold", `{"name":"d","type":"monitor_down_for","subject":"x","threshold":0}`},
	}
	for _, tc := range cases {
		if w := createRule(t, r, sessionID, tc.body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", tc.name, w.Code)
		}
	}
}

func TestAlertRule_UpdateAndDelete(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	if w := createRule(t, r, sessionID, `{"name":"rule1","type":"monitor_down_for","subject":"plex","threshold":"5m"}`); w.Code != http.StatusCreated {
		t.Fatalf("create: %d", w.Code)
	}
	if w := createRule(t, r, sessionID, `{"name":"rule2","type":"monitor_down_for","subject":"jelly","threshold":"5m"}`); w.Code != http.StatusCreated {
		t.Fatalf("create2: %d", w.Code)
	}

	// Rename to an existing name conflicts.
	req := authRequest(t, "PUT", "/api/alert-rules/2", `{"name":"rule1"}`, sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("rename conflict expected 409, got %d", w.Code)
	}

	// Disable rule 1.
	req = authRequest(t, "PUT", "/api/alert-rules/1", `{"enabled":false}`, sessionID)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("disable expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var rule db.AlertRule
	json.Unmarshal(w.Body.Bytes(), &rule)
	if rule.Enabled {
		t.Errorf("expected enabled=false after update")
	}

	// Delete rule 2.
	req = authRequest(t, "DELETE", "/api/alert-rules/2", "", sessionID)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("delete expected 200, got %d", w.Code)
	}
	req = authRequest(t, "DELETE", "/api/alert-rules/999", "", sessionID)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("delete missing expected 404, got %d", w.Code)
	}
}

func TestIncidents_LifecycleTransitions(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	now := time.Now()
	ruleID, err := app.database.CreateAlertRule(&db.AlertRule{Name: "plex-down", Type: db.AlertTypeMonitorDownFor, Subject: "plex", Threshold: 600, Enabled: true})
	if err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	inc, created, err := app.database.OpenIncident(ruleID, "plex", "plex down 10m", now)
	if err != nil || !created {
		t.Fatalf("open incident: created=%v err=%v", created, err)
	}

	// Filtered listing.
	req := authRequest(t, "GET", "/api/incidents?status=open", "", sessionID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var list []db.AlertIncident
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Status != "open" || list[0].RuleName != "" && list[0].Subject != "plex" {
		t.Fatalf("expected one open plex incident, got %+v", list)
	}

	// Ack.
	req = authRequest(t, "POST", "/api/incidents/"+strconv.FormatInt(inc.ID, 10)+"/ack", "", sessionID)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ack expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var acked db.AlertIncident
	json.Unmarshal(w.Body.Bytes(), &acked)
	if acked.Status != "acknowledged" || acked.AckAt == nil {
		t.Errorf("expected acknowledged with ack_at, got %+v", acked)
	}

	// Ack again fails (not open anymore).
	req = authRequest(t, "POST", "/api/incidents/"+strconv.FormatInt(inc.ID, 10)+"/ack", "", sessionID)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("second ack expected 409, got %d", w.Code)
	}

	// Resolve.
	req = authRequest(t, "POST", "/api/incidents/"+strconv.FormatInt(inc.ID, 10)+"/resolve", "", sessionID)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve expected 200, got %d", w.Code)
	}
	var resolved db.AlertIncident
	json.Unmarshal(w.Body.Bytes(), &resolved)
	if resolved.Status != "resolved" || resolved.ResolvedAt == nil {
		t.Errorf("expected resolved with resolved_at, got %+v", resolved)
	}

	// Open filter no longer matches.
	req = authRequest(t, "GET", "/api/incidents?status=open", "", sessionID)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	list = nil
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Errorf("expected no open incidents after resolve, got %d", len(list))
	}
}
