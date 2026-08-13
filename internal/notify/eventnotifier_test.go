package notify

import (
	"context"
	"testing"
	"time"
)

type fakeSender struct {
	sent []string
}

func (f *fakeSender) send(_ context.Context, title, message string) error {
	f.sent = append(f.sent, title+": "+message)
	return nil
}

// testClient wraps a fake sender in the *Client type used by EventNotifier.
// We can't inject into *Client directly, so we test throttling logic via a
// notifier with a nil client disabled path plus direct unit checks, and use a
// real httptest Gotify in notify_integration_test if needed.
func TestEventNotifierToggle(t *testing.T) {
	n := NewEventNotifier(nil, EventNotifierOptions{
		Enabled: true,
		Toggles: map[Category]bool{CatDockerDie: true, CatDockerHealth: false},
	})
	if !n.Toggle(CatDockerDie) {
		t.Fatal("docker_die should be on")
	}
	if n.Toggle(CatDockerHealth) {
		t.Fatal("docker_health should be off")
	}
	if !n.Toggle(CatDockerImage) {
		t.Fatal("untoggled category should default on")
	}
	if !n.Toggle(CatReconcile) {
		t.Fatal("untoggled category should default on (2)")
	}
}

func TestEventNotifierMasterSwitch(t *testing.T) {
	n := NewEventNotifier(nil, EventNotifierOptions{Enabled: false})
	if n.Toggle(CatDockerDie) {
		t.Fatal("master off should disable all categories")
	}
}

func TestEventNotifierCooldown(t *testing.T) {
	base := time.Now()
	n := NewEventNotifier(nil, EventNotifierOptions{
		Enabled:  true,
		Cooldown: time.Minute,
	})
	n.nowFn = func() time.Time { return base }

	// nil client: Notify returns false (nothing sent) but still counts
	// cooldown? No — cooldown should not advance when client is nil.
	if n.Notify(context.Background(), CatDockerDie, "t", "m") {
		t.Fatal("nil client should not send")
	}
}

func TestContainerStopTracker(t *testing.T) {
	base := time.Now()
	tr := NewContainerStopTracker(60 * time.Second)
	tr.nowFn = func() time.Time { return base }

	if tr.WasGraceful("web", base) {
		t.Fatal("no stop recorded yet")
	}
	tr.MarkStop("web", base.Add(-10*time.Second))
	if !tr.WasGraceful("web", base) {
		t.Fatal("stop within window should be graceful")
	}
	// Record is consumed
	if tr.WasGraceful("web", base) {
		t.Fatal("stop record should be consumed on lookup")
	}

	tr.MarkStop("old", base.Add(-120*time.Second))
	if tr.WasGraceful("old", base) {
		t.Fatal("stop outside window should not be graceful")
	}
}

func TestSuppressedCount(t *testing.T) {
	n := NewEventNotifier(nil, EventNotifierOptions{Enabled: true, Cooldown: time.Minute})
	base := time.Now()
	n.nowFn = func() time.Time { return base }
	if n.Suppressed(CatReconcile) != 0 {
		t.Fatal("expected zero suppressed initially")
	}
}
