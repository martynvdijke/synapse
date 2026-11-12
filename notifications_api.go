package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"synapse/internal/db"
	"synapse/internal/push"
)

// requestUserID resolves the acting user from the session or, for bearer-token
// automation, from the token owner. Mutating routes accept either.
func requestUserID(c *gin.Context) (int64, bool) {
	if id, ok := sessionUserID(c); ok && id != 0 {
		return id, true
	}
	if v, ok := c.Get("api_token_owner_id"); ok {
		if id, ok := v.(int64); ok && id != 0 {
			return id, true
		}
	}
	return 0, false
}

// actorName returns a human-readable actor for notification bodies.
func (app *App) actorName(c *gin.Context) string {
	if uid, ok := requestUserID(c); ok {
		if u, err := app.database.GetAdminUserByID(uid); err == nil && u != nil && u.Username != "" {
			return u.Username
		}
	}
	return "a user"
}

// ListNotifications returns the current user's preferences plus the
// server-wide push capabilities the frontend needs to render controls.
func (app *App) ListNotifications(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	prefs, err := app.database.GetPushPreferences(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	s := app.settings()
	c.JSON(http.StatusOK, gin.H{
		"preferences": gin.H{
			"enabled":  prefs.Enabled,
			"web_push": prefs.WebPush,
			"fcm":      prefs.FCM,
			"new_item": prefs.NewItem,
		},
		"capabilities": gin.H{
			"push_enabled":           s.PushEnabled,
			"notify_new_item":        s.NotifyNewItem,
			"web_push_enabled":       s.WebPushEnabled,
			"fcm_enabled":            s.FCMEnabled,
			"vapid_public_key":       s.WebPushVAPIDPublicKey,
			"fcm_project_id":         s.FCMProjectID,
			"fcm_android_channel_id": s.FCMAndroidChannelID,
		},
	})
}

// NotificationsStream streams realtime push events to the browser over SSE.
func (app *App) NotificationsStream(c *gin.Context) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "streaming unsupported")
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ch, unsubscribe := app.pushNotifier().Hub().Subscribe()
	defer unsubscribe()

	fmt.Fprint(c.Writer, "retry: 5000\n\n")
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Fprint(c.Writer, ": ping\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// UpdateNotificationPreferences upserts the current user's push opt-ins.
func (app *App) UpdateNotificationPreferences(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
		WebPush *bool `json:"web_push"`
		FCM     *bool `json:"fcm"`
		NewItem *bool `json:"new_item"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	prefs, err := app.database.GetPushPreferences(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	prefs.UserID = userID
	if input.Enabled != nil {
		prefs.Enabled = *input.Enabled
	}
	if input.WebPush != nil {
		prefs.WebPush = *input.WebPush
	}
	if input.FCM != nil {
		prefs.FCM = *input.FCM
	}
	if input.NewItem != nil {
		prefs.NewItem = *input.NewItem
	}
	if err := app.database.SavePushPreferences(prefs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"preferences": gin.H{
			"enabled":  prefs.Enabled,
			"web_push": prefs.WebPush,
			"fcm":      prefs.FCM,
			"new_item": prefs.NewItem,
		},
	})
}

// CreatePushSubscription registers a browser Web Push subscription for the
// current user.
func (app *App) CreatePushSubscription(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	var input struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256DH string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if input.Endpoint == "" || input.Keys.P256DH == "" || input.Keys.Auth == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint and keys.p256dh/keys.auth are required"})
		return
	}
	id, err := app.database.UpsertPushSubscription(&db.PushSubscription{
		UserID:    userID,
		Endpoint:  input.Endpoint,
		P256DH:    input.Keys.P256DH,
		Auth:      input.Keys.Auth,
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// DeletePushSubscription removes one of the current user's subscriptions.
func (app *App) DeletePushSubscription(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	var input struct {
		Endpoint string `json:"endpoint"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint is required"})
		return
	}
	if err := app.database.DeletePushSubscriptionByEndpoint(userID, input.Endpoint); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RegisterPushDevice registers an FCM token for the current user. This is the
// Android app's primary integration point.
func (app *App) RegisterPushDevice(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	var input struct {
		Token      string `json:"token"`
		Platform   string `json:"platform"`
		DeviceName string `json:"device_name"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if input.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token is required"})
		return
	}
	if input.Platform == "" {
		input.Platform = "android"
	}
	id, err := app.database.UpsertPushDevice(&db.PushDevice{
		UserID:     userID,
		Token:      input.Token,
		Platform:   input.Platform,
		DeviceName: input.DeviceName,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// DeletePushDevice removes one of the current user's devices.
func (app *App) DeletePushDevice(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device id"})
		return
	}
	if err := app.database.DeletePushDevice(userID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// TestPushNotification delivers a test message to the current user's channels.
func (app *App) TestPushNotification(c *gin.Context) {
	userID, ok := requestUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	sent, errs := app.pushNotifier().NotifyTest(c.Request.Context(), userID)
	if errs == nil {
		errs = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"sent": sent, "errors": errs})
}

// GenerateVAPIDKeysHandler creates a fresh VAPID key pair for the settings UI.
// It does not persist anything; the admin saves the values through /api/settings.
func (app *App) GenerateVAPIDKeysHandler(c *gin.Context) {
	public, private, err := push.GenerateVAPIDKeys()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"public_key": public, "private_key": private})
}
