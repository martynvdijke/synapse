package docker

import (
	"context"
	"sync"
	"time"
)

// ImageUpdate describes a detected image change for a container name.
type ImageUpdate struct {
	ContainerID   string
	ContainerName string
	ImageOld      string // previous image ID
	ImageNew      string // current image ID
}

// WatcherOptions tunes the watcher loop. Zero values fall back to defaults.
type WatcherOptions struct {
	// ReconnectBase is the initial backoff after a stream error (default 1s).
	ReconnectBase time.Duration
	// ReconnectMax caps the backoff (default 30s).
	ReconnectMax time.Duration
	// OnEvent receives every raw docker event (may be nil).
	OnEvent func(Event)
	// OnImageUpdate fires when a container's image changes (may be nil).
	OnImageUpdate func(u ImageUpdate)
	// Since is the unix timestamp to start the stream from on the first
	// connect (0 = from now).
	Since int64
}

// Watcher tails the docker events stream, maintains an in-memory
// container-name → imageID map and detects image updates on container
// create/restart/die events. The stream is reconnected with exponential
// backoff (ReconnectBase → ReconnectMax) on errors.
type Watcher struct {
	client *Client
	opts   WatcherOptions

	mu              sync.Mutex
	imageByContainer map[string]string // container name → image ID
	connects        int
}

// NewWatcher builds a watcher for the given client.
func NewWatcher(client *Client, opts WatcherOptions) *Watcher {
	if opts.ReconnectBase <= 0 {
		opts.ReconnectBase = time.Second
	}
	if opts.ReconnectMax <= 0 {
		opts.ReconnectMax = 30 * time.Second
	}
	return &Watcher{
		client:           client,
		opts:             opts,
		imageByContainer: make(map[string]string),
	}
}

// Connects returns the number of successful stream connects (diagnostics).
func (w *Watcher) Connects() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.connects
}

// Run blocks forever (or until ctx is cancelled): connect, consume events,
// reconnect with backoff. Returns only on ctx cancellation.
func (w *Watcher) Run(ctx context.Context) {
	backoff := w.opts.ReconnectBase
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		events := make(chan Event, 256)
		// Consumer: handles events as they stream in. Exits when the stream
		// closes the channel.
		go func() {
			for ev := range events {
				w.handleEvent(ev)
			}
		}()

		// StreamEvents blocks while the stream is live and returns when the
		// stream ends or errors (closing the channel).
		err := w.client.StreamEvents(ctx, w.opts.Since, events)

		w.mu.Lock()
		w.connects++
		w.mu.Unlock()

		if ctx.Err() != nil {
			return
		}
		if err == nil {
			// Clean EOF: reconnect promptly with the base backoff.
			backoff = w.opts.ReconnectBase
		} else {
			// Connection/stream failure: exponential backoff up to the cap.
			backoff = w.nextBackoff(backoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// nextBackoff doubles a backoff, capped at ReconnectMax.
func (w *Watcher) nextBackoff(b time.Duration) time.Duration {
	b *= 2
	if b > w.opts.ReconnectMax {
		return w.opts.ReconnectMax
	}
	return b
}

// handleEvent processes a single docker event.
func (w *Watcher) handleEvent(ev Event) {
	if w.opts.OnEvent != nil {
		w.opts.OnEvent(ev)
	}

	if ev.Type != "container" {
		return
	}
	name := ev.ContainerName()
	if name == "" {
		name = ev.Actor.ID
	}

	switch ev.Action {
	case "create", "start":
		// New container (or first sighting): record its image id.
		w.updateContainerImage(context.Background(), name, ev.Actor.ID)
	case "die", "restart", "kill":
		// A container stopped or restarted: inspect to detect image drift
		// (compose recreates with a new image id under the same name).
		w.detectImageChange(context.Background(), name, ev.Actor.ID)
	}
}

// updateContainerImage records the current image id for a container without
// firing an update.
func (w *Watcher) updateContainerImage(ctx context.Context, name, id string) {
	imageID, err := w.resolveImageID(ctx, id)
	if err != nil {
		return
	}
	w.mu.Lock()
	w.imageByContainer[name] = imageID
	w.mu.Unlock()
}

// detectImageChange compares the container's current image id to the last
// recorded one and fires OnImageUpdate when it differs.
func (w *Watcher) detectImageChange(ctx context.Context, name, id string) {
	imageID, err := w.resolveImageID(ctx, id)
	if err != nil {
		return
	}

	w.mu.Lock()
	old := w.imageByContainer[name]
	w.imageByContainer[name] = imageID
	w.mu.Unlock()

	if old != "" && old != imageID && w.opts.OnImageUpdate != nil {
		w.opts.OnImageUpdate(ImageUpdate{
			ContainerID:   id,
			ContainerName: name,
			ImageOld:      old,
			ImageNew:      imageID,
		})
	}
}

// resolveImageID inspects a container and returns its image ID, falling back
// to the container's ImageConfig if needed.
func (w *Watcher) resolveImageID(ctx context.Context, id string) (string, error) {
	ins, err := w.client.InspectContainer(ctx, id)
	if err != nil {
		return "", err
	}
	if ins.Config != nil && ins.Config.Image != "" {
		return ins.Config.Image, nil
	}
	return ins.ID, nil
}
