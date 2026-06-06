package db

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"synapse/ent"
	"synapse/ent/migrate"
	"synapse/ent/monitor"
	"synapse/ent/settings"
	"synapse/ent/syncrun"

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
	Skipped       int        `json:"skipped"`
	Failed        int        `json:"failed"`
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
	CreatedAt       time.Time `json:"created_at"`
}

type AdminUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Password string `json:"-"`
}

type AutheliaAlert struct {
	ID        int64     `json:"id"`
	CNAME     string    `json:"cname"`
	Message   string    `json:"message"`
	Severity  string    `json:"severity"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type TempAccess struct {
	ID        int64     `json:"id"`
	IP        string    `json:"ip"`
	Reason    string    `json:"reason"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"`
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
}

type DB struct {
	client  *ent.Client
	rawDB   *sql.DB
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
		created_at DATETIME NOT NULL
	)`)
	if err != nil {
		return err
	}

	_, err = db.rawDB.Exec(`CREATE TABLE IF NOT EXISTS temp_access (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ip TEXT NOT NULL,
		reason TEXT NOT NULL DEFAULT '',
		expires_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL,
		status TEXT NOT NULL DEFAULT 'active'
	)`)
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
		Skipped:       e.Skipped,
		Failed:        e.Failed,
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
		CreatedAt:       e.CreatedAt,
	}
}

func (db *DB) CreateSyncRun(run *SyncRun) (int64, error) {
	q := db.client.SyncRun.Create().
		SetSource(run.Source).
		SetStatus(run.Status).
		SetStartedAt(run.StartedAt).
		SetTotalServices(run.TotalServices).
		SetAdded(run.Added).
		SetSkipped(run.Skipped).
		SetFailed(run.Failed)
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
		}
	}
	return s
}

func (db *DB) SaveSettings(s Settings) error {
	syncEnabled := "false"
	if s.AutheliaSyncEnabled {
		syncEnabled = "true"
	}

	pairs := map[string]string{
		"compose_path":            s.ComposePath,
		"npm_host":               s.NPMHost,
		"npm_user":               s.NPMUser,
		"npm_pass":               s.NPMPass,
		"kuma_url":               s.KumaURL,
		"kuma_user":               s.KumaUser,
		"kuma_pass":               s.KumaPass,
		"authelia_config_path":    s.AutheliaConfigPath,
		"authelia_db_path":        s.AutheliaDBPath,
		"authelia_sync_enabled":   syncEnabled,
		"authelia_default_policy": s.AutheliaDefaultPolicy,
		"authelia_sync_overrides": s.AutheliaSyncOverrides,
		"otel_endpoint":           s.OTelEndpoint,
		"otel_enabled":            strconv.FormatBool(s.OTelEnabled),
	}
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

// AutheliaAlert CRUD

func (db *DB) AddAutheliaAlert(a *AutheliaAlert) error {
	_, err := db.rawDB.Exec(
		"INSERT INTO authelia_alerts (cname, message, severity, status, created_at) VALUES (?, ?, ?, ?, ?)",
		a.CNAME, a.Message, a.Severity, "open", a.CreatedAt,
	)
	return err
}

func (db *DB) GetAutheliaAlerts() ([]AutheliaAlert, error) {
	rows, err := db.rawDB.Query(
		"SELECT id, cname, message, severity, status, created_at FROM authelia_alerts ORDER BY id DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []AutheliaAlert
	for rows.Next() {
		var a AutheliaAlert
		if err := rows.Scan(&a.ID, &a.CNAME, &a.Message, &a.Severity, &a.Status, &a.CreatedAt); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

func (db *DB) GetOpenAutheliaAlerts() ([]AutheliaAlert, error) {
	rows, err := db.rawDB.Query(
		"SELECT id, cname, message, severity, status, created_at FROM authelia_alerts WHERE status = 'open' ORDER BY id DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []AutheliaAlert
	for rows.Next() {
		var a AutheliaAlert
		if err := rows.Scan(&a.ID, &a.CNAME, &a.Message, &a.Severity, &a.Status, &a.CreatedAt); err != nil {
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
		"INSERT INTO temp_access (ip, reason, expires_at, created_at, status) VALUES (?, ?, ?, ?, ?)",
		t.IP, t.Reason, t.ExpiresAt, t.CreatedAt, "active",
	)
	return err
}

func (db *DB) GetTempAccessRules() ([]TempAccess, error) {
	rows, err := db.rawDB.Query(
		"SELECT id, ip, reason, expires_at, created_at, status FROM temp_access ORDER BY id DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []TempAccess
	for rows.Next() {
		var t TempAccess
		if err := rows.Scan(&t.ID, &t.IP, &t.Reason, &t.ExpiresAt, &t.CreatedAt, &t.Status); err != nil {
			return nil, err
		}
		rules = append(rules, t)
	}
	return rules, rows.Err()
}

func (db *DB) GetActiveTempAccess() ([]TempAccess, error) {
	rows, err := db.rawDB.Query(
		"SELECT id, ip, reason, expires_at, created_at, status FROM temp_access WHERE status = 'active' ORDER BY id DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []TempAccess
	for rows.Next() {
		var t TempAccess
		if err := rows.Scan(&t.ID, &t.IP, &t.Reason, &t.ExpiresAt, &t.CreatedAt, &t.Status); err != nil {
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
