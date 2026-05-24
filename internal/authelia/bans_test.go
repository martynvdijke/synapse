package authelia

import (
	"testing"
	"time"
)

func TestNewBanManager(t *testing.T) {
	bm := NewBanManager("/config/configuration.yml")
	if bm == nil {
		t.Fatal("expected non-nil ban manager")
	}
	if bm.BinaryPath != "authelia" {
		t.Errorf("expected default binary path 'authelia', got %q", bm.BinaryPath)
	}
	if bm.ConfigPath != "/config/configuration.yml" {
		t.Errorf("expected config path, got %q", bm.ConfigPath)
	}
}

func TestValidateBanDuration_Valid(t *testing.T) {
	valid := []string{"1h", "24h", "7d", "30m", "1m30s"}
	for _, d := range valid {
		if err := ValidateBanDuration(d); err != nil {
			t.Errorf("expected valid duration %q, got error: %v", d, err)
		}
	}
}

func TestValidateBanDuration_Empty(t *testing.T) {
	if err := ValidateBanDuration(""); err != nil {
		t.Errorf("expected empty to be valid, got: %v", err)
	}
}

func TestValidateBanDuration_Invalid(t *testing.T) {
	invalid := []string{"abc", "1x", "-1h", "forever"}
	for _, d := range invalid {
		if err := ValidateBanDuration(d); err == nil {
			t.Errorf("expected error for invalid duration %q", d)
		}
	}
}

func TestValidateBanDuration_ParsesToDuration(t *testing.T) {
	d, err := time.ParseDuration("24h")
	if err != nil {
		t.Fatalf("parse 24h: %v", err)
	}
	if d != 24*time.Hour {
		t.Errorf("expected 24h, got %v", d)
	}
}
