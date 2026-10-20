package db

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// Session is a server-side login session. Only the SHA-256 hash of the opaque
// cookie token is stored; the token itself exists only in the client cookie.
type Session struct {
	ID        string // hash of the session token
	UserID    int64
	Expiry    time.Time
	OIDC      bool
	Email     string
	Groups    []string
	CreatedAt time.Time
}

// SessionTTL is the server-side session lifetime.
const SessionTTL = 24 * time.Hour

func (db *DB) createSessionsTable() error {
	_, err := db.rawDB.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		expiry DATETIME NOT NULL,
		oidc INTEGER NOT NULL DEFAULT 0,
		email TEXT NOT NULL DEFAULT '',
		groups_json TEXT NOT NULL DEFAULT '[]',
		created_at DATETIME NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = db.rawDB.Exec(`CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expiry)`)
	return err
}

// HashSessionToken derives the storage key for an opaque session token.
func HashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// GenerateSessionToken returns a 256-bit random session token.
func GenerateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateSession persists a new session and returns its expiry.
func (db *DB) CreateSession(token string, userID int64, oidc bool, email string, groups []string, ttl time.Duration) (time.Time, error) {
	if ttl == 0 {
		ttl = SessionTTL
	}
	now := time.Now()
	expiry := now.Add(ttl)
	if groups == nil {
		groups = []string{}
	}
	gj, err := json.Marshal(groups)
	if err != nil {
		return time.Time{}, err
	}
	_, err = db.rawDB.Exec(
		`INSERT INTO sessions (id, user_id, expiry, oidc, email, groups_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		HashSessionToken(token), userID, expiry.UTC(), boolInt(oidc), email, string(gj), now.UTC(),
	)
	if err != nil {
		return time.Time{}, err
	}
	return expiry, nil
}

// GetSession loads a session by its opaque token. Expired sessions are deleted
// and reported as not found.
func (db *DB) GetSession(token string) (*Session, error) {
	if token == "" {
		return nil, nil
	}
	var s Session
	var oidc int
	var groupsJSON string
	err := db.rawDB.QueryRow(
		`SELECT id, user_id, expiry, oidc, email, groups_json, created_at FROM sessions WHERE id = ?`,
		HashSessionToken(token),
	).Scan(&s.ID, &s.UserID, &s.Expiry, &oidc, &s.Email, &groupsJSON, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(s.Expiry) {
		_ = db.DeleteSession(token)
		return nil, nil
	}
	s.OIDC = oidc != 0
	if groupsJSON != "" {
		_ = json.Unmarshal([]byte(groupsJSON), &s.Groups)
	}
	return &s, nil
}

// DeleteSession removes one session by token.
func (db *DB) DeleteSession(token string) error {
	if token == "" {
		return nil
	}
	_, err := db.rawDB.Exec(`DELETE FROM sessions WHERE id = ?`, HashSessionToken(token))
	return err
}

// CleanupSessions deletes expired rows and returns the number removed.
func (db *DB) CleanupSessions() (int64, error) {
	res, err := db.rawDB.Exec(`DELETE FROM sessions WHERE expiry < ?`, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
