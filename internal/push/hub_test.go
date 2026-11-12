package push

import (
	"testing"
	"time"
)

func TestHubPublishSubscribe(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe()

	ev := Event{ID: "1", Type: "new_item", Title: "hi"}
	h.Publish(ev)

	select {
	case got := <-ch:
		if got.ID != ev.ID || got.Title != ev.Title {
			t.Fatalf("got %+v, want %+v", got, ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	unsub()
	// Unsubscribe is idempotent.
	unsub()
}

func TestHubUnsubscribeStopsDelivery(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe()
	unsub()

	// Channel is closed by unsubscribe, so a receive returns !ok.
	if _, ok := <-ch; ok {
		t.Fatal("expected closed channel after unsubscribe")
	}
	h.Publish(Event{ID: "dropped"})
}

func TestHubPublishNonBlockingWhenFull(t *testing.T) {
	h := NewHub()
	_, unsub := h.Subscribe() // capacity 8, never drained
	defer unsub()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			h.Publish(Event{ID: "flood"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full subscriber")
	}
}

func TestHubClose(t *testing.T) {
	h := NewHub()
	ch, _ := h.Subscribe()
	h.Close()
	if _, ok := <-ch; ok {
		t.Fatal("expected channel closed by Close")
	}
	h.Close() // idempotent

	// Subscribe after close returns an already-closed channel.
	ch2, unsub := h.Subscribe()
	defer unsub()
	if _, ok := <-ch2; ok {
		t.Fatal("expected closed channel from closed hub")
	}
}
