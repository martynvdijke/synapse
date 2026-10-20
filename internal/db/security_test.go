package db

import (
	"strings"
	"testing"
	"time"

	"synapse/internal/secrets"
)

// rawSetting reads a setting directly from SQLite, bypassing decryption, so
// tests can assert what is actually stored at rest.
func rawSetting(t *testing.T, db *DB, key string) string {
	t.Helper()
	var value string
	if err := db.rawDB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value); err != nil {
		t.Fatalf("read raw setting %q: %v", key, err)
	}
	return value
}

func TestSecretSettingsEncryptedAtRest(t *testing.T) {
	db := setupTestDB(t)

	plain := "trmnl-token-value"
	if err := db.SaveSettingsMap(map[string]string{"trmnl_api_token": plain}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	stored := rawSetting(t, db, "trmnl_api_token")
	if stored == plain {
		t.Fatal("secret stored in plaintext")
	}
	if !secrets.IsEncrypted(stored) {
		t.Fatalf("stored secret missing encryption prefix: %q", stored)
	}
	if strings.Contains(stored, plain) {
		t.Fatal("ciphertext leaks plaintext")
	}

	loaded := db.GetSettings(Settings{})
	if loaded.TrmnlApiToken != plain {
		t.Fatalf("decrypted token = %q, want %q", loaded.TrmnlApiToken, plain)
	}
}

func TestAllKnownSecretSettingsEncrypted(t *testing.T) {
	db := setupTestDB(t)

	for _, settingKey := range []string{"npm_pass", "kuma_pass", "trmnl_api_token", "gotify_token", "notify_channels"} {
		if err := db.SaveSettingsMap(map[string]string{settingKey: "value-for-" + settingKey}); err != nil {
			t.Fatalf("save %s: %v", settingKey, err)
		}
		if stored := rawSetting(t, db, settingKey); !secrets.IsEncrypted(stored) {
			t.Errorf("%s not encrypted at rest: %q", settingKey, stored)
		}
	}
}

func TestLegacyPlaintextSecretMigratesOnRead(t *testing.T) {
	db := setupTestDB(t)

	// Simulate a row written by an older build (plaintext, no prefix).
	if _, err := db.rawDB.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)`, "gotify_token", "legacy-token",
	); err != nil {
		t.Fatalf("insert legacy setting: %v", err)
	}

	loaded := db.GetSettings(Settings{})
	if loaded.GotifyToken != "legacy-token" {
		t.Fatalf("legacy token = %q, want legacy-token", loaded.GotifyToken)
	}
	if stored := rawSetting(t, db, "gotify_token"); !secrets.IsEncrypted(stored) {
		t.Fatalf("legacy secret not migrated on read: %q", stored)
	}
	if stored := rawSetting(t, db, "gotify_token"); strings.Contains(stored, "legacy-token") {
		t.Fatal("migrated ciphertext leaks plaintext")
	}
}

func TestInstancePasswordsEncryptedAtRest(t *testing.T) {
	db := setupTestDB(t)

	kuma, err := db.CreateKumaInstance(&KumaInstance{Name: "k", URL: "http://kuma", Username: "u", Password: "kuma-pass"})
	if err != nil {
		t.Fatalf("create kuma: %v", err)
	}
	if kuma.Password != "kuma-pass" {
		t.Fatalf("returned kuma password = %q", kuma.Password)
	}
	var rawKuma string
	if err := db.rawDB.QueryRow(`SELECT password FROM kuma_instances WHERE id = ?`, kuma.ID).Scan(&rawKuma); err != nil {
		t.Fatalf("read raw kuma password: %v", err)
	}
	if !secrets.IsEncrypted(rawKuma) {
		t.Fatalf("kuma password not encrypted: %q", rawKuma)
	}

	npm, err := db.CreateNPMInstance(&NPMInstance{Name: "n", URL: "http://npm", Username: "u", Password: "npm-pass"})
	if err != nil {
		t.Fatalf("create npm: %v", err)
	}
	if npm.Password != "npm-pass" {
		t.Fatalf("returned npm password = %q", npm.Password)
	}
	var rawNPM string
	if err := db.rawDB.QueryRow(`SELECT password FROM npm_instances WHERE id = ?`, npm.ID).Scan(&rawNPM); err != nil {
		t.Fatalf("read raw npm password: %v", err)
	}
	if !secrets.IsEncrypted(rawNPM) {
		t.Fatalf("npm password not encrypted: %q", rawNPM)
	}
}

func TestLegacyInstancePasswordMigratedOnOpen(t *testing.T) {
	path := t.TempDir() + "/legacy.db"

	db1, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db1.rawDB.Exec(
		`INSERT INTO kuma_instances (name, url, username, password, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"legacy", "http://kuma", "u", "legacy-kuma-pass", true, time.Now().UTC(),
	); err != nil {
		t.Fatalf("insert legacy instance: %v", err)
	}
	if _, err := db1.rawDB.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)`, "gotify_token", "legacy-gotify-token",
	); err != nil {
		t.Fatalf("insert legacy setting: %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopening runs the startup migration.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { db2.Close() })

	var rawKuma string
	if err := db2.rawDB.QueryRow(`SELECT password FROM kuma_instances LIMIT 1`).Scan(&rawKuma); err != nil {
		t.Fatalf("read raw kuma password: %v", err)
	}
	if !secrets.IsEncrypted(rawKuma) {
		t.Fatalf("legacy kuma password not migrated: %q", rawKuma)
	}
	if stored := rawSetting(t, db2, "gotify_token"); !secrets.IsEncrypted(stored) {
		t.Fatalf("legacy setting not migrated: %q", stored)
	}

	instances, err := db2.GetKumaInstances()
	if err != nil {
		t.Fatalf("get instances: %v", err)
	}
	if len(instances) != 1 || instances[0].Password != "legacy-kuma-pass" {
		t.Fatalf("migrated password did not decrypt: %+v", instances)
	}
	if got := db2.GetSettings(Settings{}).GotifyToken; got != "legacy-gotify-token" {
		t.Fatalf("migrated setting did not decrypt: %q", got)
	}
}

func TestSessionPersistenceAcrossReopen(t *testing.T) {
	path := t.TempDir() + "/sessions.db"

	db1, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	token, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if _, err := db1.CreateSession(token, 42, true, "user@example.com", []string{"admins"}, time.Hour); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { db2.Close() })

	s, err := db2.GetSession(token)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if s == nil {
		t.Fatal("session missing after reopen")
	}
	if s.UserID != 42 || !s.OIDC || s.Email != "user@example.com" || len(s.Groups) != 1 || s.Groups[0] != "admins" {
		t.Fatalf("session mismatch after reopen: %+v", s)
	}

	// The token itself is never stored, only its hash.
	var storedID string
	if err := db2.rawDB.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&storedID); err != nil {
		t.Fatalf("read stored session id: %v", err)
	}
	if storedID == token {
		t.Fatal("raw session token stored in database")
	}
	if storedID != HashSessionToken(token) {
		t.Fatalf("stored id = %q, want token hash", storedID)
	}
}

func TestExpiredSessionRejectedAndDeleted(t *testing.T) {
	db := setupTestDB(t)
	token, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if _, err := db.CreateSession(token, 1, false, "", nil, -time.Second); err != nil {
		t.Fatalf("create session: %v", err)
	}

	s, err := db.GetSession(token)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if s != nil {
		t.Fatal("expired session should not be returned")
	}

	n, err := db.CleanupSessions()
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	var count int
	if err := db.rawDB.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected expired sessions removed, found %d (cleanup removed %d)", count, n)
	}
}
