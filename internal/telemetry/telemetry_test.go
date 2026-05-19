package telemetry

import (
	"context"
	"os"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestInitTracerProvider_NoEndpoint(t *testing.T) {
	os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	tp, err := InitTracerProvider()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil tracer provider")
	}
	Shutdown(tp)
}

func TestInitTracerProvider_WithEndpoint(t *testing.T) {
	os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	defer os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	tp, err := InitTracerProvider()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil tracer provider")
	}
	Shutdown(tp)
}

func TestTracerProvider_ExportsSpans(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span",
		trace.WithAttributes(attribute.String("key", "value")),
	)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name != "test-span" {
		t.Errorf("expected span name 'test-span', got '%s'", spans[0].Name)
	}
}

func TestShutdown_Nil(t *testing.T) {
	Shutdown(nil)
}

func TestTracer_IsExported(t *testing.T) {
	if Tracer == nil {
		t.Fatal("expected Tracer to be non-nil after init")
	}
}
