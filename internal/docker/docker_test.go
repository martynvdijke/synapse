package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeEngine is an httptest server mimicking the subset of the Docker Engine
// API the client uses.
type fakeEngine struct {
	srv     *httptest.Server
	eventCh chan string // raw JSON event lines streamed to /events
	closed  chan struct{}
}

func newFakeEngine(t *testing.T) *fakeEngine {
	t.Helper()
	f := &fakeEngine{
		eventCh: make(chan string, 256),
		closed:  make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]ContainerSummary{
			{ID: "c1", Names: []string{"/web"}, Image: "nginx:latest", ImageID: "sha256:aaa", State: "running"},
		})
	})
	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/containers/"):]
		id = id[:len(id)-len("/json")]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ContainerInspect{
			ID:   id,
			Name: "/" + id,
			Config: &struct {
				Image        string            `json:"Image"`
				Labels       map[string]string `json:"Labels"`
				Healthcheck  *struct {
					Test []string `json:"Test"`
				} `json:"Healthcheck"`
				ExposedPorts map[string]struct{} `json:"ExposedPorts"`
			}{Image: "image-" + id},
		})
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Error("no flusher")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Stream queued events as they arrive until the test closes.
		for {
			select {
			case line := <-f.eventCh:
				fmt.Fprintln(w, line)
				fl.Flush()
			case <-f.closed:
				return
			}
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(func() {
		close(f.closed)
		f.srv.Close()
	})
	return f
}

func (f *fakeEngine) queueEvent(ev Event) {
	b, _ := json.Marshal(ev)
	f.eventCh <- string(b)
}

func testClient(f *fakeEngine) *Client {
	return NewWithClient(f.srv.URL, &http.Client{})
}

func TestPing(t *testing.T) {
	f := newFakeEngine(t)
	c := testClient(f)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestPingFailsWhenUnreachable(t *testing.T) {
	c, err := New("/nonexistent/docker.sock")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := c.Ping(ctx); err == nil {
		t.Fatal("expected ping error for missing socket")
	}
}

func TestListContainers(t *testing.T) {
	f := newFakeEngine(t)
	c := testClient(f)
	containers, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(containers) != 1 || containers[0].ID != "c1" {
		t.Fatalf("unexpected containers: %+v", containers)
	}
}

func TestInspectContainer(t *testing.T) {
	f := newFakeEngine(t)
	c := testClient(f)
	ins, err := c.InspectContainer(context.Background(), "web")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if ins.Config == nil || ins.Config.Image != "image-web" {
		t.Fatalf("unexpected inspect: %+v", ins)
	}
}

func TestStreamEvents(t *testing.T) {
	f := newFakeEngine(t)
	f.queueEvent(Event{Type: "container", Action: "start", Actor: EventActor{ID: "c1", Attributes: map[string]string{"name": "web"}}})
	f.queueEvent(Event{Type: "container", Action: "die", Actor: EventActor{ID: "c1", Attributes: map[string]string{"name": "web"}}})

	c := testClient(f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan Event, 10)
	done := make(chan error, 1)
	go func() { done <- c.StreamEvents(ctx, 0, got) }()

	first := <-got
	if first.Action != "start" || first.ContainerName() != "web" {
		t.Fatalf("unexpected first event: %+v", first)
	}
	second := <-got
	if second.Action != "die" {
		t.Fatalf("unexpected second event: %+v", second)
	}
	cancel()
	<-done
}

func TestWatcherDetectsImageUpdate(t *testing.T) {
	f := newFakeEngine(t)
	c := testClient(f)

	var updates atomic.Int32
	var mu sync.Mutex
	var got []ImageUpdate
	w := NewWatcher(c, WatcherOptions{
		OnImageUpdate: func(u ImageUpdate) {
			mu.Lock()
			got = append(got, u)
			mu.Unlock()
			updates.Add(1)
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	// Simulate container recreation under the same name with a new image:
	// first a create (records image-image-c1), then die of a container whose
	// inspect resolves to the same name but a different image.
	// Our fake engine maps inspect id -> Image: "image-"+id, so we use
	// distinct container ids c1 (old) and c2 (new).
	f.queueEvent(Event{Type: "container", Action: "create", Actor: EventActor{ID: "c1", Attributes: map[string]string{"name": "web"}}})
	time.Sleep(200 * time.Millisecond) // let the watcher record image for c1

	f.queueEvent(Event{Type: "container", Action: "die", Actor: EventActor{ID: "c2", Attributes: map[string]string{"name": "web"}}})

	deadline := time.Now().Add(3 * time.Second)
	for updates.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if updates.Load() == 0 {
		t.Fatal("expected an image update notification")
	}
	mu.Lock()
	defer mu.Unlock()
	if got[0].ContainerName != "web" {
		t.Fatalf("unexpected update: %+v", got[0])
	}
	if got[0].ImageOld != "image-c1" || got[0].ImageNew != "image-c2" {
		t.Fatalf("unexpected image ids: %+v", got[0])
	}
}

func TestWatcherReconnectsAfterStreamError(t *testing.T) {
	// A server whose /events always fails: the watcher must retry with
	// backoff and accumulate connects.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/events" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := NewWithClient(srv.URL, &http.Client{})
	w := NewWatcher(c, WatcherOptions{
		ReconnectBase: 10 * time.Millisecond,
		ReconnectMax:  50 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go w.Run(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for w.Connects() < 3 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if w.Connects() < 3 {
		t.Fatalf("expected at least 3 connects after reconnect, got %d", w.Connects())
	}
}
