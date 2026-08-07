package db

import (
	"context"
	"errors"
	"testing"
)

func TestTraceDBQuery_Success(t *testing.T) {
	err := TraceDBQuery(context.Background(), "test-query", func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestTraceDBQuery_Error(t *testing.T) {
	expected := errors.New("db error")
	err := TraceDBQuery(context.Background(), "test-query", func(ctx context.Context) error {
		return expected
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expected) {
		t.Errorf("expected %v, got %v", expected, err)
	}
}

func TestTraceDBQuery_PropagatesContext(t *testing.T) {
	type ctxKey string
	const key ctxKey = "test"

	ctx := context.WithValue(context.Background(), key, "value")
	err := TraceDBQuery(ctx, "test-query", func(innerCtx context.Context) error {
		if v := innerCtx.Value(key); v == nil {
			return errors.New("context was not propagated")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}
