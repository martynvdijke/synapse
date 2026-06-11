package logging

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEntryCreation(t *testing.T) {
	now := time.Now()
	e := Entry{
		Timestamp: now,
		Level:     "INFO",
		Source:    "test",
		Message:   "hello",
		Duration:  1000000,
		Error:     "",
		Metadata:  map[string]string{"key": "val"},
	}
	if e.Level != "INFO" {
		t.Errorf("expected INFO, got %s", e.Level)
	}
	if e.Message != "hello" {
		t.Errorf("expected hello, got %s", e.Message)
	}
	if e.Metadata["key"] != "val" {
		t.Errorf("expected val, got %s", e.Metadata["key"])
	}
}

func TestNewLogBuffer(t *testing.T) {
	b := NewLogBuffer(50)
	if b.capacity != 50 {
		t.Errorf("expected capacity 50, got %d", b.capacity)
	}
	if len(b.entries) != 50 {
		t.Errorf("expected entries len 50, got %d", len(b.entries))
	}
}

func TestLogBufferAppendAndSnapshot(t *testing.T) {
	b := NewLogBuffer(10)
	for i := 0; i < 5; i++ {
		b.Append(Entry{Level: "INFO", Message: "msg"})
	}
	snap := b.Snapshot()
	if len(snap) != 5 {
		t.Errorf("expected 5 entries, got %d", len(snap))
	}
	// Newest first
	if snap[0].Message != "msg" {
		t.Errorf("expected newest msg first, got %s", snap[0].Message)
	}
}

func TestLogBufferSnapshotEmpty(t *testing.T) {
	b := NewLogBuffer(10)
	snap := b.Snapshot()
	if snap != nil {
		t.Errorf("expected nil for empty buffer, got %v", snap)
	}
}

func TestLogBufferRingOverwrite(t *testing.T) {
	b := NewLogBuffer(3)
	for i := 0; i < 5; i++ {
		b.Append(Entry{Level: "INFO", Message: strings.ToUpper(string(rune('a' + i)))})
	}
	snap := b.Snapshot()
	if len(snap) != 3 {
		t.Errorf("expected 3 entries (capacity), got %d", len(snap))
	}
	// Oldest surviving entry is "C" (index 2), newest is "E" (index 4)
	if snap[0].Message != "E" {
		t.Errorf("expected newest E, got %s", snap[0].Message)
	}
	if snap[2].Message != "C" {
		t.Errorf("expected oldest C, got %s", snap[2].Message)
	}
}

func TestLogBufferConcurrentSafe(t *testing.T) {
	b := NewLogBuffer(100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			b.Append(Entry{Level: "INFO", Message: "test"})
		}(i)
	}
	wg.Wait()
	snap := b.Snapshot()
	if len(snap) != 50 {
		t.Errorf("expected 50 entries, got %d", len(snap))
	}
}

// --- Filter tests ---

func TestFilterNoParams(t *testing.T) {
	b := NewLogBuffer(50)
	for i := 0; i < 10; i++ {
		b.Append(Entry{Level: "INFO", Source: "app", Message: "msg"})
	}
	entries := b.Filter(FilterParams{Limit: 200})
	if len(entries) != 10 {
		t.Errorf("expected 10, got %d", len(entries))
	}
}

func TestFilterBySource(t *testing.T) {
	b := NewLogBuffer(50)
	b.Append(Entry{Level: "INFO", Source: "kuma", Message: "kuma msg"})
	b.Append(Entry{Level: "INFO", Source: "npm", Message: "npm msg"})
	b.Append(Entry{Level: "INFO", Source: "kuma", Message: "kuma msg 2"})

	entries := b.Filter(FilterParams{Source: "kuma", Limit: 200})
	if len(entries) != 2 {
		t.Errorf("expected 2 kuma entries, got %d", len(entries))
	}
}

func TestFilterBySourceCaseInsensitive(t *testing.T) {
	b := NewLogBuffer(10)
	b.Append(Entry{Level: "INFO", Source: "Kuma", Message: "test"})
	entries := b.Filter(FilterParams{Source: "kuma", Limit: 200})
	if len(entries) != 1 {
		t.Errorf("expected 1, got %d", len(entries))
	}
}

func TestFilterByLevel(t *testing.T) {
	b := NewLogBuffer(50)
	b.Append(Entry{Level: "DEBUG", Source: "app", Message: "debug"})
	b.Append(Entry{Level: "INFO", Source: "app", Message: "info"})
	b.Append(Entry{Level: "WARN", Source: "app", Message: "warn"})
	b.Append(Entry{Level: "ERROR", Source: "app", Message: "error"})

	// Warn level shows warn + error
	entries := b.Filter(FilterParams{Level: "warn", Limit: 200})
	if len(entries) != 2 {
		t.Errorf("expected 2 (warn+error), got %d", len(entries))
	}
	if entries[0].Level != "ERROR" && entries[1].Level != "WARN" {
		t.Errorf("expected ERROR and WARN entries")
	}
}

func TestFilterByLevelDebug(t *testing.T) {
	b := NewLogBuffer(50)
	b.Append(Entry{Level: "DEBUG", Source: "app", Message: "debug"})
	b.Append(Entry{Level: "INFO", Source: "app", Message: "info"})
	entries := b.Filter(FilterParams{Level: "debug", Limit: 200})
	if len(entries) != 2 {
		t.Errorf("expected 2 (all), got %d", len(entries))
	}
}

func TestFilterBySearch(t *testing.T) {
	b := NewLogBuffer(50)
	b.Append(Entry{Level: "INFO", Source: "app", Message: "login successful"})
	b.Append(Entry{Level: "INFO", Source: "app", Message: "logout"})
	b.Append(Entry{Level: "ERROR", Source: "app", Message: "login failed"})

	entries := b.Filter(FilterParams{Search: "login", Limit: 200})
	if len(entries) != 2 {
		t.Errorf("expected 2 login entries, got %d", len(entries))
	}
}

func TestFilterBySearchCaseInsensitive(t *testing.T) {
	b := NewLogBuffer(10)
	b.Append(Entry{Level: "INFO", Source: "app", Message: "Login Successful"})
	entries := b.Filter(FilterParams{Search: "successful", Limit: 200})
	if len(entries) != 1 {
		t.Errorf("expected 1, got %d", len(entries))
	}
}

func TestFilterLimit(t *testing.T) {
	b := NewLogBuffer(50)
	for i := 0; i < 20; i++ {
		b.Append(Entry{Level: "INFO", Source: "app", Message: "msg"})
	}
	entries := b.Filter(FilterParams{Limit: 5})
	if len(entries) != 5 {
		t.Errorf("expected 5, got %d", len(entries))
	}
}

func TestFilterOffset(t *testing.T) {
	b := NewLogBuffer(50)
	for i := 0; i < 10; i++ {
		b.Append(Entry{Level: "INFO", Source: "app", Message: "msg"})
	}
	entries := b.Filter(FilterParams{Limit: 200, Offset: 8})
	if len(entries) != 2 {
		t.Errorf("expected 2 (offset 8 of 10), got %d", len(entries))
	}
}

func TestFilterOffsetBeyondRange(t *testing.T) {
	b := NewLogBuffer(10)
	for i := 0; i < 5; i++ {
		b.Append(Entry{Level: "INFO", Source: "app", Message: "msg"})
	}
	entries := b.Filter(FilterParams{Limit: 200, Offset: 10})
	if entries != nil {
		t.Errorf("expected nil for offset beyond range")
	}
}

func TestFilterMaxLimit(t *testing.T) {
	b := NewLogBuffer(1200)
	for i := 0; i < 1200; i++ {
		b.Append(Entry{Level: "INFO", Source: "app", Message: "msg"})
	}
	entries := b.Filter(FilterParams{Limit: 5000}) // should cap at 1000
	if len(entries) > 1000 {
		t.Errorf("expected at most 1000 entries, got %d", len(entries))
	}
}

// --- Stats tests ---

func TestStats(t *testing.T) {
	b := NewLogBuffer(10)
	total, current := b.Stats()
	if total != 0 || current != 0 {
		t.Errorf("expected (0,0), got (%d,%d)", total, current)
	}
	for i := 0; i < 5; i++ {
		b.Append(Entry{Level: "INFO", Message: "msg"})
	}
	total, current = b.Stats()
	if total != 5 || current != 5 {
		t.Errorf("expected (5,5), got (%d,%d)", total, current)
	}
	// Fill past capacity
	for i := 0; i < 10; i++ {
		b.Append(Entry{Level: "INFO", Message: "msg"})
	}
	total, current = b.Stats()
	if total != 15 || current != 10 {
		t.Errorf("expected (15,10), got (%d,%d)", total, current)
	}
}

// --- BufferHandler tests ---

func TestBufferHandlerEnabled(t *testing.T) {
	h := NewBufferHandler(nil, NewLogBuffer(10), slog.LevelInfo)
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected enabled for INFO")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("expected enabled for ERROR")
	}
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected disabled for DEBUG")
	}
}

func TestBufferHandlerHandle(t *testing.T) {
	buf := NewLogBuffer(10)
	var out bytes.Buffer
	h := NewBufferHandler(&out, buf, slog.LevelInfo)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	r.AddAttrs(slog.String("source", "test"), slog.String("key", "val"))
	h.Handle(context.Background(), r)

	// Check buffer
	snap := buf.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry in buffer, got %d", len(snap))
	}
	if snap[0].Source != "test" {
		t.Errorf("expected source 'test', got %s", snap[0].Source)
	}
	if snap[0].Message != "test message" {
		t.Errorf("expected 'test message', got %s", snap[0].Message)
	}

	// Check stdout output
	if !strings.Contains(out.String(), "test message") {
		t.Errorf("stdout should contain 'test message', got: %s", out.String())
	}
	if !strings.Contains(out.String(), "[test]") {
		t.Errorf("stdout should contain '[test]', got: %s", out.String())
	}
	if !strings.Contains(out.String(), "key=val") {
		t.Errorf("stdout should contain 'key=val', got: %s", out.String())
	}
}

func TestBufferHandlerHandleWithError(t *testing.T) {
	buf := NewLogBuffer(10)
	var out bytes.Buffer
	h := NewBufferHandler(&out, buf, slog.LevelInfo)

	r := slog.NewRecord(time.Now(), slog.LevelError, "request failed", 0)
	r.AddAttrs(slog.String("source", "kuma"), slog.String("error", "timeout"))
	h.Handle(context.Background(), r)

	snap := buf.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	if snap[0].Level != "ERROR" {
		t.Errorf("expected ERROR level, got %s", snap[0].Level)
	}
	if snap[0].Error != "timeout" {
		t.Errorf("expected error 'timeout', got %s", snap[0].Error)
	}
	if !strings.Contains(out.String(), "error=timeout") {
		t.Errorf("stdout should contain error=timeout")
	}
}

func TestBufferHandlerHandleWithDuration(t *testing.T) {
	buf := NewLogBuffer(10)
	h := NewBufferHandler(nil, buf, slog.LevelInfo)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "completed", 0)
	r.AddAttrs(slog.String("duration", "1.5s"))
	h.Handle(context.Background(), r)

	snap := buf.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	if snap[0].Duration != 1500000000 {
		t.Errorf("expected 1500000000 ns, got %d", snap[0].Duration)
	}
}

func TestBufferHandlerWithAttrs(t *testing.T) {
	buf := NewLogBuffer(10)
	h := NewBufferHandler(nil, buf, slog.LevelInfo)
	h2 := h.WithAttrs([]slog.Attr{slog.String("source", "preset")})

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
	h2.Handle(context.Background(), r)

	snap := buf.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
}

func TestBufferHandlerWithGroup(t *testing.T) {
	buf := NewLogBuffer(10)
	h := NewBufferHandler(nil, buf, slog.LevelInfo)
	h2 := h.WithGroup("group1")

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0)
	h2.Handle(context.Background(), r)

	snap := buf.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
}

// --- Subscribe/Unsubscribe tests ---

func TestSubscribeAndUnsubscribe(t *testing.T) {
	buf := NewLogBuffer(10)
	// Use default buffer for subscribe (subscribers use the package-level broadcast)
	oldDefault := defaultBuffer
	defaultBuffer = buf
	defer func() { defaultBuffer = oldDefault }()

	ch := Subscribe()
	defer Unsubscribe(ch)

	e := Entry{Level: "INFO", Source: "test", Message: "sse test"}
	buf.Append(e)

	select {
	case received := <-ch:
		if received.Message != "sse test" {
			t.Errorf("expected 'sse test', got %s", received.Message)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for SSE entry")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	buf := NewLogBuffer(10)
	oldDefault := defaultBuffer
	defaultBuffer = buf
	defer func() { defaultBuffer = oldDefault }()

	ch1 := Subscribe()
	defer Unsubscribe(ch1)
	ch2 := Subscribe()
	defer Unsubscribe(ch2)

	e := Entry{Level: "INFO", Source: "test", Message: "multi"}
	buf.Append(e)

	for i, ch := range []chan Entry{ch1, ch2} {
		select {
		case received := <-ch:
			if received.Message != "multi" {
				t.Errorf("subscriber %d: expected 'multi', got %s", i, received.Message)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("subscriber %d timed out", i)
		}
	}
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	buf := NewLogBuffer(10)
	oldDefault := defaultBuffer
	defaultBuffer = buf
	defer func() { defaultBuffer = oldDefault }()

	ch := Subscribe()
	Unsubscribe(ch)

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after Unsubscribe")
	}
}

func TestSlowSubscriberDropped(t *testing.T) {
	buf := NewLogBuffer(10)
	oldDefault := defaultBuffer
	defaultBuffer = buf
	defer func() { defaultBuffer = oldDefault }()

	ch := Subscribe()
	defer Unsubscribe(ch)

	// Fill the subscriber's channel buffer (size 256)
	// Then verify it doesn't block the broadcaster
	for i := 0; i < 300; i++ {
		buf.Append(Entry{Level: "INFO", Source: "test", Message: "flood"})
	}

	// Drain what's available (some may be dropped)
	drained := 0
	for {
		select {
		case <-ch:
			drained++
		default:
			goto done
		}
	}
done:
	if drained > 256 {
		t.Errorf("expected at most 256 entries before buffer drop, drained %d", drained)
	}
}

// --- Convenience functions ---

func TestConvenienceFunctions(t *testing.T) {
	buf := NewLogBuffer(10)
	oldDefault := defaultBuffer
	defaultBuffer = buf
	defer func() { defaultBuffer = oldDefault }()

	h := NewBufferHandler(nil, buf, slog.LevelDebug)
	slog.SetDefault(slog.New(h))
	defer slog.SetDefault(slog.New(NewBufferHandler(nil, NewLogBuffer(10), slog.LevelInfo)))

	LogDebug("test", "debug msg")
	LogInfo("test", "info msg")
	LogWarn("test", "warn msg")
	LogError("test", "error msg")

	snap := buf.Snapshot()
	levels := make(map[string]bool)
	for _, e := range snap {
		levels[e.Level] = true
	}

	if !levels["DEBUG"] {
		t.Error("expected DEBUG entry")
	}
	if !levels["INFO"] {
		t.Error("expected INFO entry")
	}
	if !levels["WARN"] {
		t.Error("expected WARN entry")
	}
	if !levels["ERROR"] {
		t.Error("expected ERROR entry")
	}
}

func TestConvenienceFunctionsWithAttrs(t *testing.T) {
	buf := NewLogBuffer(10)
	oldDefault := defaultBuffer
	defaultBuffer = buf
	defer func() { defaultBuffer = oldDefault }()

	h := NewBufferHandler(nil, buf, slog.LevelDebug)
	slog.SetDefault(slog.New(h))
	defer slog.SetDefault(slog.New(NewBufferHandler(nil, NewLogBuffer(10), slog.LevelInfo)))

	LogInfo("test", "msg", slog.String("key1", "val1"), slog.Int("key2", 42))

	snap := buf.Snapshot()
	if len(snap) < 1 {
		t.Fatal("expected at least 1 entry")
	}
	if snap[0].Source != "test" {
		t.Errorf("expected source 'test', got %s", snap[0].Source)
	}
}

// --- Init function ---

func TestInit(t *testing.T) {
	// Save and restore env
	oldLevel := os.Getenv("LOG_LEVEL")
	oldSize := os.Getenv("LOG_BUFFER_SIZE")
	defer func() {
		os.Setenv("LOG_LEVEL", oldLevel)
		os.Setenv("LOG_BUFFER_SIZE", oldSize)
	}()

	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("LOG_BUFFER_SIZE", "500")

	Init()

	if defaultBuffer.capacity != 500 {
		t.Errorf("expected buffer capacity 500, got %d", defaultBuffer.capacity)
	}

	// Verify slog is configured (writing a log should not panic)
	LogInfo("test", "init test")
}

// --- DefaultBuffer ---

func TestDefaultBuffer(t *testing.T) {
	b := DefaultBuffer()
	if b == nil {
		t.Error("DefaultBuffer() returned nil")
	}
}
