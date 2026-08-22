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

// fakeChannel is a scriptable Notifier for fan-out tests.
type fakeChannel struct {
	name    string
	enabled bool
	fail    bool
	got     []string
}

func (f *fakeChannel) Name() string    { return f.name }
func (f *fakeChannel) Enabled() bool   { return f.enabled }
func (f *fakeChannel) Send(_ context.Context, _ Category, title, message string) error {
	if f.fail {
		return errFakeSend
	}
	f.got = append(f.got, title+": "+message)
	return nil
}

var errFakeSend = &fakeError{}

type fakeError struct{}

func (*fakeError) Error() string { return "fake send failure" }

func TestEventNotifierFansOutToAllChannels(t *testing.T) {
	a := &fakeChannel{name: "ntfy", enabled: true}
	b := &fakeChannel{name: "telegram", enabled: true}
	n := NewEventNotifier([]Notifier{a, b}, EventNotifierOptions{Enabled: true})

	if !n.Notify(context.Background(), CatDockerDie, "died", "plex") {
		t.Fatal("fan-out should report delivered")
	}
	if len(a.got) != 1 || len(b.got) != 1 {
		t.Fatalf("both channels must receive the message: a=%v b=%v", a.got, b.got)
	}
}

func TestEventNotifierIsolatesFailingChannel(t *testing.T) {
	bad := &fakeChannel{name: "discord", enabled: true, fail: true}
	good := &fakeChannel{name: "webhook", enabled: true}
	n := NewEventNotifier([]Notifier{bad, good}, EventNotifierOptions{Enabled: true})

	if !n.Notify(context.Background(), CatDockerImage, "updated", "nginx") {
		t.Fatal("one failing channel must not prevent delivery elsewhere")
	}
	if len(good.got) != 1 {
		t.Fatalf("healthy channel should still receive: %v", good.got)
	}
}

func TestEventNotifierSkipsDisabledChannels(t *testing.T) {
	off := &fakeChannel{name: "ntfy", enabled: false}
	on := &fakeChannel{name: "gotify", enabled: true}
	n := NewEventNotifier([]Notifier{off, on}, EventNotifierOptions{Enabled: true})

	if !n.Notify(context.Background(), CatReconcile, "r", "drift") {
		t.Fatal("enabled channel should deliver")
	}
	if len(off.got) != 0 {
		t.Fatal("disabled channel must not be called")
	}
}

func TestEventNotifierCooldownGatesAllChannels(t *testing.T) {
	base := time.Now()
	a := &fakeChannel{name: "a", enabled: true}
	b := &fakeChannel{name: "b", enabled: true}
	n := NewEventNotifier([]Notifier{a, b}, EventNotifierOptions{Enabled: true, Cooldown: time.Minute})
	n.nowFn = func() time.Time { return base }

	n.Notify(context.Background(), CatDockerDie, "t1", "m1")
	n.Notify(context.Background(), CatDockerDie, "t2", "m2")

	if len(a.got) != 1 || len(b.got) != 1 {
		t.Fatalf("cooldown must gate the whole fan-out: a=%v b=%v", a.got, b.got)
	}
	if n.Suppressed(CatDockerDie) != 1 {
		t.Fatalf("second notify should be suppressed, got %d", n.Suppressed(CatDockerDie))
	}
}
