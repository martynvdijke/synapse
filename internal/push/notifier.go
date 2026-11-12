package push

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"synapse/internal/db"
	"synapse/internal/logging"
)

const (
	// maxConcurrent bounds parallel outbound push sends.
	maxConcurrent = 8
	// sendTimeout bounds one notification fan-out.
	sendTimeout = 15 * time.Second
)

// Notifier fans a new-item event out to the realtime hub, Web Push
// subscriptions and FCM devices. All delivery is best-effort: failures are
// logged and never surfaced to the caller.
type Notifier struct {
	db         *db.DB
	settings   func() db.Settings
	hub        *Hub
	httpClient *http.Client

	fcmMu     sync.Mutex
	fcmToken  string
	fcmExpiry time.Time
}

// NewNotifier builds a notifier over the given database and a settings
// accessor. The hub is created here and shared with SSE subscribers.
func NewNotifier(database *db.DB, settings func() db.Settings) *Notifier {
	return &Notifier{
		db:         database,
		settings:   settings,
		hub:        NewHub(),
		httpClient: &http.Client{Timeout: sendTimeout},
	}
}

// Hub exposes the realtime event hub for SSE handlers.
func (n *Notifier) Hub() *Hub { return n.hub }

// attr builds a slog attribute for the string-or-number values logged here.
func attr(key string, value any) slog.Attr {
	switch v := value.(type) {
	case int64:
		return slog.Int64(key, v)
	case int:
		return slog.Int(key, v)
	default:
		return slog.String(key, fmt.Sprint(value))
	}
}

// eligible reports whether a user's preferences allow a channel. Pure helper so
// the filtering rule is unit-testable without a database.
func eligible(p db.PushPreferences, channel string) bool {
	if !p.Enabled || !p.NewItem {
		return false
	}
	switch channel {
	case "web_push":
		return p.WebPush
	case "fcm":
		return p.FCM
	default:
		return false
	}
}

func newEventID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func newItemTitle(kind string) string {
	switch kind {
	case "service link":
		return "New service link added"
	case "alert rule":
		return "New alert rule added"
	default:
		return "New item added"
	}
}

// NotifyNewItem publishes a new-item event to the realtime hub and, when
// enabled, to every eligible Web Push / FCM target. It never returns an error.
func (n *Notifier) NotifyNewItem(ctx context.Context, actor, kind, name string) {
	if n == nil {
		return
	}
	s := n.settings()
	if !s.PushEnabled || !s.NotifyNewItem {
		return
	}
	if actor == "" {
		actor = "a user"
	}
	ev := Event{
		ID:       newEventID(),
		Type:     "new_item",
		Title:    newItemTitle(kind),
		Body:     fmt.Sprintf("%s added %s “%s”", actor, kind, name),
		URL:      "/",
		Category: "new_item",
		At:       time.Now(),
	}
	if n.hub != nil {
		n.hub.Publish(ev)
	}
	if !s.WebPushEnabled && !s.FCMEnabled {
		return
	}
	if n.db == nil {
		return
	}

	payload := Payload{
		Title:    ev.Title,
		Body:     ev.Body,
		URL:      ev.URL,
		Tag:      "new-item",
		Category: ev.Category,
		Data: map[string]string{
			"type":     "new_item",
			"kind":     kind,
			"name":     name,
			"event_id": ev.ID,
		},
	}

	prefs := make(map[int64]db.PushPreferences)
	prefsFor := func(userID int64) db.PushPreferences {
		if p, ok := prefs[userID]; ok {
			return p
		}
		p, err := n.db.GetPushPreferences(userID)
		if err != nil {
			logging.LogWarn("push", "Failed to load push preferences",
				attr("user_id", userID), attr("error", err.Error()))
		}
		prefs[userID] = p
		return p
	}

	var jobs []func(context.Context) error
	if s.WebPushEnabled && s.WebPushVAPIDPublicKey != "" && s.WebPushVAPIDPrivateKey != "" {
		subs, err := n.db.ListAllPushSubscriptions()
		if err != nil {
			logging.LogWarn("push", "Failed to list push subscriptions", attr("error", err.Error()))
		}
		for _, sub := range subs {
			if !eligible(prefsFor(sub.UserID), "web_push") {
				continue
			}
			sub := sub
			jobs = append(jobs, func(ctx context.Context) error {
				err := sendWebPush(ctx, n.httpClient, s, sub, payload)
				if err == ErrSubscriptionGone {
					_ = n.db.DeletePushSubscriptionByID(sub.ID)
					return nil
				}
				if err != nil {
					return fmt.Errorf("subscription %d: %w", sub.ID, err)
				}
				return nil
			})
		}
	}
	if s.FCMEnabled && s.FCMServiceAccountJSON != "" {
		devices, err := n.db.ListAllPushDevices()
		if err != nil {
			logging.LogWarn("push", "Failed to list push devices", attr("error", err.Error()))
		}
		for _, dev := range devices {
			if !eligible(prefsFor(dev.UserID), "fcm") {
				continue
			}
			dev := dev
			jobs = append(jobs, func(ctx context.Context) error {
				err := n.sendFCM(ctx, s, dev, payload)
				if err == ErrDeviceGone {
					_ = n.db.DeletePushDeviceByToken(dev.Token)
					return nil
				}
				if err != nil {
					return fmt.Errorf("device %d: %w", dev.ID, err)
				}
				_ = n.db.TouchPushDevice(dev.Token)
				return nil
			})
		}
	}
	n.dispatch(ctx, jobs)
}

// NotifyNewItemAsync runs NotifyNewItem off the request goroutine. The request
// cancellation is detached so a completed HTTP response does not abort an
// in-flight fan-out; a 15s timeout still bounds it.
func (n *Notifier) NotifyNewItemAsync(ctx context.Context, actor, kind, name string) {
	if n == nil {
		return
	}
	base := context.WithoutCancel(ctx)
	go func() {
		ctx, cancel := context.WithTimeout(base, sendTimeout)
		defer cancel()
		n.NotifyNewItem(ctx, actor, kind, name)
	}()
}

// NotifyTest sends a test notification to one user's registered channels.
// It ignores the user's per-item toggles (an explicit test should exercise the
// transport) but only attempts channels enabled globally.
func (n *Notifier) NotifyTest(ctx context.Context, userID int64) (int, []string) {
	s := n.settings()
	payload := Payload{
		Title:    "Synapse test notification",
		Body:     "Push notifications are working.",
		URL:      "/",
		Tag:      "synapse-test",
		Category: "test",
		Data:     map[string]string{"type": "test"},
	}
	var errs []string
	var jobs []func(context.Context) error
	sent := 0

	if s.WebPushEnabled && s.WebPushVAPIDPublicKey != "" && s.WebPushVAPIDPrivateKey != "" {
		subs, err := n.db.ListPushSubscriptions(userID)
		if err != nil {
			errs = append(errs, "list subscriptions: "+err.Error())
		}
		for _, sub := range subs {
			sub := sub
			jobs = append(jobs, func(ctx context.Context) error {
				err := sendWebPush(ctx, n.httpClient, s, sub, payload)
				if err == ErrSubscriptionGone {
					_ = n.db.DeletePushSubscriptionByID(sub.ID)
				}
				return err
			})
		}
	}
	if s.FCMEnabled && s.FCMServiceAccountJSON != "" {
		devices, err := n.db.ListPushDevices(userID)
		if err != nil {
			errs = append(errs, "list devices: "+err.Error())
		}
		for _, dev := range devices {
			dev := dev
			jobs = append(jobs, func(ctx context.Context) error {
				err := n.sendFCM(ctx, s, dev, payload)
				if err == ErrDeviceGone {
					_ = n.db.DeletePushDeviceByToken(dev.Token)
				}
				return err
			})
		}
	}
	if len(jobs) == 0 {
		if !s.WebPushEnabled && !s.FCMEnabled {
			errs = append(errs, "push notifications are disabled")
		} else if s.WebPushVAPIDPublicKey == "" || s.WebPushVAPIDPrivateKey == "" {
			errs = append(errs, "web push is enabled but VAPID keys are not configured")
		}
		if len(errs) == 0 {
			errs = append(errs, "no registered subscriptions or devices")
		}
		return 0, errs
	}

	var mu sync.Mutex
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func(job func(context.Context) error) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				errs = append(errs, ctx.Err().Error())
				mu.Unlock()
				return
			}
			if err := job(ctx); err != nil {
				mu.Lock()
				errs = append(errs, err.Error())
				mu.Unlock()
				return
			}
			mu.Lock()
			sent++
			mu.Unlock()
		}(job)
	}
	wg.Wait()
	return sent, errs
}

// dispatch runs the jobs with a bounded worker pool, logging per-target errors.
func (n *Notifier) dispatch(ctx context.Context, jobs []func(context.Context) error) {
	if len(jobs) == 0 {
		return
	}
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func(job func(context.Context) error) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			if err := job(ctx); err != nil {
				logging.LogWarn("push", "Push delivery failed", attr("error", err.Error()))
			}
		}(job)
	}
	wg.Wait()
}
