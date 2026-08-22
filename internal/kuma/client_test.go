package kuma

import (
	"fmt"
	"testing"
)

func TestClient_Login_Deprecated(t *testing.T) {
	c := NewClient("http://test:3000")
	err := c.Login("admin", "secret")
	if err != ErrRESTNotSupported {
		t.Fatalf("expected ErrRESTNotSupported, got %v", err)
	}
	if c.username != "admin" || c.password != "secret" {
		t.Errorf("Login should store credentials: got %s/%s", c.username, c.password)
	}
}

func TestClient_GetMonitors_Deprecated(t *testing.T) {
	c := NewClient("http://test:3000")
	_, err := c.GetMonitors()
	if err != ErrRESTNotSupported {
		t.Fatalf("expected ErrRESTNotSupported, got %v", err)
	}
}

func TestClient_GetDockerHosts_Deprecated(t *testing.T) {
	c := NewClient("http://test:3000")
	_, err := c.GetDockerHosts()
	if err != ErrRESTNotSupported {
		t.Fatalf("expected ErrRESTNotSupported, got %v", err)
	}
}

func TestClient_AddMonitor_Deprecated(t *testing.T) {
	c := NewClient("http://test:3000")
	_, err := c.AddMonitor("http", "test", "http://example.com", "", 0)
	if err != ErrRESTNotSupported {
		t.Fatalf("expected ErrRESTNotSupported, got %v", err)
	}
}

func TestClient_QueryMonitorsViaSocketIO_UsesFnVar(t *testing.T) {
	called := false
	queryMonitorsFn = func(url, user, pass string) ([]KumaMonitor, error) {
		called = true
		if url != "http://kuma:3000" || user != "u" || pass != "p" {
			t.Errorf("unexpected args: %s/%s/%s", url, user, pass)
		}
		return []KumaMonitor{{ID: 1, Name: "m1"}}, nil
	}
	defer func() { queryMonitorsFn = QueryMonitorsViaSocketIO }()

	c := NewClient("http://kuma:3000")
	c.username = "u"
	c.password = "p"
	monitors, err := c.QueryMonitorsViaSocketIO()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("queryMonitorsFn was not called")
	}
	if len(monitors) != 1 || monitors[0].ID != 1 {
		t.Errorf("unexpected monitors: %+v", monitors)
	}
}

func TestClient_AddMonitorViaSocketIO_UsesFnVar(t *testing.T) {
	called := false
	addMonitorFn = func(url, user, pass, mtype, name, monURL, container string, hostID int) (int, error) {
		called = true
		if url != "http://kuma:3000" || user != "u" || pass != "p" || name != "test" {
			t.Errorf("unexpected args: %s/%s/%s/%s", url, user, pass, name)
		}
		return 42, nil
	}
	defer func() { addMonitorFn = AddMonitorViaSocketIO }()

	c := NewClient("http://kuma:3000")
	c.username = "u"
	c.password = "p"
	id, err := c.AddMonitorViaSocketIO("http", "test", "http://ex.com", "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("addMonitorFn was not called")
	}
	if id != 42 {
		t.Errorf("expected id 42, got %d", id)
	}
}

func TestClientDeleteMonitorViaSocketIO(t *testing.T) {
	restore := SetDeleteMonitorTestHook(func(url, user, pass string, monitorID int) error {
		if url != "http://kuma" || user != "u" || pass != "p" || monitorID != 7 {
			return fmt.Errorf("unexpected args: %s %s %s %d", url, user, pass, monitorID)
		}
		return nil
	})
	defer restore()

	c := NewClient("http://kuma")
	c.Login("u", "p")
	if err := c.DeleteMonitorViaSocketIO(7); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestClientDeleteMonitorErrorPropagates(t *testing.T) {
	restore := SetDeleteMonitorTestHook(func(url, user, pass string, monitorID int) error {
		return fmt.Errorf("delete monitor rejected: boom")
	})
	defer restore()

	c := NewClient("http://kuma")
	c.Login("u", "p")
	err := c.DeleteMonitorViaSocketIO(1)
	if err == nil || err.Error() != "delete monitor rejected: boom" {
		t.Fatalf("expected propagated error, got %v", err)
	}
}

func TestClientEditMonitorViaSocketIO(t *testing.T) {
	var gotPayload map[string]any
	var gotID int
	restore := SetEditMonitorTestHook(func(url, user, pass string, monitorID int, payload map[string]any) error {
		gotID = monitorID
		gotPayload = payload
		return nil
	})
	defer restore()

	c := NewClient("http://kuma")
	c.Login("u", "p")
	payload := map[string]any{"name": "renamed", "type": "http", "interval": 30}
	if err := c.EditMonitorViaSocketIO(3, payload); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if gotID != 3 {
		t.Fatalf("expected id 3, got %d", gotID)
	}
	if gotPayload["name"] != "renamed" || gotPayload["interval"] != 30 {
		t.Fatalf("payload not forwarded: %+v", gotPayload)
	}
}

func TestClientEditMonitorUsesClientTestHook(t *testing.T) {
	c := NewClient("http://kuma")
	c.Login("u", "p")
	called := false
	c.SetTestHooks(&ClientTestHooks{
		EditMonitor: func(monitorID int, payload map[string]any) error {
			called = true
			return nil
		},
	})
	if err := c.EditMonitorViaSocketIO(5, map[string]any{}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if !called {
		t.Fatal("client-level hook not called")
	}
}

func TestClientPauseMonitorSuccess(t *testing.T) {
	restore := SetPauseMonitorTestHook(func(url, user, pass string, monitorID int) error {
		if monitorID != 5 {
			return fmt.Errorf("unexpected id %d", monitorID)
		}
		return nil
	})
	defer restore()
	c := NewClient("http://kuma")
	c.Login("u", "p")
	// prime cache
	restoreQ := SetQueryMonitorsTestHook(func(url, user, pass string) ([]KumaMonitor, error) {
		return []KumaMonitor{{ID: 1}}, nil
	})
	defer restoreQ()
	c.QueryMonitorsViaSocketIO()
	c.mu.Lock()
	hasCache := c.monCache != nil
	c.mu.Unlock()
	if !hasCache {
		t.Fatal("cache should be set")
	}
	if err := c.PauseMonitorViaSocketIO(5); err != nil {
		t.Fatalf("pause: %v", err)
	}
	c.mu.Lock()
	hasCache = c.monCache != nil
	c.mu.Unlock()
	if hasCache {
		t.Fatal("cache should be invalidated after pause success")
	}
}

func TestClientPauseMonitorErrorPropagates(t *testing.T) {
	restore := SetPauseMonitorTestHook(func(url, user, pass string, monitorID int) error {
		return fmt.Errorf("pause monitor rejected: boom")
	})
	defer restore()
	c := NewClient("http://kuma")
	c.Login("u", "p")
	err := c.PauseMonitorViaSocketIO(1)
	if err == nil || err.Error() != "pause monitor rejected: boom" {
		t.Fatalf("expected propagated error, got %v", err)
	}
}

func TestClientResumeMonitorSuccess(t *testing.T) {
	restore := SetResumeMonitorTestHook(func(url, user, pass string, monitorID int) error {
		return nil
	})
	defer restore()
	c := NewClient("http://kuma")
	c.Login("u", "p")
	restoreQ := SetQueryMonitorsTestHook(func(url, user, pass string) ([]KumaMonitor, error) {
		return []KumaMonitor{{ID: 1}}, nil
	})
	defer restoreQ()
	c.QueryMonitorsViaSocketIO()
	if err := c.ResumeMonitorViaSocketIO(3); err != nil {
		t.Fatalf("resume: %v", err)
	}
	c.mu.Lock()
	hasCache := c.monCache != nil
	c.mu.Unlock()
	if hasCache {
		t.Fatal("cache should be invalidated after resume")
	}
}

func TestClientResumeMonitorErrorPropagates(t *testing.T) {
	restore := SetResumeMonitorTestHook(func(url, user, pass string, monitorID int) error {
		return fmt.Errorf("resume monitor rejected: fail")
	})
	defer restore()
	c := NewClient("http://kuma")
	c.Login("u", "p")
	err := c.ResumeMonitorViaSocketIO(2)
	if err == nil || err.Error() != "resume monitor rejected: fail" {
		t.Fatalf("expected error, got %v", err)
	}
}

func TestClientAddMonitorTagSuccess(t *testing.T) {
	restore := SetAddMonitorTagTestHook(func(url, user, pass string, monitorID, tagID int) error {
		if monitorID != 10 || tagID != 3 {
			return fmt.Errorf("unexpected args %d %d", monitorID, tagID)
		}
		return nil
	})
	defer restore()
	c := NewClient("http://kuma")
	c.Login("u", "p")
	if err := c.AddMonitorTagViaSocketIO(10, 3); err != nil {
		t.Fatalf("add tag: %v", err)
	}
}

func TestClientAddMonitorTagError(t *testing.T) {
	restore := SetAddMonitorTagTestHook(func(url, user, pass string, monitorID, tagID int) error {
		return fmt.Errorf("add monitor tag rejected: oops")
	})
	defer restore()
	c := NewClient("http://kuma")
	c.Login("u", "p")
	err := c.AddMonitorTagViaSocketIO(1, 2)
	if err == nil || err.Error() != "add monitor tag rejected: oops" {
		t.Fatalf("expected error, got %v", err)
	}
}

func TestClientDeleteMonitorTagSuccess(t *testing.T) {
	restore := SetDeleteMonitorTagTestHook(func(url, user, pass string, monitorID, tagID int) error {
		return nil
	})
	defer restore()
	c := NewClient("http://kuma")
	c.Login("u", "p")
	if err := c.DeleteMonitorTagViaSocketIO(7, 2); err != nil {
		t.Fatalf("delete tag: %v", err)
	}
}

func TestClientDeleteMonitorTagError(t *testing.T) {
	restore := SetDeleteMonitorTagTestHook(func(url, user, pass string, monitorID, tagID int) error {
		return fmt.Errorf("delete monitor tag rejected: bad")
	})
	defer restore()
	c := NewClient("http://kuma")
	c.Login("u", "p")
	err := c.DeleteMonitorTagViaSocketIO(7, 2)
	if err == nil || err.Error() != "delete monitor tag rejected: bad" {
		t.Fatalf("expected error, got %v", err)
	}
}

func TestClientPauseResumeTagCacheInvalidationOnFailure(t *testing.T) {
	// failures should NOT invalidate cache
	restoreQ := SetQueryMonitorsTestHook(func(url, user, pass string) ([]KumaMonitor, error) {
		return []KumaMonitor{{ID: 1}}, nil
	})
	defer restoreQ()
	c := NewClient("http://kuma")
	c.Login("u", "p")
	c.QueryMonitorsViaSocketIO()

	restore := SetPauseMonitorTestHook(func(url, user, pass string, monitorID int) error {
		return fmt.Errorf("fail")
	})
	defer restore()
	_ = c.PauseMonitorViaSocketIO(1)
	c.mu.Lock()
	hasCache := c.monCache != nil
	c.mu.Unlock()
	if !hasCache {
		t.Fatal("cache should remain on failure")
	}
}
