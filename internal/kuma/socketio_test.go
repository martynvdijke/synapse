package kuma

import (
	"testing"
)

func TestGetMonitorStats_WSDialFailure(t *testing.T) {
	// Invalid URL should fail immediately, no 20s wait.
	c := NewClient("http://127.0.0.1:1")
	c.username = "admin"
	c.password = "secret"

	stats, err := GetMonitorStats(c, 1)
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
	if stats != nil {
		t.Errorf("expected nil stats on error, got %+v", stats)
	}
}

func TestGetMonitorStats_ShortURL(t *testing.T) {
	// Very short URL that causes dial to fail fast.
	c := NewClient("http://127.0.0.1:1")
	c.username = "admin"
	c.password = "secret"

	_, err := GetMonitorStats(c, 42)
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}
