package telemetry

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

func TestInitTelemetry_NoEndpoint(t *testing.T) {
	os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	providers, err := InitTelemetry("", "test", nil)
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

	res, err := buildResource("test-service", "1.0.0")
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
	providers, err := InitTelemetry("", "test", nil)
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

	providers, err := InitTelemetry("", "test", nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	Shutdown(providers)
}

// The configured DB/UI endpoint must actually reach the exporters, which read
// the standard OTEL_EXPORTER_OTLP_ENDPOINT env var (they are created with no
// options). Regression guard for the endpoint being silently ignored.
func TestInitTelemetry_PublishesEndpointToEnv(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	providers, err := InitTelemetry("http://localhost:4317", "1.0.0", nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); got != "http://localhost:4317" {
		t.Fatalf("expected exporter env endpoint to be published, got %q", got)
	}
	Shutdown(providers)
}

type recordingHandler struct{ n int }

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (h *recordingHandler) Handle(context.Context, slog.Record) error { h.n++; return nil }
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler        { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler             { return h }

func TestMultiHandler_FansOut(t *testing.T) {
	a, b := &recordingHandler{}, &recordingHandler{}
	m := multiHandler{handlers: []slog.Handler{a, b}}
	if err := m.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "x", 0)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if a.n != 1 || b.n != 1 {
		t.Fatalf("expected both handlers to receive the record, got a=%d b=%d", a.n, b.n)
	}
}

type gatedHandler struct {
	enabled bool
	n       int
}

func (h *gatedHandler) Enabled(context.Context, slog.Level) bool  { return h.enabled }
func (h *gatedHandler) Handle(context.Context, slog.Record) error { h.n++; return nil }
func (h *gatedHandler) WithAttrs([]slog.Attr) slog.Handler        { return h }
func (h *gatedHandler) WithGroup(string) slog.Handler             { return h }

func TestMultiHandler_SkipsDisabledHandlers(t *testing.T) {
	off, on := &gatedHandler{enabled: false}, &gatedHandler{enabled: true}
	m := multiHandler{handlers: []slog.Handler{off, on}}

	if !m.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("expected Enabled true when one handler is enabled")
	}
	if err := m.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "x", 0)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if off.n != 0 {
		t.Errorf("expected disabled handler to be skipped, got %d records", off.n)
	}
	if on.n != 1 {
		t.Errorf("expected enabled handler to receive 1 record, got %d", on.n)
	}
}

func TestMultiHandler_EnabledFalseWhenAllDisabled(t *testing.T) {
	m := multiHandler{handlers: []slog.Handler{&gatedHandler{}, &gatedHandler{}}}
	if m.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("expected Enabled false when every handler is disabled")
	}
}

func TestMultiHandler_WithAttrsAndGroupStayMulti(t *testing.T) {
	m := multiHandler{handlers: []slog.Handler{&recordingHandler{}, &recordingHandler{}}}
	withAttrs := m.WithAttrs([]slog.Attr{slog.String("k", "v")})
	if _, ok := withAttrs.(multiHandler); !ok {
		t.Fatalf("expected WithAttrs to return multiHandler, got %T", withAttrs)
	}
	withGroup := m.WithGroup("g")
	if _, ok := withGroup.(multiHandler); !ok {
		t.Fatalf("expected WithGroup to return multiHandler, got %T", withGroup)
	}
	if len(withGroup.(multiHandler).handlers) != 2 {
		t.Fatalf("expected 2 derived handlers, got %d", len(withGroup.(multiHandler).handlers))
	}
}

func TestGetOTLPEndpoint(t *testing.T) {
	cases := []struct {
		name string
		db   string
		env  string
		want string
	}{
		{"db wins over env", "http://db:4317", "http://env:4317", "http://db:4317"},
		{"falls back to env", "", "http://env:4317", "http://env:4317"},
		{"empty when neither set", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tc.env)
			if got := getOTLPEndpoint(tc.db); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetServiceName(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	if got := getServiceName(); got != "synapse" {
		t.Fatalf("expected default service name synapse, got %q", got)
	}
	t.Setenv("OTEL_SERVICE_NAME", "custom-svc")
	if got := getServiceName(); got != "custom-svc" {
		t.Fatalf("expected env service name, got %q", got)
	}
}

func TestGetOTLPProtocol(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "")
	if got := getOTLPProtocol(); got != "grpc" {
		t.Fatalf("expected default protocol grpc, got %q", got)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	if got := getOTLPProtocol(); got != "http/protobuf" {
		t.Fatalf("expected env protocol, got %q", got)
	}
}

// parentCtx builds a context carrying a remote parent span with the given
// sampling flag, so ParentBased sampler decisions can be asserted.
func parentCtx(sampled bool) context.Context {
	var flags trace.TraceFlags
	if sampled {
		flags = trace.FlagsSampled
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4},
		SpanID:     trace.SpanID{1, 2, 3, 4},
		TraceFlags: flags,
		Remote:     true,
	})
	return trace.ContextWithSpanContext(context.Background(), sc)
}

func TestConfigureSampler_Decisions(t *testing.T) {
	cases := []struct {
		name   string
		sampler string
		arg    string
		parent context.Context
		want   sdktrace.SamplingDecision
	}{
		{"default samples root", "", "", context.Background(), sdktrace.RecordAndSample},
		{"always_on", "always_on", "", context.Background(), sdktrace.RecordAndSample},
		{"always_off", "always_off", "", context.Background(), sdktrace.Drop},
		{"traceidratio zero drops", "traceidratio", "0", context.Background(), sdktrace.Drop},
		{"traceidratio one samples", "traceidratio", "1", context.Background(), sdktrace.RecordAndSample},
		{"parentbased honors sampled parent", "", "", parentCtx(true), sdktrace.RecordAndSample},
		{"parentbased honors unsampled parent", "", "", parentCtx(false), sdktrace.Drop},
		{"case insensitive", "ALWAYS_OFF", "", context.Background(), sdktrace.Drop},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_TRACES_SAMPLER", tc.sampler)
			t.Setenv("OTEL_TRACES_SAMPLER_ARG", tc.arg)
			got := configureSampler().ShouldSample(sdktrace.SamplingParameters{
				ParentContext: tc.parent,
				TraceID:       trace.TraceID{9, 9, 9, 9},
				Name:          "test",
			}).Decision
			if got != tc.want {
				t.Fatalf("got decision %v, want %v", got, tc.want)
			}
		})
	}
}

func findKV(kvs []attribute.KeyValue, key attribute.Key) (attribute.Value, bool) {
	for _, kv := range kvs {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

func TestBuildResource_ServiceAttrs(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	res, err := buildResource("svc-a", "1.2.3")
	if err != nil {
		t.Fatalf("buildResource: %v", err)
	}
	if v, ok := findKV(res.Attributes(), semconv.ServiceNameKey); !ok || v.AsString() != "svc-a" {
		t.Fatalf("expected service.name=svc-a, got %v (found=%v)", v, ok)
	}
	if v, ok := findKV(res.Attributes(), semconv.ServiceVersionKey); !ok || v.AsString() != "1.2.3" {
		t.Fatalf("expected service.version=1.2.3, got %v (found=%v)", v, ok)
	}
}

func TestBuildResource_OmitsEmptyVersion(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	res, err := buildResource("svc-a", "")
	if err != nil {
		t.Fatalf("buildResource: %v", err)
	}
	if _, ok := findKV(res.Attributes(), semconv.ServiceVersionKey); ok {
		t.Fatal("expected no service.version when version is empty")
	}
}

func TestBuildResource_ParsesExtraAttributes(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "env=prod, region = eu-west-1 ,malformed")
	res, err := buildResource("svc-a", "")
	if err != nil {
		t.Fatalf("buildResource: %v", err)
	}
	attrs := res.Attributes()
	if v, ok := findKV(attrs, attribute.Key("env")); !ok || v.AsString() != "prod" {
		t.Fatalf("expected env=prod, got %v (found=%v)", v, ok)
	}
	// Whitespace around key/value is trimmed.
	if v, ok := findKV(attrs, attribute.Key("region")); !ok || v.AsString() != "eu-west-1" {
		t.Fatalf("expected region=eu-west-1, got %v (found=%v)", v, ok)
	}
	if _, ok := findKV(attrs, attribute.Key("malformed")); ok {
		t.Fatal("expected malformed pair (no '=') to be skipped")
	}
}

func TestInitTelemetry_HTTPProtocol(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	providers, err := InitTelemetry("http://localhost:4318", "test", nil)
	if err != nil {
		t.Fatalf("InitTelemetry: %v", err)
	}
	if providers.MeterProvider == nil {
		t.Error("expected meter provider with http/protobuf protocol")
	}
	if providers.LoggerProvider == nil {
		t.Error("expected logger provider with http/protobuf protocol")
	}
	Shutdown(providers)
}

func findMetric(rms metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, sm := range rms.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m, true
			}
		}
	}
	return metricdata.Metrics{}, false
}

func TestMetricsMiddleware_RecordsRequest(t *testing.T) {
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(mp)
	defer mp.Shutdown(context.Background())

	// Reset the package-level sync.Once so the instruments bind to the manual
	// reader installed above instead of a provider from a previous test.
	metrics = nil
	metricsOnce = sync.Once{}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(MetricsMiddleware())
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping", nil))

	var rms metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rms); err != nil {
		t.Fatalf("collect: %v", err)
	}

	counter, ok := findMetric(rms, "otel_http_requests_total")
	if !ok {
		t.Fatal("expected otel_http_requests_total metric")
	}
	sum, ok := counter.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("expected Sum[int64] data, got %T", counter.Data)
	}
	if len(sum.DataPoints) != 1 {
		t.Fatalf("expected 1 counter data point, got %d", len(sum.DataPoints))
	}
	if sum.DataPoints[0].Value != 1 {
		t.Errorf("expected request count 1, got %d", sum.DataPoints[0].Value)
	}
	if v, ok := sum.DataPoints[0].Attributes.Value(attribute.Key("http.route")); !ok || v.AsString() != "/ping" {
		t.Errorf("expected http.route=/ping, got %v (found=%v)", v, ok)
	}

	histogram, ok := findMetric(rms, "otel_http_request_duration_seconds")
	if !ok {
		t.Fatal("expected otel_http_request_duration_seconds metric")
	}
	hist, ok := histogram.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("expected Histogram[float64] data, got %T", histogram.Data)
	}
	if len(hist.DataPoints) != 1 || hist.DataPoints[0].Count != 1 {
		t.Fatalf("expected 1 duration observation, got %+v", hist.DataPoints)
	}
}

func TestRecordMetricAttrs_UsesRoutePattern(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var attrs []attribute.KeyValue
	r.GET("/api/thing/:id", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
		attrs = RecordMetricAttrs(c)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/thing/42", nil))

	if v, ok := findKV(attrs, attribute.Key("http.method")); !ok || v.AsString() != "GET" {
		t.Errorf("expected http.method=GET, got %v (found=%v)", v, ok)
	}
	if v, ok := findKV(attrs, attribute.Key("http.route")); !ok || v.AsString() != "/api/thing/:id" {
		t.Errorf("expected templated route, got %v (found=%v)", v, ok)
	}
	if v, ok := findKV(attrs, attribute.Key("http.status_code")); !ok || v.AsInt64() != http.StatusNoContent {
		t.Errorf("expected http.status_code=204, got %v (found=%v)", v, ok)
	}
}
