package push

import "sync"

// Hub fans out realtime events to SSE subscribers. Publishing never blocks: a
// subscriber that cannot keep up drops the event rather than stalling the
// producer (push is best-effort by design).
type Hub struct {
	mu     sync.Mutex
	subs   map[chan Event]struct{}
	closed bool
}

// NewHub returns an empty, ready-to-use hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[chan Event]struct{})}
}

// Subscribe registers a listener. The returned unsubscribe function is safe to
// call more than once and after Close.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 8)
	h.mu.Lock()
	if h.closed {
		// A closed hub still hands out a closed channel so handlers exit.
		h.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			if _, ok := h.subs[ch]; ok {
				delete(h.subs, ch)
				close(ch)
			}
			h.mu.Unlock()
		})
	}
}

// Publish delivers ev to every subscriber without blocking.
func (h *Hub) Publish(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
			// Slow subscriber: drop this event rather than block producers.
		}
	}
}

// Close shuts the hub down and closes every subscriber channel.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for ch := range h.subs {
		close(ch)
		delete(h.subs, ch)
	}
}
