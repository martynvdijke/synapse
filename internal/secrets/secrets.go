// Package secrets provides transparent AES-GCM encryption for values stored
// at rest (settings and integration credentials).
//
// Ciphertext values carry an "enc:v1:" prefix. Values without the prefix are
// treated as legacy plaintext and are returned unchanged, which lets the
// database migrate them on read without breaking existing installs.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// Prefix identifies a value encrypted by this package.
	Prefix = "enc:v1:"

	// EnvKey holds the encryption key directly (hex, base64, or raw 32 bytes).
	EnvKey = "SYNAPSE_SECRET_KEY"
	// EnvKeyFile points at a file containing the encryption key.
	EnvKeyFile = "SYNAPSE_SECRET_KEY_FILE"
)

// Cipher encrypts and decrypts short secret strings.
type Cipher struct {
	aead cipher.AEAD
}

// New builds a Cipher from a 16, 24, or 32 byte AES key.
func New(key []byte) (*Cipher, error) {
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, fmt.Errorf("secrets: invalid AES key length %d (want 16, 24 or 32)", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Load resolves the encryption key. Resolution order:
//
//  1. $SYNAPSE_SECRET_KEY (hex / base64 / raw)
//  2. $SYNAPSE_SECRET_KEY_FILE
//  3. <dbPath>.key, generated with 0600 permissions when absent
//
// The generated file is written next to the database so encryption is on by
// default and survives restarts.
func Load(dbPath string) (*Cipher, error) {
	if raw := strings.TrimSpace(os.Getenv(EnvKey)); raw != "" {
		key, err := decodeKey(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", EnvKey, err)
		}
		return New(key)
	}

	keyPath := strings.TrimSpace(os.Getenv(EnvKeyFile))
	if keyPath == "" {
		keyPath = dbPath + ".key"
	}
	key, err := readOrCreateKey(keyPath)
	if err != nil {
		return nil, err
	}
	return New(key)
}

// decodeKey accepts hex, base64 (standard or raw), or a raw 32-byte string.
func decodeKey(raw string) ([]byte, error) {
	if b, err := hex.DecodeString(raw); err == nil && (len(b) == 16 || len(b) == 24 || len(b) == 32) {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && (len(b) == 16 || len(b) == 24 || len(b) == 32) {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(raw); err == nil && (len(b) == 16 || len(b) == 24 || len(b) == 32) {
		return b, nil
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	return nil, errors.New("key must be 32 raw bytes, 64 hex chars, or base64")
}

func readOrCreateKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return decodeKey(strings.TrimSpace(string(data)))
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("secrets: read key %s: %w", path, err)
	}

	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("secrets: generate key: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("secrets: create key dir: %w", err)
		}
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, fmt.Errorf("secrets: write key %s: %w", path, err)
	}
	return key, nil
}

// Encrypt returns "" for an empty plaintext and a prefixed ciphertext
// otherwise. The nonce is prepended to the ciphertext.
func (c *Cipher) Encrypt(plain string) (string, error) {
	if plain == "" || c == nil {
		return plain, nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plain), nil)
	return Prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt returns legacy plaintext unchanged, and errors if a prefixed value
// cannot be authenticated with the configured key.
func (c *Cipher) Decrypt(value string) (string, error) {
	if value == "" || c == nil || !strings.HasPrefix(value, Prefix) {
		return value, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, Prefix))
	if err != nil {
		return "", fmt.Errorf("secrets: decode ciphertext: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("secrets: ciphertext too short")
	}
	plain, err := c.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", errors.New("secrets: decrypt failed (wrong key?)")
	}
	return string(plain), nil
}

// IsEncrypted reports whether a stored value is already ciphertext.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, Prefix)
}
