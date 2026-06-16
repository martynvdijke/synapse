package logging

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrorKind represents a category of error for filtering and diagnostics.
type ErrorKind string

const (
	ErrorKindAuth     ErrorKind = "auth"
	ErrorKindNetwork  ErrorKind = "network"
	ErrorKindServer   ErrorKind = "server"
	ErrorKindParse    ErrorKind = "parse"
	ErrorKindNotFound ErrorKind = "not_found"
)

// Level aliases for convenience.
const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// Entry is a single structured log entry stored in the ring buffer.
type Entry struct {
	Timestamp time.Time         `json:"timestamp"`
	Level     string            `json:"level"`
	Source    string            `json:"source"`
	Message   string            `json:"message"`
	Duration  int64             `json:"duration,omitempty"` // nanoseconds
	Error     string            `json:"error,omitempty"`
	ErrorKind string            `json:"error_kind,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// FilterParams for querying log entries.
type FilterParams struct {
	Level     string `form:"level"`
	Source    string `form:"source"`
	Search    string `form:"search"`
	ErrorKind string `form:"error_kind"`
	Limit     int    `form:"limit"`
	Offset    int    `form:"offset"`
}

// LogBuffer is a concurrency-safe ring buffer of log entries.
type LogBuffer struct {
	mu       sync.RWMutex
	entries  []Entry
	capacity int
	next     int // next write position
	count    int // total entries written (for offset calculation)
	full     bool
}

// NewLogBuffer creates a ring buffer with the given capacity.
func NewLogBuffer(capacity int) *LogBuffer {
	return &LogBuffer{
		entries:  make([]Entry, capacity),
		capacity: capacity,
	}
}

var defaultBuffer = NewLogBuffer(1000)

// ─── SSE Subscriber support ──────────────────────────────────────────────────

var (
	subscribers   []chan Entry
	subscribersMu sync.RWMutex
)

// droppedSubscriberCount tracks total dropped messages across all subscribers.
var droppedSubscriberCount atomic.Int64

// Subscribe registers a channel that receives new log entries as they are appended.
// The caller MUST call Unsubscribe with the returned channel to avoid leaks.
func Subscribe() chan Entry {
	ch := make(chan Entry, 512)
	subscribersMu.Lock()
	subscribers = append(subscribers, ch)
	subscribersMu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
func Unsubscribe(ch chan Entry) {
	subscribersMu.Lock()
	defer subscribersMu.Unlock()
	for i, c := range subscribers {
		if c == ch {
			subscribers = append(subscribers[:i], subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// broadcast sends an entry to all active subscribers. Drops slow subscribers.
func broadcast(e Entry) {
	subscribersMu.RLock()
	defer subscribersMu.RUnlock()
	for _, ch := range subscribers {
		select {
		case ch <- e:
		default:
			// Subscriber too slow, drop entry
			n := droppedSubscriberCount.Add(1)
			if n%100 == 1 {
				log.Printf("[logging] subscriber channel full, dropped %d messages", n)
			}
		}
	}
}

// DefaultBuffer returns the package-level log buffer.
func DefaultBuffer() *LogBuffer {
	return defaultBuffer
}

// Append adds an entry to the buffer and broadcasts to SSE subscribers. Safe for concurrent use.
func (b *LogBuffer) Append(e Entry) {
	b.mu.Lock()
	b.entries[b.next] = e
	b.next = (b.next + 1) % b.capacity
	b.count++
	if b.next == 0 {
		b.full = true
	}
	b.mu.Unlock()

	broadcast(e)
}

// Snapshot returns a copy of all entries from newest to oldest.
func (b *LogBuffer) Snapshot() []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.count == 0 {
		return nil
	}

	n := b.capacity
	if !b.full && b.count < b.capacity {
		n = b.count
	}

	result := make([]Entry, n)
	if b.full {
		// Copy from next to end, then from 0 to next
		part1 := b.entries[b.next:]
		part2 := b.entries[:b.next]
		copy(result[:len(part1)], part1)
		copy(result[len(part1):], part2)
	} else {
		copy(result, b.entries[:b.count])
	}

	// Reverse to newest-first
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

// Filter returns entries matching the given filter, newest-first.
func (b *LogBuffer) Filter(f FilterParams) []Entry {
	all := b.Snapshot()

	levelVal := levelFromString(f.Level)

	var result []Entry
	for _, e := range all {
		if f.Source != "" && !strings.EqualFold(e.Source, f.Source) {
			continue
		}
		if levelVal > 0 {
			eLevel := levelFromString(e.Level)
			if eLevel < levelVal {
				continue
			}
		}
		if f.Search != "" && !strings.Contains(strings.ToLower(e.Message), strings.ToLower(f.Search)) {
			continue
		}
		if f.ErrorKind != "" && !strings.EqualFold(e.ErrorKind, f.ErrorKind) {
			continue
		}
		result = append(result, e)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := max(f.Offset, 0)
	if offset >= len(result) {
		return nil
	}
	end := min(offset+limit, len(result))
	return result[offset:end]
}

// Stats returns the total number of entries written and current count in buffer.
func (b *LogBuffer) Stats() (total int, current int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.full {
		return b.count, b.capacity
	}
	return b.count, b.count
}

// levelFromString converts level string to slog.Level value.
func levelFromString(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return -1 // no filter
	}
}

// ─── slog Handler ────────────────────────────────────────────────────────────

// BufferHandler is an slog.Handler that writes to both stdout and the LogBuffer.
type BufferHandler struct {
	mu     sync.Mutex
	out    io.Writer
	buffer *LogBuffer
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

// NewBufferHandler creates a new BufferHandler.
func NewBufferHandler(out io.Writer, buffer *LogBuffer, level slog.Leveler) *BufferHandler {
	if out == nil {
		out = os.Stdout
	}
	if buffer == nil {
		buffer = defaultBuffer
	}
	if level == nil {
		level = slog.LevelInfo
	}
	return &BufferHandler{
		out:    out,
		buffer: buffer,
		level:  level,
	}
}

// Enabled reports whether the handler handles records at the given level.
func (h *BufferHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Handle formats and writes the log record.
func (h *BufferHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Extract source attribute and other metadata
	source := "app"
	metadata := make(map[string]string)
	var errStr string
	var errKind string
	var durationNs int64

	r.Attrs(func(a slog.Attr) bool {
		key := a.Key
		val := a.Value.String()
		switch key {
		case "source":
			source = val
		case "error":
			errStr = val
		case "error_kind":
			errKind = val
		case "duration":
			if d, err := time.ParseDuration(val); err == nil {
				durationNs = d.Nanoseconds()
			}
		default:
			if val != "" {
				metadata[key] = val
			}
		}
		return true
	})

	// Build text output for stdout
	timeStr := r.Time.Format("15:04:05.000")
	levelStr := r.Level.String()
	msg := r.Message

	var textLine strings.Builder
	textLine.WriteString(fmt.Sprintf("%s %-5s [%s] %s", timeStr, levelStr, source, msg))
	if errStr != "" {
		textLine.WriteString(" error=" + errStr)
	}
	if durationNs > 0 {
		textLine.WriteString(fmt.Sprintf(" duration=%s", time.Duration(durationNs)))
	}
	for k, v := range metadata {
		textLine.WriteString(fmt.Sprintf(" %s=%s", k, v))
	}
	textLine.WriteString("\n")
	fmt.Fprint(h.out, textLine.String())

	// Add to buffer
	entry := Entry{
		Timestamp: r.Time,
		Level:     r.Level.String(),
		Source:    source,
		Message:   msg,
		Duration:  durationNs,
		Error:     errStr,
		ErrorKind: errKind,
		Metadata:  metadata,
	}
	if len(metadata) == 0 {
		entry.Metadata = nil
	}

	h.buffer.Append(entry)

	return nil
}

// WithAttrs returns a new handler with the given attributes pre-attached.
func (h *BufferHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &BufferHandler{
		out:    h.out,
		buffer: h.buffer,
		level:  h.level,
		attrs:  append(h.attrs, attrs...),
		groups: h.groups,
	}
}

// WithGroup returns a new handler with the given group.
func (h *BufferHandler) WithGroup(name string) slog.Handler {
	return &BufferHandler{
		out:    h.out,
		buffer: h.buffer,
		level:  h.level,
		attrs:  h.attrs,
		groups: append(h.groups, name),
	}
}

// ─── Convenience functions ───────────────────────────────────────────────────

func logAttrs(level slog.Level, source, msg string, attrs ...slog.Attr) {
	if !slog.Default().Enabled(context.Background(), level) {
		return
	}
	if source != "" {
		attrs = append(attrs, slog.String("source", source))
	}
	slog.LogAttrs(context.Background(), level, msg, attrs...)
}

// LogDebug logs at DEBUG level with source and optional attributes.
func LogDebug(source, msg string, attrs ...slog.Attr) {
	logAttrs(slog.LevelDebug, source, msg, attrs...)
}

// LogInfo logs at INFO level with source and optional attributes.
func LogInfo(source, msg string, attrs ...slog.Attr) {
	logAttrs(slog.LevelInfo, source, msg, attrs...)
}

// LogWarn logs at WARN level with source and optional attributes.
func LogWarn(source, msg string, attrs ...slog.Attr) {
	logAttrs(slog.LevelWarn, source, msg, attrs...)
}

// LogError logs at ERROR level with source and optional attributes.
func LogError(source, msg string, attrs ...slog.Attr) {
	logAttrs(slog.LevelError, source, msg, attrs...)
}

// ─── Initialization ──────────────────────────────────────────────────────────

// Init sets up the global slog logger with the BufferHandler.
// env vars:
//
//	LOG_LEVEL: debug, info, warn, error (default: info)
//	LOG_BUFFER_SIZE: capacity of ring buffer (default: 1000)
func Init() {
	levelStr := os.Getenv("LOG_LEVEL")
	if levelStr == "" {
		levelStr = "info"
	}

	var level slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	bufSize := 1000
	if s := os.Getenv("LOG_BUFFER_SIZE"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			bufSize = n
		}
	}

	defaultBuffer = NewLogBuffer(bufSize)
	handler := NewBufferHandler(os.Stdout, defaultBuffer, level)
	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Also redirect standard log package to slog
	slog.LogAttrs(context.Background(), slog.LevelInfo, "logging initialized",
		slog.String("source", "logging"),
		slog.String("level", levelStr),
		slog.Int("buffer_size", bufSize),
	)
}

// helper to get caller info
func callerPC(skip int) uintptr {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return 0
	}
	return pc
}
