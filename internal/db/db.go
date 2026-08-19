package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strconv"
	"time"

	"synapse/ent"
	"synapse/ent/migrate"
	"synapse/ent/kumainstance"
	"synapse/ent/monitor"
	"synapse/ent/npminstance"
	"synapse/ent/autheliainstance"
	"synapse/ent/settings"
	"synapse/ent/syncrun"
	"synapse/ent/servicelink"
	"synapse/ent/dockerevent"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
)

type SyncRun struct {
	ID            int64      `json:"id"`
	Source        string     `json:"source"`
	Status        string     `json:"status"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	TotalServices int        `json:"total_services"`
	Added         int        `json:"added"`
	Updated       int        `json:"updated"`
	Skipped       int        `json:"skipped"`
	Failed        int        `json:"failed"`
	DryRun        bool       `json:"dry_run"`
	ErrorMessage  string     `json:"error_message,omitempty"`
}

type Monitor struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	ServiceName     string    `json:"service_name"`
	MonitorType     string    `json:"monitor_type"`
	URL             string    `json:"url,omitempty"`
	DockerContainer string    `json:"docker_container,omitempty"`
	KumaID          int       `json:"kuma_id"`
	KumaInstanceID  int       `json:"kuma_instance_id"`
	CreatedAt       time.Time `json:"created_at"`
}

// ServiceLink ties a compose service name to an NPM proxy host and a Kuma
// monitor. npm_instance_id / kuma_instance_id of 0 mean "not linked" on that
// side. npm_details / kuma_details are JSON snapshots of the integration's
// cached configuration, refreshed via the link refresh endpoint.
type ServiceLink struct {
	ID              int64      `json:"id"`
	ServiceName     string     `json:"service_name"`
	NPMInstanceID   int        `json:"npm_instance_id"`
	NPMHostName     string     `json:"npm_host_name,omitempty"`
	NPMDetails      string     `json:"npm_details,omitempty"`
	KumaInstanceID  int        `json:"kuma_instance_id"`
	KumaMonitorID   int        `json:"kuma_monitor_id"`
	KumaMonitorName string     `json:"kuma_monitor_name,omitempty"`
	KumaDetails     string     `json:"kuma_details,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

// DockerEvent is a persisted event observed from the Docker Engine events
// stream. image_old/image_new are set for synthesized "image updated" events.
type DockerEvent struct {
	ID         int64     `json:"id"`
	EventType  string    `json:"event_type"`
	Action     string    `json:"action"`
	ActorID    string    `json:"actor_id,omitempty"`
	ActorName  string    `json:"actor_name,omitempty"`
	Image      string    `json:"image,omitempty"`
	Status     string    `json:"status,omitempty"`
	ImageOld   string    `json:"image_old,omitempty"`
	ImageNew   string    `json:"image_new,omitempty"`
	Payload    string    `json:"payload,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// DockerEventFilter restricts a docker event listing query.
type DockerEventFilter struct {
	EventType string
	Action    string
	Container string
	Since     *time.Time
	Limit     int
}

// KumaInstance holds the connection details for a single Uptime Kuma
// instance. Multiple instances may be configured; syncs fan out to all
// enabled instances.
type KumaInstance struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Username  string    `json:"username"`
	Password  string    `json:"password,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// NPMInstance holds the connection details for a single Nginx Proxy Manager
// instance. Multiple instances may be configured; syncs fan out to all
// enabled instances.
type NPMInstance struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Username  string    `json:"username"`
	Password  string    `json:"password,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type AdminUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Password string `json:"-"`
}

// APIToken is a bearer credential used to authorize mutation requests.
// Only the SHA-256 hash of the secret is persisted; the plaintext secret is
// shown exactly once at creation/rotation time and never stored or logged.
type APIToken struct {
	ID         int64      `json:"id"`
	OwnerID    int64      `json:"owner_id"`
	Name       string     `json:"name"`
	Hash       string     `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type AutheliaInstance struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	ConfigPath     string    `json:"config_path"`
	DBPath         string    `json:"db_path,omitempty"`
	DefaultPolicy  string    `json:"default_policy"`
	Overrides      string    `json:"overrides,omitempty"`
	AutoSync       bool      `json:"auto_sync"`
	NPMInstanceIDs string    `json:"npm_instance_ids,omitempty"` // JSON array, empty=all NPMs
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
}

type AutheliaAlert struct {
	ID                int64     `json:"id"`
	CNAME             string    `json:"cname"`
	Message           string    `json:"message"`
	Severity          string    `json:"severity"`
	Status            string    `json:"status"`
	AutheliaInstanceID int64    `json:"authelia_instance_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

type TempAccess struct {
	ID                int64     `json:"id"`
	IP                string    `json:"ip"`
	Reason            string    `json:"reason"`
	AutheliaInstanceID int64    `json:"authelia_instance_id,omitempty"`
	ExpiresAt         time.Time `json:"expires_at"`
	CreatedAt         time.Time `json:"created_at"`
	Status            string    `json:"status"`
}

type Settings struct {
	ComposePath           string `json:"compose_path"`
	NPMHost               string `json:"npm_host"`
	NPMUser               string `json:"npm_user"`
	NPMPass               string `json:"npm_pass"`
	KumaURL               string `json:"kuma_url"`
	KumaUser              string `json:"kuma_user"`
	KumaPass              string `json:"kuma_pass"`
	AutheliaConfigPath    string `json:"authelia_config_path"`
	AutheliaDBPath        string `json:"authelia_db_path"`
	AutheliaSyncEnabled   bool   `json:"authelia_sync_enabled"`
	AutheliaDefaultPolicy string `json:"authelia_default_policy"`
	AutheliaSyncOverrides string `json:"authelia_sync_overrides"`
	OTelEndpoint          string `json:"otel_endpoint"`
	OTelEnabled           bool   `json:"otel_enabled"`
	EinkEnabled           bool   `json:"eink_enabled"`
	TrmnlApiToken         string `json:"trmnl_api_token"`
	NotifyEnabled         bool   `json:"notify_enabled"`
	NotifyIntervalMinutes int    `json:"notify_interval_minutes"`
	GotifyURL             string `json:"gotify_url"`
	GotifyToken           string `json:"gotify_token"`
	GotifyPriority        int    `json:"gotify_priority"`
	DockerSocket          string `json:"docker_socket"`
	DockerEventsEnabled   bool   `json:"docker_events_enabled"`
	DockerEventsRetentionDays int `json:"docker_events_retention_days"`
	ReconcileEnabled      bool   `json:"reconcile_enabled"`
	ReconcileIntervalMinutes int `json:"reconcile_interval_minutes"`
	ReconcileDryRunDefault bool  `json:"reconcile_dry_run_default"`
	NotifyDockerDie       bool   `json:"notify_docker_die"`
	NotifyDockerHealth    bool   `json:"notify_docker_health"`
	NotifyDockerImage     bool   `json:"notify_docker_image"`
	NotifyReconcile       bool   `json:"notify_reconcile"`
	NotifyCooldownMinutes int    `json:"notify_cooldown_minutes"`
}

type DB struct {
	client *ent.Client
	rawDB  *sql.DB
}

func Open(path string) (*DB, error) {
	drv, err := entsql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_fk=1")
	if err != nil {
		return nil, err
	}
	sqlDB := drv.DB()
	sqlDB.SetMaxOpenConns(1)

	client := ent.NewClient(ent.Driver(drv))
	if err := client.Schema.Create(context.Background(), migrate.WithForeignKeys(false)); err != nil {
		return nil, err
	}

	db := &DB{client: client, rawDB: sqlDB}
	if err := db.createAdminUsersTable(); err != nil {
		return nil, err
	}
	if err := db.createAutheliaTables(); err != nil {
		return nil, err
	}
	if err := db.createAPITokensTable(); err != nil {
		return nil, err
	}
	return db, nil
}

func (db *DB) createAdminUsersTable() error {
	_, err := db.rawDB.Exec(`CREATE TABLE IF NOT EXISTS admin_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password TEXT NOT NULL
	)`)
	return err
}

func (db *DB) createAutheliaTables() error {
	_, err := db.rawDB.Exec(`CREATE TABLE IF NOT EXISTS authelia_alerts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		cname TEXT NOT NULL,
		message TEXT NOT NULL DEFAULT '',
		severity TEXT NOT NULL DEFAULT 'warning',
		status TEXT NOT NULL DEFAULT 'open',
		authelia_instance_id INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL
	)`)
	if err != nil {
		return err
	}

	_, err = db.rawDB.Exec(`CREATE TABLE IF NOT EXISTS temp_access (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ip TEXT NOT NULL,
		reason TEXT NOT NULL DEFAULT '',
		authelia_instance_id INTEGER DEFAULT 0,
		expires_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL,
		status TEXT NOT NULL DEFAULT 'active'
	)`)
	return err
}

// createAPITokensTable creates the bearer-token store. The migration is
// reversible: dropping this table (and restoring the legacy trmnl_api_token
// setting) restores the pre-change behavior, since no other table references it.
func (db *DB) createAPITokensTable() error {
	_, err := db.rawDB.Exec(`CREATE TABLE IF NOT EXISTS api_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		owner_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		hash TEXT NOT NULL UNIQUE,
		created_at DATETIME NOT NULL,
		expires_at DATETIME,
		revoked_at DATETIME,
		last_used_at DATETIME
	)`)
	if err != nil {
		return err
	}
	if _, err := db.rawDB.Exec(`CREATE INDEX IF NOT EXISTS idx_api_tokens_owner ON api_tokens(owner_id)`); err != nil {
		return err
	}
	_, err = db.rawDB.Exec(`CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(hash)`)
	return err
}

func (db *DB) CreateAdminUser(username, passwordHash string) (int64, error) {
	res, err := db.rawDB.Exec("INSERT INTO admin_users (username, password) VALUES (?, ?)", username, passwordHash)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) GetAdminUser(username string) (*AdminUser, error) {
	var u AdminUser
	err := db.rawDB.QueryRow("SELECT id, username, password FROM admin_users WHERE username = ?", username).Scan(&u.ID, &u.Username, &u.Password)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (db *DB) CountAdminUsers() (int, error) {
	var count int
	err := db.rawDB.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// GetFirstAdminUser returns the admin user with the lowest id (the original
// setup user). Used as the default owner for migrated tokens.
func (db *DB) GetFirstAdminUser() (*AdminUser, error) {
	var u AdminUser
	err := db.rawDB.QueryRow("SELECT id, username, password FROM admin_users ORDER BY id LIMIT 1").Scan(&u.ID, &u.Username, &u.Password)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// --- API token store ---

// HashToken returns the hex-encoded SHA-256 digest of a token secret. This is
// the only representation of a token that is ever persisted or compared.
func HashToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func scanAPIToken(row *sql.Row) (*APIToken, error) {
	var t APIToken
	var expiresAt, revokedAt, lastUsedAt sql.NullString
	err := row.Scan(&t.ID, &t.OwnerID, &t.Name, &t.Hash, &t.CreatedAt, &expiresAt, &revokedAt, &lastUsedAt)
	if err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		if ts, err := time.Parse(time.RFC3339, expiresAt.String); err == nil {
			t.ExpiresAt = &ts
		}
	}
	if revokedAt.Valid {
		if ts, err := time.Parse(time.RFC3339, revokedAt.String); err == nil {
			t.RevokedAt = &ts
		}
	}
	if lastUsedAt.Valid {
		if ts, err := time.Parse(time.RFC3339, lastUsedAt.String); err == nil {
			t.LastUsedAt = &ts
		}
	}
	return &t, nil
}

func (db *DB) CreateAPIToken(ownerID int64, name, hash string, expiresAt *time.Time) (int64, error) {
	var exp interface{}
	if expiresAt != nil {
		exp = expiresAt.Format(time.RFC3339)
	}
	res, err := db.rawDB.Exec(
		"INSERT INTO api_tokens (owner_id, name, hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?)",
		ownerID, name, hash, time.Now().Format(time.RFC3339), exp,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) GetAPITokenByHash(hash string) (*APIToken, error) {
	row := db.rawDB.QueryRow(
		"SELECT id, owner_id, name, hash, created_at, expires_at, revoked_at, last_used_at FROM api_tokens WHERE hash = ?",
		hash,
	)
	t, err := scanAPIToken(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (db *DB) GetAPITokenByID(id int64) (*APIToken, error) {
	row := db.rawDB.QueryRow(
		"SELECT id, owner_id, name, hash, created_at, expires_at, revoked_at, last_used_at FROM api_tokens WHERE id = ?",
		id,
	)
	t, err := scanAPIToken(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (db *DB) ListAPITokens(ownerID int64) ([]APIToken, error) {
	rows, err := db.rawDB.Query(
		"SELECT id, owner_id, name, hash, created_at, expires_at, revoked_at, last_used_at FROM api_tokens WHERE owner_id = ? ORDER BY created_at DESC",
		ownerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []APIToken
	for rows.Next() {
		var t APIToken
		var expiresAt, revokedAt, lastUsedAt sql.NullString
		if err := rows.Scan(&t.ID, &t.OwnerID, &t.Name, &t.Hash, &t.CreatedAt, &expiresAt, &revokedAt, &lastUsedAt); err != nil {
			return nil, err
		}
		if expiresAt.Valid {
			if ts, err := time.Parse(time.RFC3339, expiresAt.String); err == nil {
				t.ExpiresAt = &ts
			}
		}
		if revokedAt.Valid {
			if ts, err := time.Parse(time.RFC3339, revokedAt.String); err == nil {
				t.RevokedAt = &ts
			}
		}
		if lastUsedAt.Valid {
			if ts, err := time.Parse(time.RFC3339, lastUsedAt.String); err == nil {
				t.LastUsedAt = &ts
			}
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (db *DB) RevokeAPIToken(id int64) error {
	_, err := db.rawDB.Exec("UPDATE api_tokens SET revoked_at = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)
	return err
}

func (db *DB) RotateAPIToken(id int64, newHash string) error {
	_, err := db.rawDB.Exec(
		"UPDATE api_tokens SET hash = ?, revoked_at = NULL, last_used_at = NULL WHERE id = ?",
		newHash, id,
	)
	return err
}

func (db *DB) TouchAPIToken(id int64) error {
	_, err := db.rawDB.Exec("UPDATE api_tokens SET last_used_at = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)
	return err
}

func (db *DB) DeleteAPIToken(id int64) error {
	_, err := db.rawDB.Exec("DELETE FROM api_tokens WHERE id = ?", id)
	return err
}

// DeleteSetting removes a settings key. Used by the TRMNL token migration to
// drop the obsolete placeholder credential after provisioning a real token.
func (db *DB) DeleteSetting(key string) error {
	_, err := db.client.Settings.Delete().Where(settings.KeyEQ(key)).Exec(context.Background())
	return err
}

// MigrateTrmnlToken provisions an API token from the legacy trmnl_api_token
// setting (owner = first admin user) and removes the obsolete setting key.
// Idempotent: if a token with the same hash already exists, only the setting is
// removed. No-op when the setting is empty or no admin user exists yet.
func (db *DB) MigrateTrmnlToken() error {
	s := db.GetSettings(Settings{})
	if s.TrmnlApiToken == "" {
		return nil
	}
	owner, err := db.GetFirstAdminUser()
	if err != nil {
		if err == sql.ErrNoRows {
			return nil // fresh install, nothing to migrate
		}
		return err
	}
	hash := HashToken(s.TrmnlApiToken)
	existing, err := db.GetAPITokenByHash(hash)
	if err != nil {
		return err
	}
	if existing == nil {
		if _, err := db.CreateAPIToken(owner.ID, "TRMNL (migrated)", hash, nil); err != nil {
			return err
		}
	}
	return db.DeleteSetting("trmnl_api_token")
}

func (db *DB) Close() error {
	return db.client.Close()
}

func toSyncRun(e *ent.SyncRun) SyncRun {
	r := SyncRun{
		ID:            int64(e.ID),
		Source:        e.Source,
		Status:        e.Status,
		StartedAt:     e.StartedAt,
		TotalServices: e.TotalServices,
		Added:         e.Added,
		Updated:       e.Updated,
		Skipped:       e.Skipped,
		Failed:        e.Failed,
		DryRun:        e.DryRun,
		ErrorMessage:  e.ErrorMessage,
	}
	if e.FinishedAt != nil {
		r.FinishedAt = e.FinishedAt
	}
	return r
}

func toMonitor(e *ent.Monitor) Monitor {
	return Monitor{
		ID:              int64(e.ID),
		Name:            e.Name,
		ServiceName:     e.ServiceName,
		MonitorType:     e.MonitorType,
		URL:             e.URL,
		DockerContainer: e.DockerContainer,
		KumaID:          e.KumaID,
		KumaInstanceID:  e.KumaInstanceID,
		CreatedAt:       e.CreatedAt,
	}
}

func toKumaInstance(e *ent.KumaInstance) KumaInstance {
	return KumaInstance{
		ID:        int64(e.ID),
		Name:      e.Name,
		URL:       e.URL,
		Username:  e.Username,
		Password:  e.Password,
		Enabled:   e.Enabled,
		CreatedAt: e.CreatedAt,
	}
}

func toNPMInstance(e *ent.NPMInstance) NPMInstance {
	return NPMInstance{
		ID:        int64(e.ID),
		Name:      e.Name,
		URL:       e.URL,
		Username:  e.Username,
		Password:  e.Password,
		Enabled:   e.Enabled,
		CreatedAt: e.CreatedAt,
	}
}

func toAutheliaInstance(e *ent.AutheliaInstance) AutheliaInstance {
	return AutheliaInstance{
		ID:             int64(e.ID),
		Name:           e.Name,
		ConfigPath:     e.ConfigPath,
		DBPath:         e.DbPath,
		DefaultPolicy:  e.DefaultPolicy,
		Overrides:      e.Overrides,
		AutoSync:       e.AutoSync,
		NPMInstanceIDs: e.NpmInstanceIds,
		Enabled:        e.Enabled,
		CreatedAt:      e.CreatedAt,
	}
}

func (db *DB) CreateSyncRun(run *SyncRun) (int64, error) {
	q := db.client.SyncRun.Create().
		SetSource(run.Source).
		SetStatus(run.Status).
		SetStartedAt(run.StartedAt).
		SetTotalServices(run.TotalServices).
		SetAdded(run.Added).
		SetUpdated(run.Updated).
		SetSkipped(run.Skipped).
		SetFailed(run.Failed).
		SetDryRun(run.DryRun)
	if run.ErrorMessage != "" {
		q.SetErrorMessage(run.ErrorMessage)
	}
	e, err := q.Save(context.Background())
	if err != nil {
		return 0, err
	}
	return int64(e.ID), nil
}

func (db *DB) FinishSyncRun(id int64, status string, added, skipped, failed int, errMsg string) error {
	_, err := db.client.SyncRun.UpdateOneID(int(id)).
		SetStatus(status).
		SetFinishedAt(time.Now()).
		SetAdded(added).
		SetSkipped(skipped).
		SetFailed(failed).
		SetErrorMessage(errMsg).
		Save(context.Background())
	return err
}

// FinishReconcileRun completes a reconcile run, including the updated counter
// that only reconcile produces.
func (db *DB) FinishReconcileRun(id int64, status string, added, updated, skipped, failed int, errMsg string) error {
	_, err := db.client.SyncRun.UpdateOneID(int(id)).
		SetStatus(status).
		SetFinishedAt(time.Now()).
		SetAdded(added).
		SetUpdated(updated).
		SetSkipped(skipped).
		SetFailed(failed).
		SetErrorMessage(errMsg).
		Save(context.Background())
	return err
}

func (db *DB) GetSyncRuns(limit int) ([]SyncRun, error) {
	entries, err := db.client.SyncRun.Query().
		Order(syncrun.ByID(entsql.OrderDesc())).
		Limit(limit).
		All(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]SyncRun, len(entries))
	for i, e := range entries {
		result[i] = toSyncRun(e)
	}
	return result, nil
}

func (db *DB) GetLatestSyncRun(source string) (*SyncRun, error) {
	q := db.client.SyncRun.Query().Order(syncrun.ByID(entsql.OrderDesc())).Limit(1)
	if source != "" {
		q = q.Where(syncrun.Source(source))
	}
	e, err := q.Only(context.Background())
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	r := toSyncRun(e)
	return &r, nil
}

func (db *DB) AddMonitor(m *Monitor) error {
	_, err := db.client.Monitor.Create().
		SetName(m.Name).
		SetServiceName(m.ServiceName).
		SetMonitorType(m.MonitorType).
		SetURL(m.URL).
		SetDockerContainer(m.DockerContainer).
		SetKumaID(m.KumaID).
		SetKumaInstanceID(m.KumaInstanceID).
		SetCreatedAt(m.CreatedAt).
		Save(context.Background())
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil
		}
		return err
	}
	return nil
}

func (db *DB) GetMonitors() ([]Monitor, error) {
	entries, err := db.client.Monitor.Query().
		Order(monitor.ByID(entsql.OrderDesc())).
		All(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]Monitor, len(entries))
	for i, e := range entries {
		result[i] = toMonitor(e)
	}
	return result, nil
}

func (db *DB) GetMonitorCount() (int, error) {
	return db.client.Monitor.Query().Count(context.Background())
}

// --- ServiceLink CRUD ---

func toServiceLink(e *ent.ServiceLink) ServiceLink {
	return ServiceLink{
		ID:              int64(e.ID),
		ServiceName:     e.ServiceName,
		NPMInstanceID:   e.NpmInstanceID,
		NPMHostName:     e.NpmHostName,
		NPMDetails:      e.NpmDetails,
		KumaInstanceID:  e.KumaInstanceID,
		KumaMonitorID:   e.KumaMonitorID,
		KumaMonitorName: e.KumaMonitorName,
		KumaDetails:     e.KumaDetails,
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}
}

// CreateServiceLink persists a new service link.
func (db *DB) CreateServiceLink(l *ServiceLink) (*ServiceLink, error) {
	now := time.Now()
	q := db.client.ServiceLink.Create().
		SetServiceName(l.ServiceName).
		SetNpmInstanceID(l.NPMInstanceID).
		SetNpmHostName(l.NPMHostName).
		SetNpmDetails(l.NPMDetails).
		SetKumaInstanceID(l.KumaInstanceID).
		SetKumaMonitorID(l.KumaMonitorID).
		SetKumaMonitorName(l.KumaMonitorName).
		SetKumaDetails(l.KumaDetails).
		SetCreatedAt(now)
	if l.CreatedAt.IsZero() {
		q.SetCreatedAt(now)
	} else {
		q.SetCreatedAt(l.CreatedAt)
	}
	e, err := q.Save(context.Background())
	if err != nil {
		return nil, err
	}
	r := toServiceLink(e)
	return &r, nil
}

// GetServiceLinks returns all service links ordered by id.
func (db *DB) GetServiceLinks() ([]ServiceLink, error) {
	entries, err := db.client.ServiceLink.Query().
		Order(servicelink.ByID(entsql.OrderAsc())).
		All(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]ServiceLink, len(entries))
	for i, e := range entries {
		result[i] = toServiceLink(e)
	}
	return result, nil
}

// GetServiceLink returns a single service link by id.
func (db *DB) GetServiceLink(id int64) (*ServiceLink, error) {
	e, err := db.client.ServiceLink.Query().
		Where(servicelink.IDEQ(int(id))).
		Only(context.Background())
	if err != nil {
		return nil, err
	}
	r := toServiceLink(e)
	return &r, nil
}

// GetServiceLinkByService returns the link for a compose service name, if any.
func (db *DB) GetServiceLinkByService(name string) (*ServiceLink, error) {
	e, err := db.client.ServiceLink.Query().
		Where(servicelink.ServiceNameEQ(name)).
		Only(context.Background())
	if err != nil {
		return nil, err
	}
	r := toServiceLink(e)
	return &r, nil
}

// UpdateServiceLink updates the mutable fields of a service link.
func (db *DB) UpdateServiceLink(l *ServiceLink) error {
	now := time.Now()
	_, err := db.client.ServiceLink.Update().
		Where(servicelink.IDEQ(int(l.ID))).
		SetNpmInstanceID(l.NPMInstanceID).
		SetNpmHostName(l.NPMHostName).
		SetNpmDetails(l.NPMDetails).
		SetKumaInstanceID(l.KumaInstanceID).
		SetKumaMonitorID(l.KumaMonitorID).
		SetKumaMonitorName(l.KumaMonitorName).
		SetKumaDetails(l.KumaDetails).
		SetNillableUpdatedAt(&now).
		Save(context.Background())
	return err
}

// DeleteServiceLink removes a service link by id.
func (db *DB) DeleteServiceLink(id int64) error {
	_, err := db.client.ServiceLink.Delete().
		Where(servicelink.IDEQ(int(id))).
		Exec(context.Background())
	return err
}

// UpsertServiceLink inserts a link or updates the existing one with the same
// service name. Returns the resulting link.
func (db *DB) UpsertServiceLink(l *ServiceLink) (*ServiceLink, error) {
	existing, err := db.GetServiceLinkByService(l.ServiceName)
	if err != nil {
		if ent.IsNotFound(err) {
			return db.CreateServiceLink(l)
		}
		return nil, err
	}
	l.ID = existing.ID
	if err := db.UpdateServiceLink(l); err != nil {
		return nil, err
	}
	return db.GetServiceLink(l.ID)
}

// --- DockerEvent CRUD ---

func toDockerEvent(e *ent.DockerEvent) DockerEvent {
	return DockerEvent{
		ID:        int64(e.ID),
		EventType: e.EventType,
		Action:    e.Action,
		ActorID:   e.ActorID,
		ActorName: e.ActorName,
		Image:     e.Image,
		Status:    e.Status,
		ImageOld:  e.ImageOld,
		ImageNew:  e.ImageNew,
		Payload:   e.Payload,
		CreatedAt: e.CreatedAt,
	}
}

// CreateDockerEvent persists a docker event.
func (db *DB) CreateDockerEvent(ev *DockerEvent) error {
	_, err := db.client.DockerEvent.Create().
		SetEventType(ev.EventType).
		SetAction(ev.Action).
		SetActorID(ev.ActorID).
		SetActorName(ev.ActorName).
		SetImage(ev.Image).
		SetStatus(ev.Status).
		SetImageOld(ev.ImageOld).
		SetImageNew(ev.ImageNew).
		SetPayload(ev.Payload).
		SetCreatedAt(ev.CreatedAt).
		Save(context.Background())
	return err
}

// ListDockerEvents returns docker events matching the filter, newest first.
func (db *DB) ListDockerEvents(f DockerEventFilter) ([]DockerEvent, error) {
	q := db.client.DockerEvent.Query()
	if f.EventType != "" {
		q = q.Where(dockerevent.EventTypeEQ(f.EventType))
	}
	if f.Action != "" {
		q = q.Where(dockerevent.ActionEQ(f.Action))
	}
	if f.Container != "" {
		q = q.Where(
			dockerevent.Or(
				dockerevent.ActorNameContains(f.Container),
				dockerevent.ActorIDContains(f.Container),
			),
		)
	}
	if f.Since != nil {
		q = q.Where(dockerevent.CreatedAtGTE(*f.Since))
	}
	q = q.Order(dockerevent.ByCreatedAt(entsql.OrderDesc()))
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	entries, err := q.All(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]DockerEvent, len(entries))
	for i, e := range entries {
		result[i] = toDockerEvent(e)
	}
	return result, nil
}

// PurgeDockerEvents deletes all docker events older than the given time.
func (db *DB) PurgeDockerEvents(before time.Time) (int, error) {
	n, err := db.client.DockerEvent.Delete().
		Where(dockerevent.CreatedAtLT(before)).
		Exec(context.Background())
	return n, err
}

// --- KumaInstance CRUD ---

// CreateKumaInstance inserts a new Kuma instance row and returns it.
func (db *DB) CreateKumaInstance(k *KumaInstance) (*KumaInstance, error) {
	e, err := db.client.KumaInstance.Create().
		SetName(k.Name).
		SetURL(k.URL).
		SetUsername(k.Username).
		SetPassword(k.Password).
		SetEnabled(k.Enabled).
		SetCreatedAt(time.Now()).
		Save(context.Background())
	if err != nil {
		return nil, err
	}
	r := toKumaInstance(e)
	return &r, nil
}

// GetKumaInstances returns all configured Kuma instances ordered by id.
func (db *DB) GetKumaInstances() ([]KumaInstance, error) {
	entries, err := db.client.KumaInstance.Query().
		Order(kumainstance.ByID(entsql.OrderAsc())).
		All(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]KumaInstance, len(entries))
	for i, e := range entries {
		result[i] = toKumaInstance(e)
	}
	return result, nil
}

// GetKumaInstance returns a single instance by id.
func (db *DB) GetKumaInstance(id int64) (*KumaInstance, error) {
	e, err := db.client.KumaInstance.Query().
		Where(kumainstance.IDEQ(int(id))).
		Only(context.Background())
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	r := toKumaInstance(e)
	return &r, nil
}

// GetEnabledKumaInstances returns all enabled instances ordered by id.
func (db *DB) GetEnabledKumaInstances() ([]KumaInstance, error) {
	entries, err := db.client.KumaInstance.Query().
		Where(kumainstance.Enabled(true)).
		Order(kumainstance.ByID(entsql.OrderAsc())).
		All(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]KumaInstance, len(entries))
	for i, e := range entries {
		result[i] = toKumaInstance(e)
	}
	return result, nil
}

// UpdateKumaInstance updates an existing instance. If password is empty the
// existing password is preserved.
func (db *DB) UpdateKumaInstance(id int64, k *KumaInstance) error {
	q := db.client.KumaInstance.UpdateOneID(int(id)).
		SetName(k.Name).
		SetURL(k.URL).
		SetUsername(k.Username).
		SetEnabled(k.Enabled)
	if k.Password != "" {
		q.SetPassword(k.Password)
	}
	_, err := q.Save(context.Background())
	return err
}

// DeleteKumaInstance removes an instance and cascades its monitors.
func (db *DB) DeleteKumaInstance(id int64) error {
	// Delete monitors belonging to this instance first.
	_, err := db.client.Monitor.Delete().
		Where(monitor.KumaInstanceIDEQ(int(id))).
		Exec(context.Background())
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	return db.client.KumaInstance.DeleteOneID(int(id)).Exec(context.Background())
}

// MigrateKumaInstances migrates the legacy single-instance kuma_* settings
// into KumaInstance rows. It is idempotent: if instances already exist it
// does nothing. When instances are created, existing Monitor rows are
// backfilled with the default instance's id.
//
// The legacy settings are read from the provided Settings value (which the
// caller populates from the Settings KV table and/or env-var defaults).
func (db *DB) MigrateKumaInstances(legacy Settings) error {
	existing, err := db.GetKumaInstances()
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	if legacy.KumaURL == "" {
		// No legacy config to migrate; leave empty. The user can add
		// instances via the UI.
		return nil
	}
	inst, err := db.CreateKumaInstance(&KumaInstance{
		Name:     "default",
		URL:      legacy.KumaURL,
		Username: legacy.KumaUser,
		Password: legacy.KumaPass,
		Enabled:  true,
	})
	if err != nil {
		return err
	}
	// Backfill existing monitors (created before multi-instance) to the
	// default instance.
	_, err = db.client.Monitor.Update().
		Where(monitor.KumaInstanceIDEQ(0)).
		SetKumaInstanceID(int(inst.ID)).
		Save(context.Background())
	return err
}

// --- NPMInstance CRUD ---

// CreateNPMInstance inserts a new NPM instance row and returns it.
func (db *DB) CreateNPMInstance(n *NPMInstance) (*NPMInstance, error) {
	e, err := db.client.NPMInstance.Create().
		SetName(n.Name).
		SetURL(n.URL).
		SetUsername(n.Username).
		SetPassword(n.Password).
		SetEnabled(n.Enabled).
		SetCreatedAt(time.Now()).
		Save(context.Background())
	if err != nil {
		return nil, err
	}
	r := toNPMInstance(e)
	return &r, nil
}

// GetNPMInstances returns all configured NPM instances ordered by id.
func (db *DB) GetNPMInstances() ([]NPMInstance, error) {
	entries, err := db.client.NPMInstance.Query().
		Order(npminstance.ByID(entsql.OrderAsc())).
		All(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]NPMInstance, len(entries))
	for i, e := range entries {
		result[i] = toNPMInstance(e)
	}
	return result, nil
}

// GetNPMInstance returns a single instance by id.
func (db *DB) GetNPMInstance(id int64) (*NPMInstance, error) {
	e, err := db.client.NPMInstance.Query().
		Where(npminstance.IDEQ(int(id))).
		Only(context.Background())
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	r := toNPMInstance(e)
	return &r, nil
}

// GetEnabledNPMInstances returns all enabled instances ordered by id.
func (db *DB) GetEnabledNPMInstances() ([]NPMInstance, error) {
	entries, err := db.client.NPMInstance.Query().
		Where(npminstance.Enabled(true)).
		Order(npminstance.ByID(entsql.OrderAsc())).
		All(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]NPMInstance, len(entries))
	for i, e := range entries {
		result[i] = toNPMInstance(e)
	}
	return result, nil
}

// UpdateNPMInstance updates an existing instance. If password is empty the
// existing password is preserved.
func (db *DB) UpdateNPMInstance(id int64, n *NPMInstance) error {
	q := db.client.NPMInstance.UpdateOneID(int(id)).
		SetName(n.Name).
		SetURL(n.URL).
		SetUsername(n.Username).
		SetEnabled(n.Enabled)
	if n.Password != "" {
		q.SetPassword(n.Password)
	}
	_, err := q.Save(context.Background())
	return err
}

// DeleteNPMInstance removes an instance. NPM instances don't own monitors
// (monitors belong to Kuma instances), so no cascade is needed.
func (db *DB) DeleteNPMInstance(id int64) error {
	return db.client.NPMInstance.DeleteOneID(int(id)).Exec(context.Background())
}

// MigrateNPMInstances migrates the legacy single-instance npm_* settings
// into NPMInstance rows. It is idempotent: if instances already exist it
// does nothing.
func (db *DB) MigrateNPMInstances(legacy Settings) error {
	existing, err := db.GetNPMInstances()
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	if legacy.NPMHost == "" {
		return nil
	}
	_, err = db.CreateNPMInstance(&NPMInstance{
		Name:     "default",
		URL:      legacy.NPMHost,
		Username: legacy.NPMUser,
		Password: legacy.NPMPass,
		Enabled:  true,
	})
	return err
}

// --- AutheliaInstance CRUD ---

// CreateAutheliaInstance inserts a new Authelia instance row and returns it.
func (db *DB) CreateAutheliaInstance(a *AutheliaInstance) (*AutheliaInstance, error) {
	e, err := db.client.AutheliaInstance.Create().
		SetName(a.Name).
		SetConfigPath(a.ConfigPath).
		SetDbPath(a.DBPath).
		SetDefaultPolicy(a.DefaultPolicy).
		SetOverrides(a.Overrides).
		SetAutoSync(a.AutoSync).
		SetNpmInstanceIds(a.NPMInstanceIDs).
		SetEnabled(a.Enabled).
		SetCreatedAt(time.Now()).
		Save(context.Background())
	if err != nil {
		return nil, err
	}
	r := toAutheliaInstance(e)
	return &r, nil
}

// GetAutheliaInstances returns all configured Authelia instances ordered by id.
func (db *DB) GetAutheliaInstances() ([]AutheliaInstance, error) {
	entries, err := db.client.AutheliaInstance.Query().
		Order(autheliainstance.ByID(entsql.OrderAsc())).
		All(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]AutheliaInstance, len(entries))
	for i, e := range entries {
		result[i] = toAutheliaInstance(e)
	}
	return result, nil
}

// GetAutheliaInstance returns a single instance by id.
func (db *DB) GetAutheliaInstance(id int64) (*AutheliaInstance, error) {
	e, err := db.client.AutheliaInstance.Query().
		Where(autheliainstance.IDEQ(int(id))).
		Only(context.Background())
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	r := toAutheliaInstance(e)
	return &r, nil
}

// GetEnabledAutheliaInstances returns all enabled instances ordered by id.
func (db *DB) GetEnabledAutheliaInstances() ([]AutheliaInstance, error) {
	entries, err := db.client.AutheliaInstance.Query().
		Where(autheliainstance.Enabled(true)).
		Order(autheliainstance.ByID(entsql.OrderAsc())).
		All(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]AutheliaInstance, len(entries))
	for i, e := range entries {
		result[i] = toAutheliaInstance(e)
	}
	return result, nil
}

// UpdateAutheliaInstance updates an existing instance. Overrides is preserved
// if the incoming value is empty.
func (db *DB) UpdateAutheliaInstance(id int64, a *AutheliaInstance) error {
	q := db.client.AutheliaInstance.UpdateOneID(int(id)).
		SetName(a.Name).
		SetConfigPath(a.ConfigPath).
		SetDbPath(a.DBPath).
		SetDefaultPolicy(a.DefaultPolicy).
		SetAutoSync(a.AutoSync).
		SetNpmInstanceIds(a.NPMInstanceIDs).
		SetEnabled(a.Enabled)
	if a.Overrides != "" {
		q.SetOverrides(a.Overrides)
	}
	_, err := q.Save(context.Background())
	return err
}

// DeleteAutheliaInstance removes an instance and cascades its alerts and temp access rules.
func (db *DB) DeleteAutheliaInstance(id int64) error {
	// Delete alerts and temp access belonging to this instance first.
	_, err := db.rawDB.Exec("DELETE FROM authelia_alerts WHERE authelia_instance_id = ?", id)
	if err != nil {
		return err
	}
	_, err = db.rawDB.Exec("DELETE FROM temp_access WHERE authelia_instance_id = ?", id)
	if err != nil {
		return err
	}
	return db.client.AutheliaInstance.DeleteOneID(int(id)).Exec(context.Background())
}

// MigrateAutheliaInstances migrates the legacy single-instance authelia_*
// settings into AutheliaInstance rows. It is idempotent: if instances already
// exist it does nothing.
func (db *DB) MigrateAutheliaInstances(legacy Settings) error {
	existing, err := db.GetAutheliaInstances()
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	if legacy.AutheliaConfigPath == "" {
		return nil
	}

	inst, err := db.CreateAutheliaInstance(&AutheliaInstance{
		Name:          "default",
		ConfigPath:    legacy.AutheliaConfigPath,
		DBPath:        legacy.AutheliaDBPath,
		DefaultPolicy: legacy.AutheliaDefaultPolicy,
		Overrides:     legacy.AutheliaSyncOverrides,
		AutoSync:      legacy.AutheliaSyncEnabled,
		Enabled:       true,
		NPMInstanceIDs: "",
	})
	if err != nil {
		return err
	}

	// Backfill existing alerts and temp access to the default instance.
	_, err = db.rawDB.Exec("UPDATE authelia_alerts SET authelia_instance_id = ? WHERE authelia_instance_id IS NULL OR authelia_instance_id = 0", inst.ID)
	if err != nil {
		return err
	}
	_, err = db.rawDB.Exec("UPDATE temp_access SET authelia_instance_id = ? WHERE authelia_instance_id IS NULL OR authelia_instance_id = 0", inst.ID)
	return err
}

func (db *DB) GetSettings(defaults Settings) Settings {
	s := defaults
	rows, err := db.client.Settings.Query().All(context.Background())
	if err != nil {
		return s
	}
	for _, row := range rows {
		switch row.Key {
		case "compose_path":
			s.ComposePath = row.Value
		case "npm_host":
			s.NPMHost = row.Value
		case "npm_user":
			s.NPMUser = row.Value
		case "npm_pass":
			s.NPMPass = row.Value
		case "kuma_url":
			s.KumaURL = row.Value
		case "kuma_user":
			s.KumaUser = row.Value
		case "kuma_pass":
			s.KumaPass = row.Value
		case "authelia_config_path":
			s.AutheliaConfigPath = row.Value
		case "authelia_db_path":
			s.AutheliaDBPath = row.Value
		case "authelia_sync_enabled":
			s.AutheliaSyncEnabled = row.Value == "true"
		case "authelia_default_policy":
			s.AutheliaDefaultPolicy = row.Value
		case "authelia_sync_overrides":
			s.AutheliaSyncOverrides = row.Value
		case "otel_endpoint":
			s.OTelEndpoint = row.Value
		case "otel_enabled":
			s.OTelEnabled = row.Value == "true"
		case "eink_enabled":
			s.EinkEnabled = row.Value == "true"
		case "trmnl_api_token":
			s.TrmnlApiToken = row.Value
		case "notify_enabled":
			s.NotifyEnabled, _ = strconv.ParseBool(row.Value)
		case "notify_interval_minutes":
			if v, err := strconv.Atoi(row.Value); err == nil {
				s.NotifyIntervalMinutes = v
			}
		case "gotify_url":
			s.GotifyURL = row.Value
		case "gotify_token":
			s.GotifyToken = row.Value
		case "gotify_priority":
			if v, err := strconv.Atoi(row.Value); err == nil {
				s.GotifyPriority = v
			}
		case "docker_socket":
			s.DockerSocket = row.Value
		case "docker_events_enabled":
			s.DockerEventsEnabled = row.Value == "true"
		case "docker_events_retention_days":
			if v, err := strconv.Atoi(row.Value); err == nil {
				s.DockerEventsRetentionDays = v
			}
		case "reconcile_enabled":
			s.ReconcileEnabled = row.Value == "true"
		case "reconcile_interval_minutes":
			if v, err := strconv.Atoi(row.Value); err == nil {
				s.ReconcileIntervalMinutes = v
			}
		case "reconcile_dry_run_default":
			s.ReconcileDryRunDefault = row.Value == "true"
		case "notify_docker_die":
			s.NotifyDockerDie = row.Value == "true"
		case "notify_docker_health":
			s.NotifyDockerHealth = row.Value == "true"
		case "notify_docker_image":
			s.NotifyDockerImage = row.Value == "true"
		case "notify_reconcile":
			s.NotifyReconcile = row.Value == "true"
		case "notify_cooldown_minutes":
			if v, err := strconv.Atoi(row.Value); err == nil {
				s.NotifyCooldownMinutes = v
			}
		}
	}
	return s
}

func (db *DB) SaveSettingsMap(pairs map[string]string) error {
	for k, v := range pairs {
		created, err := db.client.Settings.Create().
			SetKey(k).
			SetValue(v).
			Save(context.Background())
		if err != nil {
			if ent.IsConstraintError(err) {
				_, err = db.client.Settings.Update().
					Where(settings.KeyEQ(k)).
					SetValue(v).
					Save(context.Background())
				if err != nil {
					return err
				}
				continue
			}
			return err
		}
		_ = created
	}
	return nil
}

// SaveSettings saves all known settings keys. Used by tests; prefer SaveSettingsMap for partial updates.
func (db *DB) SaveSettings(s Settings) error {
	return db.SaveSettingsMap(map[string]string{
		"compose_path":            s.ComposePath,
		"npm_host":                s.NPMHost,
		"npm_user":                s.NPMUser,
		"npm_pass":                s.NPMPass,
		"kuma_url":                s.KumaURL,
		"kuma_user":               s.KumaUser,
		"kuma_pass":               s.KumaPass,
		"authelia_config_path":    s.AutheliaConfigPath,
		"authelia_db_path":        s.AutheliaDBPath,
		"authelia_sync_enabled":   strconv.FormatBool(s.AutheliaSyncEnabled),
		"authelia_default_policy": s.AutheliaDefaultPolicy,
		"authelia_sync_overrides": s.AutheliaSyncOverrides,
		"otel_endpoint":           s.OTelEndpoint,
		"otel_enabled":            strconv.FormatBool(s.OTelEnabled),
		"eink_enabled":            strconv.FormatBool(s.EinkEnabled),
		"trmnl_api_token":         s.TrmnlApiToken,
		"notify_enabled":          strconv.FormatBool(s.NotifyEnabled),
		"notify_interval_minutes": strconv.Itoa(s.NotifyIntervalMinutes),
		"gotify_url":              s.GotifyURL,
		"gotify_token":            s.GotifyToken,
		"gotify_priority":         strconv.Itoa(s.GotifyPriority),
		"docker_socket":           s.DockerSocket,
		"docker_events_enabled":   strconv.FormatBool(s.DockerEventsEnabled),
		"docker_events_retention_days": strconv.Itoa(s.DockerEventsRetentionDays),
		"reconcile_enabled":       strconv.FormatBool(s.ReconcileEnabled),
		"reconcile_interval_minutes": strconv.Itoa(s.ReconcileIntervalMinutes),
		"reconcile_dry_run_default": strconv.FormatBool(s.ReconcileDryRunDefault),
		"notify_docker_die":       strconv.FormatBool(s.NotifyDockerDie),
		"notify_docker_health":    strconv.FormatBool(s.NotifyDockerHealth),
		"notify_docker_image":     strconv.FormatBool(s.NotifyDockerImage),
		"notify_reconcile":        strconv.FormatBool(s.NotifyReconcile),
		"notify_cooldown_minutes": strconv.Itoa(s.NotifyCooldownMinutes),
	})
}

// AutheliaAlert CRUD

func (db *DB) AddAutheliaAlert(a *AutheliaAlert) error {
	_, err := db.rawDB.Exec(
		"INSERT INTO authelia_alerts (cname, message, severity, status, authelia_instance_id, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		a.CNAME, a.Message, a.Severity, "open", a.AutheliaInstanceID, a.CreatedAt,
	)
	return err
}

func (db *DB) GetAutheliaAlerts(instanceID int64) ([]AutheliaAlert, error) {
	var rows *sql.Rows
	var err error
	if instanceID > 0 {
		rows, err = db.rawDB.Query(
			"SELECT id, cname, message, severity, status, authelia_instance_id, created_at FROM authelia_alerts WHERE authelia_instance_id = ? ORDER BY id DESC",
			instanceID,
		)
	} else {
		rows, err = db.rawDB.Query(
			"SELECT id, cname, message, severity, status, authelia_instance_id, created_at FROM authelia_alerts ORDER BY id DESC",
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []AutheliaAlert
	for rows.Next() {
		var a AutheliaAlert
		if err := rows.Scan(&a.ID, &a.CNAME, &a.Message, &a.Severity, &a.Status, &a.AutheliaInstanceID, &a.CreatedAt); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

func (db *DB) GetOpenAutheliaAlerts(instanceID int64) ([]AutheliaAlert, error) {
	var rows *sql.Rows
	var err error
	if instanceID > 0 {
		rows, err = db.rawDB.Query(
			"SELECT id, cname, message, severity, status, authelia_instance_id, created_at FROM authelia_alerts WHERE status = 'open' AND authelia_instance_id = ? ORDER BY id DESC",
			instanceID,
		)
	} else {
		rows, err = db.rawDB.Query(
			"SELECT id, cname, message, severity, status, authelia_instance_id, created_at FROM authelia_alerts WHERE status = 'open' ORDER BY id DESC",
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []AutheliaAlert
	for rows.Next() {
		var a AutheliaAlert
		if err := rows.Scan(&a.ID, &a.CNAME, &a.Message, &a.Severity, &a.Status, &a.AutheliaInstanceID, &a.CreatedAt); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

func (db *DB) ResolveAutheliaAlert(id int64) error {
	_, err := db.rawDB.Exec("UPDATE authelia_alerts SET status = 'resolved' WHERE id = ?", id)
	return err
}

// TempAccess CRUD

func (db *DB) AddTempAccess(t *TempAccess) error {
	_, err := db.rawDB.Exec(
		"INSERT INTO temp_access (ip, reason, authelia_instance_id, expires_at, created_at, status) VALUES (?, ?, ?, ?, ?, ?)",
		t.IP, t.Reason, t.AutheliaInstanceID, t.ExpiresAt, t.CreatedAt, "active",
	)
	return err
}

func (db *DB) GetTempAccessRules(instanceID int64) ([]TempAccess, error) {
	var rows *sql.Rows
	var err error
	if instanceID > 0 {
		rows, err = db.rawDB.Query(
			"SELECT id, ip, reason, authelia_instance_id, expires_at, created_at, status FROM temp_access WHERE authelia_instance_id = ? ORDER BY id DESC",
			instanceID,
		)
	} else {
		rows, err = db.rawDB.Query(
			"SELECT id, ip, reason, authelia_instance_id, expires_at, created_at, status FROM temp_access ORDER BY id DESC",
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []TempAccess
	for rows.Next() {
		var t TempAccess
		if err := rows.Scan(&t.ID, &t.IP, &t.Reason, &t.AutheliaInstanceID, &t.ExpiresAt, &t.CreatedAt, &t.Status); err != nil {
			return nil, err
		}
		rules = append(rules, t)
	}
	return rules, rows.Err()
}

func (db *DB) GetActiveTempAccess(instanceID int64) ([]TempAccess, error) {
	var rows *sql.Rows
	var err error
	if instanceID > 0 {
		rows, err = db.rawDB.Query(
			"SELECT id, ip, reason, authelia_instance_id, expires_at, created_at, status FROM temp_access WHERE status = 'active' AND authelia_instance_id = ? ORDER BY id DESC",
			instanceID,
		)
	} else {
		rows, err = db.rawDB.Query(
			"SELECT id, ip, reason, authelia_instance_id, expires_at, created_at, status FROM temp_access WHERE status = 'active' ORDER BY id DESC",
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []TempAccess
	for rows.Next() {
		var t TempAccess
		if err := rows.Scan(&t.ID, &t.IP, &t.Reason, &t.AutheliaInstanceID, &t.ExpiresAt, &t.CreatedAt, &t.Status); err != nil {
			return nil, err
		}
		rules = append(rules, t)
	}
	return rules, rows.Err()
}

func (db *DB) RevokeTempAccess(id int64) error {
	_, err := db.rawDB.Exec("UPDATE temp_access SET status = 'revoked' WHERE id = ?", id)
	return err
}

func (db *DB) CleanupExpiredTempAccess() error {
	now := time.Now()
	_, err := db.rawDB.Exec("UPDATE temp_access SET status = 'expired' WHERE status = 'active' AND expires_at <= ?", now)
	return err
}
