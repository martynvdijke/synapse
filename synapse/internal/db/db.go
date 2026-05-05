package db

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type SyncRun struct {
	ID            int64     `json:"id"`
	Status        string    `json:"status"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	TotalServices int       `json:"total_services"`
	Added         int       `json:"added"`
	Skipped       int       `json:"skipped"`
	Failed        int       `json:"failed"`
	ErrorMessage  string    `json:"error_message,omitempty"`
}

type Monitor struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	ServiceName    string    `json:"service_name"`
	MonitorType    string    `json:"monitor_type"`
	URL            string    `json:"url,omitempty"`
	DockerContainer string   `json:"docker_container,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
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

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, err
	}
	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) migrate() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS sync_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
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
			created_at TEXT NOT NULL,
			UNIQUE(name)
		);
	`)
	return err
}

func (db *DB) CreateSyncRun(run *SyncRun) (int64, error) {
	res, err := db.conn.Exec(
		`INSERT INTO sync_runs (status, started_at, total_services, added, skipped, failed)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		run.Status, run.StartedAt.Format(time.RFC3339),
		run.TotalServices, run.Added, run.Skipped, run.Failed,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) FinishSyncRun(id int64, status string, added, skipped, failed int, errMsg string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := db.conn.Exec(
		`UPDATE sync_runs SET status=?, finished_at=?, added=?, skipped=?, failed=?, error_message=?
		 WHERE id=?`,
		status, now, added, skipped, failed, errMsg, id,
	)
	return err
}

func (db *DB) GetSyncRuns(limit int) ([]SyncRun, error) {
	rows, err := db.conn.Query(
		`SELECT id, status, started_at, finished_at, total_services, added, skipped, failed, COALESCE(error_message,'')
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
		if err := rows.Scan(&r.ID, &r.Status, &startedStr, &finishedStr,
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

func (db *DB) GetLatestSyncRun() (*SyncRun, error) {
	run := &SyncRun{}
	var startedStr, finishedStr sql.NullString
	err := db.conn.QueryRow(
		`SELECT id, status, started_at, finished_at, total_services, added, skipped, failed, COALESCE(error_message,'')
		 FROM sync_runs ORDER BY id DESC LIMIT 1`,
	).Scan(&run.ID, &run.Status, &startedStr, &finishedStr,
		&run.TotalServices, &run.Added, &run.Skipped, &run.Failed, &run.ErrorMessage)
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
	_, err := db.conn.Exec(
		`INSERT OR IGNORE INTO monitors (name, service_name, monitor_type, url, docker_container, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		m.Name, m.ServiceName, m.MonitorType, m.URL, m.DockerContainer, m.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (db *DB) GetMonitors() ([]Monitor, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, service_name, monitor_type, COALESCE(url,''), COALESCE(docker_container,''), created_at
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
		if err := rows.Scan(&m.ID, &m.Name, &m.ServiceName, &m.MonitorType, &m.URL, &m.DockerContainer, &createdStr); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		monitors = append(monitors, m)
	}
	return monitors, nil
}

func (db *DB) GetMonitorCount() (int, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM monitors`).Scan(&count)
	return count, err
}
