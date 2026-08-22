package db

import (
	"database/sql"
	"fmt"
	"time"
)

// AlertRule is a stateful condition evaluated periodically by the alerts
// engine. Type is one of monitor_down_for / container_down / sync_stale /
// reconcile_drift; Subject selects the monitored entity (monitor or container
// name, sync source, empty for global rules); Threshold is the duration the
// condition must hold before an incident opens.
type AlertRule struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Subject    string    `json:"subject"`
	Threshold  int       `json:"threshold_seconds"` // seconds
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// AlertIncident is a persisted occurrence of a rule condition. Status is
// open / acknowledged / resolved. Auto-resolve and manual transitions are
// managed by the alerts engine and API handlers.
type AlertIncident struct {
	ID             int64      `json:"id"`
	RuleID         int64      `json:"rule_id"`
	RuleName       string     `json:"rule_name,omitempty"`
	Subject        string     `json:"subject"`
	Status         string     `json:"status"`
	Message        string     `json:"message"`
	OpenedAt       time.Time  `json:"opened_at"`
	AckAt          *time.Time `json:"ack_at,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	LastNotifiedAt *time.Time `json:"last_notified_at,omitempty"`
}

// Valid rule types.
const (
	AlertTypeMonitorDownFor = "monitor_down_for"
	AlertTypeContainerDown  = "container_down"
	AlertTypeSyncStale      = "sync_stale"
	AlertTypeReconcileDrift = "reconcile_drift"
)

// ValidAlertType reports whether t is a known rule type.
func ValidAlertType(t string) bool {
	switch t {
	case AlertTypeMonitorDownFor, AlertTypeContainerDown, AlertTypeSyncStale, AlertTypeReconcileDrift:
		return true
	}
	return false
}

func (db *DB) createAlertTables() error {
	_, err := db.rawDB.Exec(`CREATE TABLE IF NOT EXISTS alert_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		type TEXT NOT NULL,
		subject TEXT NOT NULL DEFAULT '',
		threshold_seconds INTEGER NOT NULL DEFAULT 0,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = db.rawDB.Exec(`CREATE TABLE IF NOT EXISTS alert_incidents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		rule_id INTEGER NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
		subject TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'open',
		message TEXT NOT NULL DEFAULT '',
		opened_at DATETIME NOT NULL,
		ack_at DATETIME,
		resolved_at DATETIME,
		last_notified_at DATETIME
	)`)
	if err != nil {
		return err
	}
	_, err = db.rawDB.Exec(`CREATE INDEX IF NOT EXISTS idx_alert_incidents_rule ON alert_incidents(rule_id, status)`)
	return err
}

func scanAlertRule(row interface{ Scan(...any) error }) (*AlertRule, error) {
	r := &AlertRule{}
	var enabled int
	err := row.Scan(&r.ID, &r.Name, &r.Type, &r.Subject, &r.Threshold, &enabled, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.Enabled = enabled != 0
	return r, nil
}

const alertRuleCols = "id, name, type, subject, threshold_seconds, enabled, created_at, updated_at"

// ListAlertRules returns every rule ordered by name.
func (db *DB) ListAlertRules() ([]AlertRule, error) {
	rows, err := db.rawDB.Query("SELECT " + alertRuleCols + " FROM alert_rules ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertRule{}
	for rows.Next() {
		r, err := scanAlertRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetAlertRule fetches one rule by id.
func (db *DB) GetAlertRule(id int64) (*AlertRule, error) {
	r, err := scanAlertRule(db.rawDB.QueryRow("SELECT "+alertRuleCols+" FROM alert_rules WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// GetAlertRuleByName fetches one rule by unique name.
func (db *DB) GetAlertRuleByName(name string) (*AlertRule, error) {
	r, err := scanAlertRule(db.rawDB.QueryRow("SELECT "+alertRuleCols+" FROM alert_rules WHERE name = ?", name))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// CreateAlertRule inserts a rule; name uniqueness is enforced by the schema.
func (db *DB) CreateAlertRule(r *AlertRule) (int64, error) {
	now := time.Now()
	res, err := db.rawDB.Exec(
		"INSERT INTO alert_rules (name, type, subject, threshold_seconds, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		r.Name, r.Type, r.Subject, r.Threshold, boolToInt(r.Enabled), now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateAlertRule overwrites the mutable fields of an existing rule.
func (db *DB) UpdateAlertRule(r *AlertRule) error {
	_, err := db.rawDB.Exec(
		"UPDATE alert_rules SET name = ?, type = ?, subject = ?, threshold_seconds = ?, enabled = ?, updated_at = ? WHERE id = ?",
		r.Name, r.Type, r.Subject, r.Threshold, boolToInt(r.Enabled), time.Now(), r.ID,
	)
	return err
}

// DeleteAlertRule removes a rule; incidents cascade via FK.
func (db *DB) DeleteAlertRule(id int64) error {
	_, err := db.rawDB.Exec("DELETE FROM alert_rules WHERE id = ?", id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- Incidents ---

func scanIncident(row interface{ Scan(...any) error }) (*AlertIncident, error) {
	i := &AlertIncident{}
	var ack, resolved, notified sql.NullTime
	err := row.Scan(&i.ID, &i.RuleID, &i.Subject, &i.Status, &i.Message, &i.OpenedAt, &ack, &resolved, &notified)
	if err != nil {
		return nil, err
	}
	i.AckAt, i.ResolvedAt, i.LastNotifiedAt = nullTimePtr(ack), nullTimePtr(resolved), nullTimePtr(notified)
	return i, nil
}

func nullTimePtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}

const incidentCols = "i.id, i.rule_id, i.subject, i.status, i.message, i.opened_at, i.ack_at, i.resolved_at, i.last_notified_at"

// OpenIncident creates an incident for rule/subject unless an unresolved one
// already exists (idempotent). It returns the incident and whether it was
// newly created.
func (db *DB) OpenIncident(ruleID int64, subject, message string, now time.Time) (*AlertIncident, bool, error) {
	existing, err := db.UnresolvedIncident(ruleID, subject)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, false, nil
	}
	res, err := db.rawDB.Exec(
		"INSERT INTO alert_incidents (rule_id, subject, status, message, opened_at) VALUES (?, ?, 'open', ?, ?)",
		ruleID, subject, message, now,
	)
	if err != nil {
		return nil, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, false, err
	}
	inc, err := db.GetIncident(id)
	if err != nil {
		return nil, false, err
	}
	if inc == nil {
		return nil, false, fmt.Errorf("incident %d vanished after insert", id)
	}
	return inc, true, nil
}

// UnresolvedIncident returns the open/acknowledged incident for a rule and
// subject, or nil when none exists.
func (db *DB) UnresolvedIncident(ruleID int64, subject string) (*AlertIncident, error) {
	i, err := scanIncident(db.rawDB.QueryRow(
		"SELECT "+incidentCols+" FROM alert_incidents i WHERE i.rule_id = ? AND i.subject = ? AND i.status IN ('open','acknowledged') ORDER BY i.id DESC LIMIT 1",
		ruleID, subject,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return i, err
}

// GetIncident fetches one incident by id.
func (db *DB) GetIncident(id int64) (*AlertIncident, error) {
	i, err := scanIncident(db.rawDB.QueryRow("SELECT "+incidentCols+" FROM alert_incidents i WHERE i.id = ?", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return i, err
}

// ListIncidents returns incidents newest-first, optionally filtered by status
// (empty = all). Rule names are joined in for display.
func (db *DB) ListIncidents(status string, limit int) ([]AlertIncident, error) {
	q := "SELECT " + incidentCols + ", r.name FROM alert_incidents i JOIN alert_rules r ON r.id = i.rule_id"
	var args []any
	if status != "" {
		q += " WHERE i.status = ?"
		args = append(args, status)
	}
	q += " ORDER BY i.id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := db.rawDB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertIncident{}
	for rows.Next() {
		i := &AlertIncident{}
		var ack, resolved, notified sql.NullTime
		if err := rows.Scan(&i.ID, &i.RuleID, &i.Subject, &i.Status, &i.Message, &i.OpenedAt, &ack, &resolved, &notified, &i.RuleName); err != nil {
			return nil, err
		}
		i.AckAt, i.ResolvedAt, i.LastNotifiedAt = nullTimePtr(ack), nullTimePtr(resolved), nullTimePtr(notified)
		out = append(out, *i)
	}
	return out, rows.Err()
}

// CountOpenIncidents returns how many incidents still need attention
// (status = open only; acknowledged ones are excluded).
func (db *DB) CountOpenIncidents() (int, error) {
	var n int
	err := db.rawDB.QueryRow("SELECT COUNT(*) FROM alert_incidents WHERE status = 'open'").Scan(&n)
	return n, err
}

// AckIncident marks an open incident acknowledged.
func (db *DB) AckIncident(id int64, now time.Time) error {
	res, err := db.rawDB.Exec(
		"UPDATE alert_incidents SET status = 'acknowledged', ack_at = ? WHERE id = ? AND status = 'open'",
		now, id,
	)
	if err != nil {
		return err
	}
	return requireAffected(res, "ack")
}

// ResolveIncident manually resolves an unresolved incident.
func (db *DB) ResolveIncident(id int64, now time.Time) error {
	res, err := db.rawDB.Exec(
		"UPDATE alert_incidents SET status = 'resolved', resolved_at = ? WHERE id = ? AND status IN ('open','acknowledged')",
		now, id,
	)
	if err != nil {
		return err
	}
	return requireAffected(res, "resolve")
}

// AutoResolveIncident resolves an incident when its condition cleared.
func (db *DB) AutoResolveIncident(id int64, now time.Time) error {
	_, err := db.rawDB.Exec(
		"UPDATE alert_incidents SET status = 'resolved', resolved_at = ? WHERE id = ? AND status IN ('open','acknowledged')",
		now, id,
	)
	return err
}

// MarkIncidentNotified records that a notification was sent for an incident
// (reminder clock reset).
func (db *DB) MarkIncidentNotified(id int64, now time.Time) error {
	_, err := db.rawDB.Exec("UPDATE alert_incidents SET last_notified_at = ? WHERE id = ?", now, id)
	return err
}

func requireAffected(res sql.Result, op string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("incident not found or not in %q-able state", op)
	}
	return nil
}
