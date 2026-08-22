package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"synapse/internal/db"
)

// alertRuleInput is the create/update payload for alert rules. Threshold
// accepts either a duration string ("10m", "90s", "1h30m") or a number of
// seconds; both normalize to threshold_seconds.
type alertRuleInput struct {
	Name             *string `json:"name"`
	Type             *string `json:"type"`
	Subject          *string `json:"subject"`
	Threshold        any     `json:"threshold,omitempty"`
	ThresholdSeconds *int    `json:"threshold_seconds,omitempty"`
	Enabled          *bool   `json:"enabled"`
}

// parseThresholdSeconds normalizes the threshold field to seconds. It returns
// (seconds, nil) when provided and (0, err) on malformed input; ok=false when
// absent.
func parseThresholdSeconds(input alertRuleInput) (int, bool, error) {
	switch {
	case input.Threshold != nil:
		switch v := input.Threshold.(type) {
		case string:
			d, err := time.ParseDuration(strings.TrimSpace(v))
			if err != nil || d <= 0 {
				return 0, true, fmt.Errorf("invalid threshold %q: use a positive duration like \"10m\"", v)
			}
			return int(d.Seconds()), true, nil
		case float64:
			if v <= 0 {
				return 0, true, fmt.Errorf("threshold must be a positive number of seconds")
			}
			return int(v), true, nil
		default:
			return 0, true, fmt.Errorf("threshold must be a duration string or seconds")
		}
	case input.ThresholdSeconds != nil:
		if *input.ThresholdSeconds <= 0 {
			return 0, true, fmt.Errorf("threshold_seconds must be positive")
		}
		return *input.ThresholdSeconds, true, nil
	default:
		return 0, false, nil
	}
}

// validateAlertRule checks type-specific constraints shared by create and
// update. monitor_down_for/container_down need a subject and positive
// threshold; sync_stale needs a positive threshold and an optional
// docker/npm subject; reconcile_drift is global with optional threshold.
func validateAlertRule(typ, subject string, thresholdSeconds int) error {
	if !db.ValidAlertType(typ) {
		return fmt.Errorf("unknown rule type %q (valid: monitor_down_for, container_down, sync_stale, reconcile_drift)", typ)
	}
	switch typ {
	case db.AlertTypeMonitorDownFor, db.AlertTypeContainerDown:
		if strings.TrimSpace(subject) == "" {
			return fmt.Errorf("subject is required for %s rules", typ)
		}
		if thresholdSeconds <= 0 {
			return fmt.Errorf("threshold is required for %s rules", typ)
		}
	case db.AlertTypeSyncStale:
		sub := strings.TrimSpace(subject)
		if sub != "" && sub != "docker" && sub != "npm" {
			return fmt.Errorf("subject for sync_stale must be \"docker\" or \"npm\"")
		}
		if thresholdSeconds <= 0 {
			return fmt.Errorf("threshold is required for sync_stale rules")
		}
	case db.AlertTypeReconcileDrift:
		if thresholdSeconds < 0 {
			return fmt.Errorf("threshold must not be negative")
		}
	}
	return nil
}

// AlertRules lists every alert rule.
func (app *App) AlertRules(c *gin.Context) {
	rules, err := app.database.ListAlertRules()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rules)
}

// CreateAlertRule persists a new rule after validation and name-uniqueness
// checks.
func (app *App) CreateAlertRule(c *gin.Context) {
	var input alertRuleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if input.Name == nil || strings.TrimSpace(*input.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if input.Type == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type is required"})
		return
	}
	name := strings.TrimSpace(*input.Name)
	typ := *input.Type
	subject := ""
	if input.Subject != nil {
		subject = strings.TrimSpace(*input.Subject)
	}
	threshold, provided, err := parseThresholdSeconds(input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !provided {
		// Absent thresholds default to 0; validateAlertRule enforces the
		// per-type minimum (duration types require > 0).
		threshold = 0
	}
	if err := validateAlertRule(typ, subject, threshold); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if existing, err := app.database.GetAlertRuleByName(name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	} else if existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("rule name %q already exists", name)})
		return
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	rule := &db.AlertRule{Name: name, Type: typ, Subject: subject, Threshold: threshold, Enabled: enabled}
	id, err := app.database.CreateAlertRule(rule)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	rule.ID = id
	c.JSON(http.StatusCreated, rule)
}

// UpdateAlertRule applies partial changes to an existing rule.
func (app *App) UpdateAlertRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule id"})
		return
	}
	rule, err := app.database.GetAlertRule(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rule == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "rule not found"})
		return
	}
	var input alertRuleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if input.Name != nil && strings.TrimSpace(*input.Name) != "" {
		name := strings.TrimSpace(*input.Name)
		if existing, err := app.database.GetAlertRuleByName(name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		} else if existing != nil && existing.ID != rule.ID {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("rule name %q already exists", name)})
			return
		}
		rule.Name = name
	}
	if input.Type != nil {
		rule.Type = *input.Type
	}
	if input.Subject != nil {
		rule.Subject = strings.TrimSpace(*input.Subject)
	}
	if threshold, provided, err := parseThresholdSeconds(input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	} else if provided {
		rule.Threshold = threshold
	}
	if input.Enabled != nil {
		rule.Enabled = *input.Enabled
	}
	if err := validateAlertRule(rule.Type, rule.Subject, rule.Threshold); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := app.database.UpdateAlertRule(rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	updated, _ := app.database.GetAlertRule(rule.ID)
	c.JSON(http.StatusOK, updated)
}

// DeleteAlertRule removes a rule; its incidents cascade.
func (app *App) DeleteAlertRule(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule id"})
		return
	}
	existing, err := app.database.GetAlertRule(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "rule not found"})
		return
	}
	if err := app.database.DeleteAlertRule(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": id})
}

// Incidents lists incidents newest-first, optionally filtered by ?status=.
func (app *App) Incidents(c *gin.Context) {
	status := c.Query("status")
	limit := 100
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 && v <= 1000 {
		limit = v
	}
	incidents, err := app.database.ListIncidents(status, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, incidents)
}

// AckIncident marks an open incident acknowledged.
func (app *App) AckIncident(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid incident id"})
		return
	}
	inc, err := app.database.GetIncident(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if inc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "incident not found"})
		return
	}
	if inc.Status != "open" {
		c.JSON(http.StatusConflict, gin.H{"error": "only open incidents can be acknowledged"})
		return
	}
	if err := app.database.AckIncident(id, time.Now()); err != nil {
		c.JSON(apiStatus(err), gin.H{"error": err.Error()})
		return
	}
	updated, _ := app.database.GetIncident(id)
	c.JSON(http.StatusOK, updated)
}

// ResolveIncident manually resolves an unresolved incident.
func (app *App) ResolveIncident(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid incident id"})
		return
	}
	inc, err := app.database.GetIncident(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if inc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "incident not found"})
		return
	}
	if inc.Status == "resolved" {
		c.JSON(http.StatusConflict, gin.H{"error": "incident already resolved"})
		return
	}
	if err := app.database.ResolveIncident(id, time.Now()); err != nil {
		c.JSON(apiStatus(err), gin.H{"error": err.Error()})
		return
	}
	updated, _ := app.database.GetIncident(id)
	c.JSON(http.StatusOK, updated)
}
