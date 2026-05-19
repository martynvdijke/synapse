package db

import (
	"context"
	"time"

	"synapse/ent"
	"synapse/ent/migrate"
	"synapse/ent/monitor"
	"synapse/ent/settings"
	"synapse/ent/syncrun"

	"entgo.io/ent/dialect/sql"
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

type Settings struct {
	ComposePath string `json:"compose_path"`
	NPMHost     string `json:"npm_host"`
	NPMUser     string `json:"npm_user"`
	NPMPass     string `json:"npm_pass"`
	KumaURL     string `json:"kuma_url"`
	KumaUser    string `json:"kuma_user"`
	KumaPass    string `json:"kuma_pass"`
}

type DB struct {
	client *ent.Client
}

func Open(path string) (*DB, error) {
	drv, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_fk=1")
	if err != nil {
		return nil, err
	}
	sqlDB := drv.DB()
	sqlDB.SetMaxOpenConns(1)

	client := ent.NewClient(ent.Driver(drv))
	if err := client.Schema.Create(context.Background(), migrate.WithForeignKeys(false)); err != nil {
		return nil, err
	}
	return &DB{client: client}, nil
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
		Order(syncrun.ByID(sql.OrderDesc())).
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
	q := db.client.SyncRun.Query().Order(syncrun.ByID(sql.OrderDesc())).Limit(1)
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
		Order(monitor.ByID(sql.OrderDesc())).
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
		}
	}
	return s
}

func (db *DB) SaveSettings(s Settings) error {
	pairs := map[string]string{
		"compose_path": s.ComposePath,
		"npm_host":    s.NPMHost,
		"npm_user":    s.NPMUser,
		"npm_pass":    s.NPMPass,
		"kuma_url":    s.KumaURL,
		"kuma_user":    s.KumaUser,
		"kuma_pass":    s.KumaPass,
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
