package secrets

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func testCipher(t *testing.T) *Cipher {
	t.Helper()
	c, err := New(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	return c
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c := testCipher(t)

	ciphertext, err := c.Encrypt("super-secret-token")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if ciphertext == "super-secret-token" {
		t.Fatal("ciphertext must not equal plaintext")
	}
	if !IsEncrypted(ciphertext) {
		t.Fatalf("ciphertext missing prefix: %q", ciphertext)
	}

	plaintext, err := c.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plaintext != "super-secret-token" {
		t.Fatalf("round-trip mismatch: got %q", plaintext)
	}
}

func TestEncryptEmpty(t *testing.T) {
	c := testCipher(t)
	got, err := c.Encrypt("")
	if err != nil {
		t.Fatalf("encrypt empty: %v", err)
	}
	if got != "" {
		t.Fatalf("empty plaintext should stay empty, got %q", got)
	}
	plain, err := c.Decrypt("")
	if err != nil || plain != "" {
		t.Fatalf("decrypt empty: got %q, %v", plain, err)
	}
}

func TestDecryptLegacyPlaintextUnchanged(t *testing.T) {
	c := testCipher(t)
	got, err := c.Decrypt("legacy-plaintext")
	if err != nil {
		t.Fatalf("decrypt legacy: %v", err)
	}
	if got != "legacy-plaintext" {
		t.Fatalf("legacy plaintext should pass through, got %q", got)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	c := testCipher(t)
	ciphertext, err := c.Encrypt("secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	other, err := New(bytes.Repeat([]byte{0x43}, 32))
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	if _, err := other.Decrypt(ciphertext); err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestLoadFromEnv(t *testing.T) {
	key := "00000000000000000000000000000000000000000000000000000000000000ff"
	t.Setenv(EnvKey, key)
	c, err := Load(filepath.Join(t.TempDir(), "unused.db"))
	if err != nil {
		t.Fatalf("load from env: %v", err)
	}
	ct, err := c.Encrypt("x")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !IsEncrypted(ct) {
		t.Fatalf("expected ciphertext, got %q", ct)
	}
}

func TestLoadGeneratesKeyFileWithPermissions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "synapse.db")
	t.Setenv(EnvKey, "")
	t.Setenv(EnvKeyFile, "")
	if _, err := Load(dbPath); err != nil {
		t.Fatalf("load: %v", err)
	}
	info, err := os.Stat(dbPath + ".key")
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("key file mode = %o, want 600", mode)
	}
	// A second load must reuse the same key.
	if _, err := Load(dbPath); err != nil {
		t.Fatalf("reload: %v", err)
	}
}
