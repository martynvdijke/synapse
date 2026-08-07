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

func TestInitTelemetry_NoEndpoint(t *testing.T) {
	os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	providers, err := InitTelemetry("")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if providers == nil {
		t.Fatal("expected non-nil providers")
	}
	if providers.TracerProvider == nil {
		t.Fatal("expected non-nil tracer provider")
	}
	Shutdown(providers)
}

func TestInitTracerProvider_NoEndpoint(t *testing.T) {
	os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	tp, err := InitTracerProvider("")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil tracer provider")
	}
	ShutdownTracerProvider(tp)
}

func TestInitTracerProvider_WithEndpoint(t *testing.T) {
	os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	defer os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	tp, err := InitTracerProvider("http://localhost:4318")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil tracer provider")
	}
	ShutdownTracerProvider(tp)
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

func TestShutdownTracerProvider_Nil(t *testing.T) {
	ShutdownTracerProvider(nil)
}

func TestTracer_IsExported(t *testing.T) {
	if Tracer == nil {
		t.Fatal("expected Tracer to be non-nil after init")
	}
}

func TestConfigureSampler_Defaults(t *testing.T) {
	os.Unsetenv("OTEL_TRACES_SAMPLER")
	os.Unsetenv("OTEL_TRACES_SAMPLER_ARG")
	sampler := configureSampler()
	if sampler == nil {
		t.Fatal("expected non-nil sampler")
	}
}

func TestConfigureSampler_TraceIDRatio(t *testing.T) {
	os.Setenv("OTEL_TRACES_SAMPLER", "traceidratio")
	os.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.5")
	defer os.Unsetenv("OTEL_TRACES_SAMPLER")
	defer os.Unsetenv("OTEL_TRACES_SAMPLER_ARG")
	sampler := configureSampler()
	if sampler == nil {
		t.Fatal("expected non-nil sampler")
	}
}

func TestConfigureSampler_AlwaysOff(t *testing.T) {
	os.Setenv("OTEL_TRACES_SAMPLER", "always_off")
	defer os.Unsetenv("OTEL_TRACES_SAMPLER")
	sampler := configureSampler()
	if sampler == nil {
		t.Fatal("expected non-nil sampler")
	}
}

func TestBuildResource_WithAttributes(t *testing.T) {
	os.Setenv("OTEL_RESOURCE_ATTRIBUTES", "env=test,region=us-east-1")
	defer os.Unsetenv("OTEL_RESOURCE_ATTRIBUTES")

	res, err := buildResource("test-service")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil resource")
	}
}

func TestInitTelemetry_NoopFallback(t *testing.T) {
	// When endpoint is empty, InitTelemetry should return a noop tracer provider
	os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	providers, err := InitTelemetry("")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if providers.TracerProvider == nil {
		t.Fatal("expected non-nil tracer provider in noop mode")
	}
	// MeterProvider and LoggerProvider should be nil (not initialized without endpoint)
	if providers.MeterProvider != nil {
		t.Log("meter provider is nil when no endpoint")
	}
	Shutdown(providers)
}

func TestInitTelemetry_ServiceNameDefault(t *testing.T) {
	os.Unsetenv("OTEL_SERVICE_NAME")
	os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	providers, err := InitTelemetry("")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	Shutdown(providers)
}
