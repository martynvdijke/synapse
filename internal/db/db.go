package db

import (
	"context"
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer trace.Tracer

func init() {
	tracer = otel.Tracer("db")
}

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

type Settings struct {
	ComposePath string `json:"compose_path"`
	NPMHost     string `json:"npm_host"`
	NPMUser     string `json:"npm_user"`
	NPMPass     string `json:"npm_pass"`
	KumaURL     string `json:"kuma_url"`
	KumaUser    string `json:"kuma_user"`
	KumaPass    string `json:"kuma_pass"`
}

type SettingsPublic struct {
	ComposePath string `json:"compose_path"`
	NPMHost     string `json:"npm_host"`
	NPMUser     string `json:"npm_user"`
	NPMPass     string `json:"npm_pass"`
	KumaURL     string `json:"kuma_url"`
	KumaUser    string `json:"kuma_user"`
	KumaPass    string `json:"kuma_pass"`
}

type DB struct {
	conn *sql.DB
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)

	database := &DB{conn: conn}
	if err := database.migrate(); err != nil {
		return nil, err
	}
	return database, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) migrate() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS sync_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source TEXT NOT NULL DEFAULT 'docker',
			status TEXT NOT NULL DEFAULT 'pending',
			started_at TEXT NOT NULL,
			finished_at TEXT,
			total_services INTEGER NOT NULL DEFAULT 0,
			added INTEGER NOT NULL DEFAULT 0,
			skipped INTEGER NOT NULL DEFAULT 0,
			failed INTEGER NOT NULL DEFAULT 0,
			error_message TEXT
		);

		CREATE TABLE IF NOT EXISTS monitors (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			service_name TEXT NOT NULL,
			monitor_type TEXT NOT NULL,
			url TEXT,
			docker_container TEXT,
			kuma_id INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			UNIQUE(name, monitor_type)
		);

		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`)
	return err
}

func (db *DB) CreateSyncRun(run *SyncRun) (int64, error) {
	_, span := tracer.Start(context.Background(), "CreateSyncRun",
		trace.WithAttributes(attribute.String("source", run.Source)),
	)
	defer span.End()

	res, err := db.conn.Exec(
		`INSERT INTO sync_runs (source, status, started_at, total_services, added, skipped, failed)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		run.Source, run.Status, run.StartedAt.Format(time.RFC3339),
		run.TotalServices, run.Added, run.Skipped, run.Failed,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) FinishSyncRun(id int64, status string, added, skipped, failed int, errMsg string) error {
	_, span := tracer.Start(context.Background(), "FinishSyncRun",
		trace.WithAttributes(
			attribute.Int64("run_id", id),
			attribute.String("status", status),
			attribute.Int("added", added),
			attribute.Int("skipped", skipped),
			attribute.Int("failed", failed),
		),
	)
	defer span.End()

	now := time.Now().Format(time.RFC3339)
	_, err := db.conn.Exec(
		`UPDATE sync_runs SET status=?, finished_at=?, added=?, skipped=?, failed=?, error_message=?
		 WHERE id=?`,
		status, now, added, skipped, failed, errMsg, id,
	)
	return err
}

func (db *DB) GetSyncRuns(limit int) ([]SyncRun, error) {
	_, span := tracer.Start(context.Background(), "GetSyncRuns",
		trace.WithAttributes(attribute.Int("limit", limit)),
	)
	defer span.End()

	rows, err := db.conn.Query(
		`SELECT id, source, status, started_at, finished_at, total_services, added, skipped, failed, COALESCE(error_message,'')
		 FROM sync_runs ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []SyncRun
	for rows.Next() {
		var r SyncRun
		var startedStr, finishedStr sql.NullString
		if err := rows.Scan(&r.ID, &r.Source, &r.Status, &startedStr, &finishedStr,
			&r.TotalServices, &r.Added, &r.Skipped, &r.Failed, &r.ErrorMessage); err != nil {
			return nil, err
		}
		if startedStr.Valid {
			r.StartedAt, _ = time.Parse(time.RFC3339, startedStr.String)
		}
		if finishedStr.Valid {
			t, _ := time.Parse(time.RFC3339, finishedStr.String)
			r.FinishedAt = &t
		}
		runs = append(runs, r)
	}
	return runs, nil
}

func (db *DB) GetLatestSyncRun(source string) (*SyncRun, error) {
	_, span := tracer.Start(context.Background(), "GetLatestSyncRun",
		trace.WithAttributes(attribute.String("source", source)),
	)
	defer span.End()

	run := &SyncRun{}
	var startedStr, finishedStr sql.NullString

	query := `SELECT id, source, status, started_at, finished_at, total_services, added, skipped, failed, COALESCE(error_message,'')
		 FROM sync_runs`
	args := []any{}

	if source != "" {
		query += ` WHERE source=?`
		args = append(args, source)
	}

	query += ` ORDER BY id DESC LIMIT 1`

	err := db.conn.QueryRow(query, args...).Scan(
		&run.ID, &run.Source, &run.Status, &startedStr, &finishedStr,
		&run.TotalServices, &run.Added, &run.Skipped, &run.Failed, &run.ErrorMessage,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if startedStr.Valid {
		run.StartedAt, _ = time.Parse(time.RFC3339, startedStr.String)
	}
	if finishedStr.Valid {
		t, _ := time.Parse(time.RFC3339, finishedStr.String)
		run.FinishedAt = &t
	}
	return run, nil
}

func (db *DB) AddMonitor(m *Monitor) error {
	_, span := tracer.Start(context.Background(), "AddMonitor",
		trace.WithAttributes(
			attribute.String("name", m.Name),
			attribute.String("monitor_type", m.MonitorType),
		),
	)
	defer span.End()

	_, err := db.conn.Exec(
		`INSERT OR IGNORE INTO monitors (name, service_name, monitor_type, url, docker_container, kuma_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.Name, m.ServiceName, m.MonitorType, m.URL, m.DockerContainer, m.KumaID, m.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (db *DB) GetMonitors() ([]Monitor, error) {
	_, span := tracer.Start(context.Background(), "GetMonitors")
	defer span.End()

	rows, err := db.conn.Query(
		`SELECT id, name, service_name, monitor_type, COALESCE(url,''), COALESCE(docker_container,''), kuma_id, created_at
		 FROM monitors ORDER BY id DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var monitors []Monitor
	for rows.Next() {
		var m Monitor
		var createdStr string
		if err := rows.Scan(&m.ID, &m.Name, &m.ServiceName, &m.MonitorType, &m.URL, &m.DockerContainer, &m.KumaID, &createdStr); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		monitors = append(monitors, m)
	}
	return monitors, nil
}

func (db *DB) GetMonitorCount() (int, error) {
	_, span := tracer.Start(context.Background(), "GetMonitorCount")
	defer span.End()

	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM monitors`).Scan(&count)
	return count, err
}

func (db *DB) getSetting(key string) (string, error) {
	var value string
	err := db.conn.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (db *DB) setSetting(key, value string) error {
	_, err := db.conn.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=?`,
		key, value, value,
	)
	return err
}

func (db *DB) GetSettings(defaults Settings) Settings {
	_, span := tracer.Start(context.Background(), "GetSettings")
	defer span.End()

	s := defaults
	if v, err := db.getSetting("compose_path"); err == nil && v != "" {
		s.ComposePath = v
	}
	if v, err := db.getSetting("npm_host"); err == nil && v != "" {
		s.NPMHost = v
	}
	if v, err := db.getSetting("npm_user"); err == nil && v != "" {
		s.NPMUser = v
	}
	if v, err := db.getSetting("npm_pass"); err == nil && v != "" {
		s.NPMPass = v
	}
	if v, err := db.getSetting("kuma_url"); err == nil && v != "" {
		s.KumaURL = v
	}
	if v, err := db.getSetting("kuma_user"); err == nil && v != "" {
		s.KumaUser = v
	}
	if v, err := db.getSetting("kuma_pass"); err == nil && v != "" {
		s.KumaPass = v
	}
	return s
}

func (db *DB) SaveSettings(s Settings) error {
	_, span := tracer.Start(context.Background(), "SaveSettings")
	defer span.End()

	pairs := map[string]string{
		"compose_path": s.ComposePath,
		"npm_host":     s.NPMHost,
		"npm_user":      s.NPMUser,
		"npm_pass":      s.NPMPass,
		"kuma_url":     s.KumaURL,
		"kuma_user":     s.KumaUser,
		"kuma_pass":     s.KumaPass,
	}
	for k, v := range pairs {
		if err := db.setSetting(k, v); err != nil {
			return err
		}
	}
	return nil
}
