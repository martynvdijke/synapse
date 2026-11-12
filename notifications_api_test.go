package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNotificationsPreferencesEndpoints(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// Defaults are all enabled.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authRequest(t, "GET", "/api/notifications", "", sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/notifications status %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		Preferences  map[string]bool `json:"preferences"`
		Capabilities map[string]any  `json:"capabilities"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"enabled", "web_push", "fcm", "new_item"} {
		if !got.Preferences[k] {
			t.Errorf("default %s = false, want true", k)
		}
	}
	for _, k := range []string{"push_enabled", "notify_new_item", "web_push_enabled", "fcm_enabled", "vapid_public_key", "fcm_project_id", "fcm_android_channel_id"} {
		if _, ok := got.Capabilities[k]; !ok {
			t.Errorf("capabilities missing %q", k)
		}
	}

	// Persist a partial update; omitted fields keep their stored values.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authRequest(t, "PUT", "/api/notifications/preferences", `{"web_push":false}`, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT preferences status %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authRequest(t, "GET", "/api/notifications", "", sessionID))
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode after update: %v", err)
	}
	if got.Preferences["web_push"] {
		t.Error("web_push should be false after update")
	}
	if !got.Preferences["enabled"] || !got.Preferences["fcm"] || !got.Preferences["new_item"] {
		t.Errorf("omitted prefs were reset: %+v", got.Preferences)
	}
}

func TestPushSubscriptionAndDeviceEndpoints(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// Register a subscription.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authRequest(t, "POST", "/api/notifications/subscriptions",
		`{"endpoint":"https://push.example/1","keys":{"p256dh":"pk","auth":"au"}}`, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("subscribe status %d: %s", w.Code, w.Body.String())
	}
	var subResp struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &subResp)
	if subResp.ID == 0 {
		t.Fatal("expected subscription id")
	}

	// Reject missing keys.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authRequest(t, "POST", "/api/notifications/subscriptions", `{"endpoint":"https://push.example/2"}`, sessionID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing keys, got %d", w.Code)
	}

	// Register a device (default platform android).
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authRequest(t, "POST", "/api/notifications/devices", `{"token":"tok-1","device_name":"Pixel"}`, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("register device status %d: %s", w.Code, w.Body.String())
	}
	var devResp struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &devResp)
	if devResp.ID == 0 {
		t.Fatal("expected device id")
	}
	devices, _ := app.database.ListAllPushDevices()
	if len(devices) != 1 || devices[0].Platform != "android" {
		t.Fatalf("unexpected devices: %+v", devices)
	}

	// Delete device and subscription.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authRequest(t, "DELETE", "/api/notifications/devices/"+itoaTest(devResp.ID), "", sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("delete device status %d: %s", w.Code, w.Body.String())
	}
	if devices, _ := app.database.ListAllPushDevices(); len(devices) != 0 {
		t.Fatal("device not deleted")
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, authRequest(t, "DELETE", "/api/notifications/subscriptions", `{"endpoint":"https://push.example/1"}`, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("delete subscription status %d: %s", w.Code, w.Body.String())
	}
	if subs, _ := app.database.ListAllPushSubscriptions(); len(subs) != 0 {
		t.Fatal("subscription not deleted")
	}
}

func itoaTest(n int64) string {
	return strconv.FormatInt(n, 10)
}

func TestSettingsPushSecretsMaskedAndKept(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	body := `{"push_enabled":true,"web_push_enabled":true,"web_push_vapid_private_key":"super-secret","fcm_service_account_json":"{\"client_email\":\"x\"}"}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authRequest(t, "POST", "/api/settings", body, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("save settings status %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, authRequest(t, "GET", "/api/settings", "", sessionID))
	raw := w.Body.String()
	if strings.Contains(raw, "super-secret") {
		t.Fatal("GET /api/settings leaked the VAPID private key")
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if view["web_push_vapid_private_key"] != "****" {
		t.Errorf("private key not masked: %v", view["web_push_vapid_private_key"])
	}
	if view["web_push_vapid_private_key_set"] != true {
		t.Errorf("private key set flag = %v", view["web_push_vapid_private_key_set"])
	}
	if view["fcm_service_account_json_set"] != true {
		t.Errorf("fcm set flag = %v", view["fcm_service_account_json_set"])
	}

	// Posting the mask back must keep the stored secret.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authRequest(t, "POST", "/api/settings", `{"web_push_vapid_private_key":"****","fcm_service_account_json":"****"}`, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("masked save status %d: %s", w.Code, w.Body.String())
	}
	s := app.settings()
	if s.WebPushVAPIDPrivateKey != "super-secret" {
		t.Errorf("stored VAPID key changed: %q", s.WebPushVAPIDPrivateKey)
	}
	if s.FCMServiceAccountJSON != `{"client_email":"x"}` {
		t.Errorf("stored FCM JSON changed: %q", s.FCMServiceAccountJSON)
	}
	if !s.PushEnabled || !s.WebPushEnabled {
		t.Error("booleans did not persist")
	}
}

func TestNotificationsStreamSmoke(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/notifications/stream", nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not exit after context cancellation")
	}

	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := w.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q", got)
	}
	if !strings.Contains(w.Body.String(), "retry: 5000") {
		t.Errorf("missing retry line in %q", w.Body.String())
	}
}
