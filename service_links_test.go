package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"synapse/internal/db"
	"synapse/internal/kuma"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// mockNPMServer returns an httptest server that fakes the NPM API used by
// internal/npm.Client: /api/tokens (login), /api/nginx/proxy-hosts
// (GET list / POST create / PUT update). hosts holds the current proxy hosts.
func mockNPMServer(t *testing.T, hosts *[]map[string]any) *httptest.Server {
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/tokens":
			fmt.Fprintf(w, `{"token":"test-token","expires":%q}`,
				time.Now().Add(24*time.Hour).Format(time.RFC3339))
		case r.Method == http.MethodGet && r.URL.Path == "/api/nginx/proxy-hosts":
			json.NewEncoder(w).Encode(*hosts)
		case r.Method == http.MethodPost && r.URL.Path == "/api/nginx/proxy-hosts":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			body["id"] = 42
			*hosts = append(*hosts, body)
			json.NewEncoder(w).Encode(body)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/nginx/proxy-hosts/"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			body["id"] = 42
			json.NewEncoder(w).Encode(body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// installKumaHooks installs package-level kuma client test hooks backed by a
// mutable monitor slice. The edit hook applies payload changes to the slice so
// a subsequent QueryMonitorsViaSocketIO reflects the update.
func installKumaHooks(t *testing.T, monitors *[]kuma.KumaMonitor) (added *[]struct {
	Type, Name, URL, Container string
	HostID                     int
}) {
	restoreLogin := kuma.SetVerifySocketIOLoginTestHook(func(url, user, pass string) error { return nil })
	t.Cleanup(restoreLogin)
	restoreQ := kuma.SetQueryMonitorsTestHook(func(url, user, pass string) ([]kuma.KumaMonitor, error) {
		return *monitors, nil
	})
	t.Cleanup(restoreQ)
	added = new([]struct {
		Type, Name, URL, Container string
		HostID                     int
	})
	restoreAdd := kuma.SetAddMonitorTestHook(func(url, user, pass, monitorType, name, monURL, container string, hostID int) (int, error) {
		*added = append(*added, struct {
			Type, Name, URL, Container string
			HostID                     int
		}{monitorType, name, monURL, container, hostID})
		*monitors = append(*monitors, kuma.KumaMonitor{ID: 9, Name: name, Type: monitorType, URL: monURL, DockerContainer: container})
		return 9, nil
	})
	t.Cleanup(restoreAdd)
	restoreEdit := kuma.SetEditMonitorTestHook(func(url, user, pass string, monitorID int, payload map[string]any) error {
		for i := range *monitors {
			if (*monitors)[i].ID != monitorID {
				continue
			}
			if v, ok := payload["name"].(string); ok {
				(*monitors)[i].Name = v
			}
			if v, ok := payload["type"].(string); ok {
				(*monitors)[i].Type = v
			}
			if v, ok := payload["url"].(string); ok {
				(*monitors)[i].URL = v
			}
			if v, ok := payload["docker_container"].(string); ok {
				(*monitors)[i].DockerContainer = v
			}
			if v, ok := payload["interval"].(int); ok {
				(*monitors)[i].Interval = v
			}
			if v, ok := payload["retryInterval"].(int); ok {
				(*monitors)[i].RetryInterval = v
			}
			if v, ok := payload["maxretries"].(int); ok {
				(*monitors)[i].MaxRetries = v
			}
		}
		return nil
	})
	t.Cleanup(restoreEdit)
	restoreDel := kuma.SetDeleteMonitorTestHook(func(url, user, pass string, monitorID int) error {
		for i := range *monitors {
			if (*monitors)[i].ID == monitorID {
				*monitors = append((*monitors)[:i], (*monitors)[i+1:]...)
				break
			}
		}
		return nil
	})
	t.Cleanup(restoreDel)
	return added
}

func doJSON(t *testing.T, r http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// service links
// ---------------------------------------------------------------------------

// TestServiceLinksCRUD covers create -> list -> upsert -> update ->
// refresh -> delete.
func TestServiceLinksCRUD(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	hosts := []map[string]any{
		{"id": 1, "domain_names": []string{"app.example.com"}, "forward_host": "nginx-web",
			"forward_port": 80, "forward_scheme": "http", "enabled": true},
		{"id": 2, "domain_names": []string{"api.example.com"}, "forward_host": "myapp-api",
			"forward_port": 8080, "forward_scheme": "http", "enabled": true},
	}
	srv := mockNPMServer(t, &hosts)

	monitors := []kuma.KumaMonitor{
		{ID: 1, Name: "web-monitor", Type: "http", URL: "http://localhost:80/health"},
		{ID: 2, Name: "api-monitor", Type: "http", URL: "http://localhost:8080/api/health"},
	}
	installKumaHooks(t, &monitors)

	npmInst, err := app.database.CreateNPMInstance(&db.NPMInstance{
		Name: "npm-test", URL: srv.URL, Username: "admin", Password: "p", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create npm instance: %v", err)
	}
	kumaInst, err := app.database.CreateKumaInstance(&db.KumaInstance{
		Name: "kuma-test", URL: "http://kuma:3001", Username: "admin", Password: "p", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create kuma instance: %v", err)
	}
	npmID := int(npmInst.ID)
	kumaID := int(kumaInst.ID)

	// Create a link for the "web" service on both sides.
	createBody := fmt.Sprintf(
		`{"service_name":"web","npm_instance_id":%d,"npm_host_name":"app.example.com","kuma_instance_id":%d,"kuma_monitor_id":1}`,
		npmID, kumaID)
	w := doJSON(t, r, authRequest(t, "POST", "/api/service-links", createBody, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("create link: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var created map[string]any
	json.NewDecoder(w.Body).Decode(&created)
	created = created["link"].(map[string]any)
	if created["service_name"] != "web" {
		t.Errorf("expected service_name web, got %v", created["service_name"])
	}
	if created["npm_host_name"] != "app.example.com" {
		t.Errorf("expected npm_host_name app.example.com, got %v", created["npm_host_name"])
	}
	if created["npm_details"] == nil {
		t.Errorf("expected npm_details snapshot to be cached")
	}
	if created["kuma_monitor_name"] != "web-monitor" {
		t.Errorf("expected kuma_monitor_name web-monitor, got %v", created["kuma_monitor_name"])
	}
	if created["kuma_details"] == nil {
		t.Errorf("expected kuma_details snapshot to be cached")
	}
	linkID := int64(created["id"].(float64))

	// Upsert: same service with different kuma monitor -> still one link.
	upsertBody := fmt.Sprintf(
		`{"service_name":"web","npm_instance_id":%d,"npm_host_name":"app.example.com","kuma_instance_id":%d,"kuma_monitor_id":2}`,
		npmID, kumaID)
	w = doJSON(t, r, authRequest(t, "POST", "/api/service-links", upsertBody, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("upsert link: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = doJSON(t, r, authRequest(t, "GET", "/api/service-links", "", sessionID))
	var links []map[string]any
	json.NewDecoder(w.Body).Decode(&links)
	if len(links) != 1 {
		t.Fatalf("expected 1 link after upsert, got %d", len(links))
	}
	if links[0]["kuma_monitor_name"] != "api-monitor" {
		t.Errorf("expected kuma_monitor_name api-monitor after upsert, got %v", links[0]["kuma_monitor_name"])
	}

	// Update: switch npm host to api.example.com and kuma monitor back to 1.
	updateBody := fmt.Sprintf(
		`{"npm_instance_id":%d,"npm_host_name":"api.example.com","kuma_instance_id":%d,"kuma_monitor_id":1}`,
		npmID, kumaID)
	req := authRequest(t, "PUT", fmt.Sprintf("/api/service-links/%d", linkID), updateBody, sessionID)
	w = doJSON(t, r, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update link: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated map[string]any
	json.NewDecoder(w.Body).Decode(&updated)
	updated = updated["link"].(map[string]any)
	if updated["npm_host_name"] != "api.example.com" {
		t.Errorf("expected npm_host_name api.example.com after update, got %v", updated["npm_host_name"])
	}
	if updated["kuma_monitor_name"] != "web-monitor" {
		t.Errorf("expected kuma_monitor_name web-monitor after update, got %v", updated["kuma_monitor_name"])
	}

	// Refresh re-resolves and re-caches snapshots.
	w = doJSON(t, r, authRequest(t, "POST", fmt.Sprintf("/api/service-links/%d/refresh", linkID), "", sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("refresh link: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var refreshed map[string]any
	json.NewDecoder(w.Body).Decode(&refreshed)
	if refreshed["npm_host_name"] != "api.example.com" {
		t.Errorf("expected npm_host_name api.example.com after refresh, got %v", refreshed["npm_host_name"])
	}

	// Delete.
	w = doJSON(t, r, authRequest(t, "DELETE", fmt.Sprintf("/api/service-links/%d", linkID), "", sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("delete link: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = doJSON(t, r, authRequest(t, "GET", "/api/service-links", "", sessionID))
	json.NewDecoder(w.Body).Decode(&links)
	if len(links) != 0 {
		t.Errorf("expected 0 links after delete, got %d", len(links))
	}
}

func TestServiceLinkValidation(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	// Unknown service in compose.
	w := doJSON(t, r, authRequest(t, "POST", "/api/service-links",
		`{"service_name":"does-not-exist"}`, sessionID))
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown service: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Missing service_name.
	w = doJSON(t, r, authRequest(t, "POST", "/api/service-links", `{}`, sessionID))
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing service_name: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Unknown npm instance.
	w = doJSON(t, r, authRequest(t, "POST", "/api/service-links",
		`{"service_name":"web","npm_instance_id":9999,"npm_host_name":"x.example.com"}`, sessionID))
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown npm instance: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// npm proxy hosts
// ---------------------------------------------------------------------------

func TestNPMProxyHostsEndpoints(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	hosts := []map[string]any{
		{"id": 1, "domain_names": []string{"app.example.com"}, "forward_host": "nginx-web",
			"forward_port": 80, "forward_scheme": "http", "enabled": true},
	}
	srv := mockNPMServer(t, &hosts)

	inst, err := app.database.CreateNPMInstance(&db.NPMInstance{
		Name: "npm-test", URL: srv.URL, Username: "admin", Password: "p", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create npm instance: %v", err)
	}
	_, err = app.database.CreateNPMInstance(&db.NPMInstance{
		Name: "npm-disabled", URL: srv.URL, Username: "admin", Password: "p", Enabled: false,
	})
	if err != nil {
		t.Fatalf("create disabled npm instance: %v", err)
	}
	instID := int(inst.ID)

	// GET aggregate.
	w := doJSON(t, r, authRequest(t, "GET", "/api/npm/proxy-hosts", "", sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("list hosts: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list []map[string]any
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("expected 1 host, got %d", len(list))
	}
	if list[0]["instance_id"].(float64) != float64(instID) {
		t.Errorf("expected instance_id %d, got %v", instID, list[0]["instance_id"])
	}
	if list[0]["instance_name"] != "npm-test" {
		t.Errorf("expected instance_name npm-test, got %v", list[0]["instance_name"])
	}

	// GET filtered by instance.
	w = doJSON(t, r, authRequest(t, "GET",
		fmt.Sprintf("/api/npm/proxy-hosts?instance=%d", instID), "", sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("list hosts filtered: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// POST create with explicit fields + service_name (derivation ignored since
	// testdata services have no synapse.* labels, but explicit fields present).
	createBody := fmt.Sprintf(
		`{"instance_id":%d,"service_name":"web","domain_names":["web.example.com"],"forward_host":"nginx-web","forward_port":80,"forward_scheme":"https"}`,
		instID)
	w = doJSON(t, r, authRequest(t, "POST", "/api/npm/proxy-hosts", createBody, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("create host: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var created map[string]any
	json.NewDecoder(w.Body).Decode(&created)
	if created["id"].(float64) != 42 {
		t.Errorf("expected created host id 42, got %v", created["id"])
	}
	if created["forward_scheme"] != "https" {
		t.Errorf("expected forward_scheme https, got %v", created["forward_scheme"])
	}

	// POST without domains -> 400.
	w = doJSON(t, r, authRequest(t, "POST", "/api/npm/proxy-hosts",
		fmt.Sprintf(`{"instance_id":%d,"forward_host":"nginx-web","forward_port":80}`, instID), sessionID))
	if w.Code != http.StatusBadRequest {
		t.Errorf("host without domains: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// POST to disabled instance -> 400.
	var disabledID2 int
	insts, _ := app.database.GetNPMInstances()
	for _, i := range insts {
		if !i.Enabled {
			disabledID2 = int(i.ID)
		}
	}
	w = doJSON(t, r, authRequest(t, "POST", "/api/npm/proxy-hosts",
		fmt.Sprintf(`{"instance_id":%d,"domain_names":["x.example.com"],"forward_host":"h"}`, disabledID2), sessionID))
	if w.Code != http.StatusBadRequest {
		t.Errorf("disabled instance: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// PUT update.
	updateBody := fmt.Sprintf(
		`{"instance_id":%d,"domain_names":["updated.example.com"],"forward_host":"nginx-web","forward_port":443,"forward_scheme":"https"}`,
		instID)
	w = doJSON(t, r, authRequest(t, "PUT", "/api/npm/proxy-hosts/42", updateBody, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("update host: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated map[string]any
	json.NewDecoder(w.Body).Decode(&updated)
	if updated["forward_port"].(float64) != 443 {
		t.Errorf("expected forward_port 443, got %v", updated["forward_port"])
	}
}

// ---------------------------------------------------------------------------
// kuma monitors
// ---------------------------------------------------------------------------

func TestKumaMonitorCRUD(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	monitors := []kuma.KumaMonitor{
		{ID: 1, Name: "web-monitor", Type: "http", URL: "http://localhost:80/health", Interval: 60, MaxRetries: 3},
	}
	added := installKumaHooks(t, &monitors)

	kumaInst, err := app.database.CreateKumaInstance(&db.KumaInstance{
		Name: "kuma-test", URL: "http://kuma:3001", Username: "admin", Password: "p", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create kuma instance: %v", err)
	}
	kumaID := int(kumaInst.ID)

	// POST create with explicit fields.
	createBody := fmt.Sprintf(
		`{"instance_id":%d,"name":"api-probe","type":"http","url":"http://localhost:8080/api/health","interval":30}`,
		kumaID)
	w := doJSON(t, r, authRequest(t, "POST", "/api/monitors", createBody, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("create monitor: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var created map[string]any
	json.NewDecoder(w.Body).Decode(&created)
	if created["id"].(float64) != 9 {
		t.Errorf("expected monitor id 9, got %v", created["id"])
	}
	if len(*added) != 1 || (*added)[0].Name != "api-probe" || (*added)[0].Type != "http" {
		t.Errorf("add hook not called with expected args: %+v", *added)
	}

	// POST create with service_name only -> derivation from compose healthcheck.
	w = doJSON(t, r, authRequest(t, "POST", "/api/monitors",
		fmt.Sprintf(`{"instance_id":%d,"service_name":"web","name":"web-derived"}`, kumaID), sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("create derived monitor: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(*added) != 2 {
		t.Fatalf("expected 2 add calls, got %d", len(*added))
	}
	if (*added)[1].Type != "http" || !strings.HasSuffix((*added)[1].URL, "/health") {
		t.Errorf("expected derived http monitor with healthcheck URL, got %+v", (*added)[1])
	}

	// POST without type (and no service derivation) -> 400.
	w = doJSON(t, r, authRequest(t, "POST", "/api/monitors",
		fmt.Sprintf(`{"instance_id":%d,"name":"no-type"}`, kumaID), sessionID))
	if w.Code != http.StatusBadRequest {
		t.Errorf("monitor without type: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// PUT rename (mutable slice reflects edit) + interval update.
	putBody := `{"name":"web-monitor-renamed","interval":120}`
	w = doJSON(t, r, authRequest(t, "PUT", fmt.Sprintf("/api/monitors/1?instance=%d", kumaID), putBody, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("rename monitor: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var renamed map[string]any
	json.NewDecoder(w.Body).Decode(&renamed)
	if renamed["name"] != "web-monitor-renamed" {
		t.Errorf("expected name web-monitor-renamed, got %v", renamed["name"])
	}
	if renamed["interval"].(float64) != 120 {
		t.Errorf("expected interval 120, got %v", renamed["interval"])
	}

	// PUT type change to docker without docker_container -> 400.
	w = doJSON(t, r, authRequest(t, "PUT", fmt.Sprintf("/api/monitors/1?instance=%d", kumaID),
		`{"type":"docker"}`, sessionID))
	if w.Code != http.StatusBadRequest {
		t.Errorf("type change without container: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// PUT unknown monitor -> 404.
	w = doJSON(t, r, authRequest(t, "PUT", fmt.Sprintf("/api/monitors/999?instance=%d", kumaID),
		`{"name":"nope"}`, sessionID))
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown monitor: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// PUT unknown instance -> 400.
	w = doJSON(t, r, authRequest(t, "PUT", "/api/monitors/1?instance=9999", `{"name":"x"}`, sessionID))
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown instance: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// DELETE monitor 1.
	w = doJSON(t, r, authRequest(t, "DELETE", fmt.Sprintf("/api/monitors/1?instance=%d", kumaID), "", sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("delete monitor: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var del map[string]any
	json.NewDecoder(w.Body).Decode(&del)
	if del["deleted"] != true {
		t.Errorf("expected deleted true, got %v", del["deleted"])
	}
	// Delete hook removed it from the slice.
	w = doJSON(t, r, authRequest(t, "GET", "/api/monitors", "", sessionID))
	var list []map[string]any
	json.NewDecoder(w.Body).Decode(&list)
	for _, m := range list {
		if m["id"].(float64) == 1 {
			t.Errorf("monitor 1 should have been deleted")
		}
	}
}

func TestKumaMonitorRenameAndDeletePropagateToLinks(t *testing.T) {
	app, r := setupTest(t)
	sessionID := createTestSession(t, app)

	monitors := []kuma.KumaMonitor{
		{ID: 1, Name: "web-monitor", Type: "http", URL: "http://localhost:80/health"},
	}
	installKumaHooks(t, &monitors)

	kumaInst, err := app.database.CreateKumaInstance(&db.KumaInstance{
		Name: "kuma-test", URL: "http://kuma:3001", Username: "admin", Password: "p", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create kuma instance: %v", err)
	}
	kumaID := int(kumaInst.ID)

	// Link web -> monitor 1.
	createBody := fmt.Sprintf(`{"service_name":"web","kuma_instance_id":%d,"kuma_monitor_id":1}`, kumaID)
	w := doJSON(t, r, authRequest(t, "POST", "/api/service-links", createBody, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("create link: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var link map[string]any
	json.NewDecoder(w.Body).Decode(&link)
	link = link["link"].(map[string]any)
	if link["kuma_monitor_name"] != "web-monitor" {
		t.Fatalf("expected kuma_monitor_name web-monitor, got %v", link["kuma_monitor_name"])
	}

	// Rename monitor -> link should follow.
	w = doJSON(t, r, authRequest(t, "PUT", fmt.Sprintf("/api/monitors/1?instance=%d", kumaID),
		`{"name":"web-monitor-v2"}`, sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("rename monitor: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = doJSON(t, r, authRequest(t, "GET", "/api/service-links", "", sessionID))
	var links []map[string]any
	json.NewDecoder(w.Body).Decode(&links)
	if len(links) != 1 || links[0]["kuma_monitor_name"] != "web-monitor-v2" {
		t.Fatalf("expected link kuma_monitor_name web-monitor-v2, got %+v", links)
	}

	// Delete monitor -> link kuma side cleared.
	w = doJSON(t, r, authRequest(t, "DELETE", fmt.Sprintf("/api/monitors/1?instance=%d", kumaID), "", sessionID))
	if w.Code != http.StatusOK {
		t.Fatalf("delete monitor: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = doJSON(t, r, authRequest(t, "GET", "/api/service-links", "", sessionID))
	var linksAfter []map[string]any
	json.NewDecoder(w.Body).Decode(&linksAfter)
	if len(linksAfter) != 1 {
		t.Fatalf("expected 1 link, got %d", len(linksAfter))
	}
	if linksAfter[0]["kuma_monitor_id"].(float64) != 0 {
		t.Errorf("expected kuma_monitor_id cleared after monitor delete, got %+v", linksAfter[0])
	}
	if name, _ := linksAfter[0]["kuma_monitor_name"].(string); name != "" {
		t.Errorf("expected kuma_monitor_name cleared after monitor delete, got %+v", linksAfter[0])
	}
}
