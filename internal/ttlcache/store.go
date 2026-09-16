package ttlcache

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Store is a generic last-good + singleflight cache.
type Store[T any] struct {
	mu         sync.Mutex
	val        T
	has        bool
	at         time.Time
	err        error
	errAt      time.Time
	ttl        time.Duration
	retryFloor time.Duration
	sf         singleflight.Group
}

func New[T any](ttl, retryFloor time.Duration) *Store[T] {
	return &Store[T]{ttl: ttl, retryFloor: retryFloor}
}

func (s *Store[T]) Peek() (T, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.has {
		var zero T
		return zero, false, false
	}
	fresh := !s.at.IsZero() && time.Since(s.at) < s.ttl
	return s.val, true, fresh
}

func (s *Store[T]) Fetch(fn func() (T, error)) (value T, stale bool, err error) {
	// Fast path: fresh value
	if v, has, fresh := s.Peek(); has && fresh {
		return v, false, nil
	}
	// Retry floor: if last error is recent, serve stale without refetch
	s.mu.Lock()
	if s.err != nil && !s.errAt.IsZero() && time.Since(s.errAt) < s.retryFloor {
		if s.has {
			v := s.val
			e := s.err
			s.mu.Unlock()
			return v, true, e
		}
		e := s.err
		s.mu.Unlock()
		var zero T
		return zero, false, e
	}
	s.mu.Unlock()

	// The coalesced fn runs once for all concurrent callers, so it uses the
	// first caller's context for the duration of the fetch.
	// ponytail: per-caller deadlines within one in-flight fetch are ignored;
	// key the singleflight group by deadline if callers ever diverge a lot.
	_, err2, _ := s.sf.Do("fetch", func() (any, error) {
		// Re-check freshness inside singleflight
		if v, has, fresh := s.Peek(); has && fresh {
			return v, nil
		}
		// Re-check retry floor inside
		s.mu.Lock()
		if s.err != nil && !s.errAt.IsZero() && time.Since(s.errAt) < s.retryFloor {
			s.mu.Unlock()
			return nil, nil
		}
		s.mu.Unlock()

		v, e := fn()
		if e != nil && (errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded)) {
			// Caller-scoped timeout/cancel: surface it, but never poison the
			// cache or the retry floor for healthy callers.
			return nil, e
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if e != nil {
			s.err = e
			s.errAt = time.Now()
			return nil, nil
		}
		s.val = v
		s.has = true
		s.at = time.Now()
		s.err = nil
		s.errAt = time.Time{}
		return v, nil
	})

	if err2 != nil {
		// Transient caller-scoped error from the shared fetch.
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.has {
			return s.val, true, err2
		}
		var zero T
		return zero, false, err2
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.has {
		fresh := !s.at.IsZero() && time.Since(s.at) < s.ttl
		if fresh {
			return s.val, false, nil
		}
		// stale value exists
		if s.err != nil {
			return s.val, true, s.err
		}
		return s.val, false, nil
	}
	// No value
	if s.err != nil {
		var zero T
		return zero, false, s.err
	}
	var zero T
	return zero, false, nil
}

func (s *Store[T]) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	var zero T
	s.val = zero
	s.has = false
	s.at = time.Time{}
	s.err = nil
	s.errAt = time.Time{}
}

func (s *Store[T]) LastError() (error, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err, s.errAt
}

func EnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
