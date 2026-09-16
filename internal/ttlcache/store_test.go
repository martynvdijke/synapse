package ttlcache

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestFreshHitAvoidsRefetch(t *testing.T) {
	s := New[int](time.Hour, 5*time.Second)
	calls := 0
	v, _, err := s.Fetch(func() (int, error) { calls++; return 42, nil })
	if err != nil || v != 42 || calls != 1 {
		t.Fatalf("first fetch %v %v calls %d", v, err, calls)
	}
	v, stale, err := s.Fetch(func() (int, error) { calls++; return 99, nil })
	if err != nil || stale || v != 42 || calls != 1 {
		t.Fatalf("second fresh hit should not refetch: v=%d stale=%v err=%v calls=%d", v, stale, err, calls)
	}
	_, has, fresh := s.Peek()
	if !has || !fresh {
		t.Fatal("peek should be fresh")
	}
}

func TestSingleflightCoalesces(t *testing.T) {
	s := New[int](time.Hour, 5*time.Second)
	var calls atomic.Int32
	sig := make(chan struct{})
	fn := func() (int, error) {
		calls.Add(1)
		<-sig
		return 7, nil
	}
	done := make(chan struct{})
	go func() { s.Fetch(fn); close(done) }()
	time.Sleep(20 * time.Millisecond)
	// second caller should join
	ch := make(chan int, 1)
	go func() {
		v, _, _ := s.Fetch(fn)
		ch <- v
	}()
	time.Sleep(20 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", calls.Load())
	}
	close(sig)
	<-done
	v2 := <-ch
	if v2 != 7 {
		t.Fatalf("coalesced value %d", v2)
	}
}

func TestTTLExpiryRefetches(t *testing.T) {
	s := New[int](20*time.Millisecond, 5*time.Second)
	s.Fetch(func() (int, error) { return 1, nil })
	time.Sleep(30 * time.Millisecond)
	calls := 0
	v, _, _ := s.Fetch(func() (int, error) { calls++; return 2, nil })
	if calls != 1 || v != 2 {
		t.Fatalf("expected refetch v=%d calls=%d", v, calls)
	}
}

func TestFailureWithLastGoodReturnsStale(t *testing.T) {
	s := New[int](20*time.Millisecond, 5*time.Second)
	s.Fetch(func() (int, error) { return 10, nil })
	time.Sleep(30 * time.Millisecond)
	v, stale, err := s.Fetch(func() (int, error) { return 0, errors.New("boom") })
	if err == nil || !stale || v != 10 {
		t.Fatalf("expected stale 10 err got v=%d stale=%v err=%v", v, stale, err)
	}
}

func TestFailureNoPriorReturnsError(t *testing.T) {
	s := New[int](20*time.Millisecond, 5*time.Second)
	v, stale, err := s.Fetch(func() (int, error) { return 0, errors.New("nope") })
	if err == nil || stale || v != 0 {
		t.Fatalf("expected error no stale v=%d stale=%v err=%v", v, stale, err)
	}
}

func TestRetryFloorSuppresses(t *testing.T) {
	s := New[int](10*time.Millisecond, 200*time.Millisecond)
	s.Fetch(func() (int, error) { return 5, nil })
	time.Sleep(20 * time.Millisecond)
	s.Fetch(func() (int, error) { return 0, errors.New("fail") })
	calls := 0
	v, stale, err := s.Fetch(func() (int, error) { calls++; return 99, nil })
	if calls != 0 || !stale || v != 5 || err == nil {
		t.Fatalf("retry floor should suppress v=%d stale=%v err=%v calls=%d", v, stale, err, calls)
	}
	time.Sleep(220 * time.Millisecond)
	v, _, _ = s.Fetch(func() (int, error) { calls++; return 99, nil })
	if calls != 1 || v != 99 {
		t.Fatalf("after floor should refetch v=%d calls=%d", v, calls)
	}
}

func TestInvalidateClears(t *testing.T) {
	s := New[int](time.Hour, 5*time.Second)
	s.Fetch(func() (int, error) { return 1, nil })
	s.Fetch(func() (int, error) { return 0, errors.New("e") })
	s.Invalidate()
	_, has, _ := s.Peek()
	if has {
		t.Fatal("should have no value after invalidate")
	}
	if e, _ := s.LastError(); e != nil {
		t.Fatal("error should be cleared")
	}
	calls := 0
	v, _, err := s.Fetch(func() (int, error) { calls++; return 2, nil })
	if err != nil || v != 2 || calls != 1 {
		t.Fatalf("after invalidate fetch %d %v calls %d", v, err, calls)
	}
}

func TestEnvDuration(t *testing.T) {
	t.Setenv("TEST_DUR", "5s")
	if EnvDuration("TEST_DUR", time.Second) != 5*time.Second {
		t.Fatal("env duration parse failed")
	}
	t.Setenv("TEST_DUR", "bad")
	if EnvDuration("TEST_DUR", time.Second) != time.Second {
		t.Fatal("bad should fallback")
	}
}
