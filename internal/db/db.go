package db

import (
	"context"
	"database/sql"
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
	KumaInstanceID  int       `json:"kuma_instance_id"`
	CreatedAt       time.Time `json:"created_at"`
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
