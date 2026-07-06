package kuma

import (
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
