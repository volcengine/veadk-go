package observability

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/volcengine/veadk-go/configs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

func cleanOTLPEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"OTEL_SDK_DISABLED", "OTEL_TRACES_EXPORTER", "OTEL_SERVICE_NAME", "OTEL_RESOURCE_ATTRIBUTES"} {
		t.Setenv(key, "")
	}
	for _, suffix := range []string{"ENDPOINT", "PROTOCOL", "HEADERS", "TIMEOUT", "COMPRESSION", "INSECURE", "CERTIFICATE", "CLIENT_CERTIFICATE", "CLIENT_KEY"} {
		t.Setenv("OTEL_EXPORTER_OTLP_"+suffix, "")
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_"+suffix, "")
	}
}

func TestOTLPHTTPEndpointsAndPrecedence(t *testing.T) {
	for _, tc := range []struct{ name, basePath, tracePath, configPath, wantPath string }{
		{"base", "", "", "", "/v1/traces"},
		{"base prefix", "/collector/", "", "", "/collector/v1/traces"},
		{"base already traces still appends", "/v1/traces", "", "", "/v1/traces/v1/traces"},
		{"trace wins", "/ignored", "/custom", "/config", "/custom"},
		{"trace root", "/ignored", "/", "", "/"},
		{"config exact", "", "", "/configured", "/configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanOTLPEnv(t)
			received := make(chan *collector.ExportTraceServiceRequest, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, tc.wantPath, r.URL.Path)
				require.Equal(t, "application/x-protobuf", r.Header.Get("Content-Type"))
				require.Equal(t, "Bearer synthetic+token", r.Header.Get("Authorization"))
				require.Empty(t, r.Header.Get("Ignored"))
				b, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				req := new(collector.ExportTraceServiceRequest)
				require.NoError(t, proto.Unmarshal(b, req))
				received <- req
				w.Header().Set("Content-Type", "application/x-protobuf")
			}))
			defer srv.Close()
			cfg := &configs.OTLPConfig{Headers: map[string]string{"Ignored": "config"}}
			if tc.configPath != "" {
				cfg.Endpoint = srv.URL + tc.configPath
			}
			if tc.configPath == "" || tc.basePath != "" {
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL+tc.basePath)
			}
			if tc.tracePath != "" {
				t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", srv.URL+tc.tracePath)
			}
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "http/protobuf")
			t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Ignored=generic")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "Authorization=Bearer%20synthetic%2Btoken")
			exp, err := NewOTLPExporter(context.Background(), cfg)
			require.NoError(t, err)
			tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "otlp-test"))))
			defer func() { require.NoError(t, tp.Shutdown(context.Background())) }()
			_, span := tp.Tracer("test").Start(context.Background(), "wire-span")
			span.End()
			require.NoError(t, tp.ForceFlush(context.Background()))
			select {
			case req := <-received:
				require.Equal(t, "wire-span", req.ResourceSpans[0].ScopeSpans[0].Spans[0].Name)
				require.Equal(t, "otlp-test", req.ResourceSpans[0].Resource.Attributes[0].Value.GetStringValue())
			case <-time.After(time.Second):
				t.Fatal("collector did not receive span")
			}
		})
	}
}

type grpcTraceCollector struct {
	collector.UnimplementedTraceServiceServer
	requests chan *collector.ExportTraceServiceRequest
	auth     chan string
}

func (c *grpcTraceCollector) Export(ctx context.Context, req *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	c.auth <- strings.Join(md.Get("authorization"), "")
	c.requests <- req
	return &collector.ExportTraceServiceResponse{}, nil
}
func TestOTLPGRPC(t *testing.T) {
	cleanOTLPEnv(t)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	defer server.Stop()
	c := &grpcTraceCollector{requests: make(chan *collector.ExportTraceServiceRequest, 1), auth: make(chan string, 1)}
	collector.RegisterTraceServiceServer(server, c)
	go func() { _ = server.Serve(lis) }()
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "grpc")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://"+lis.Addr().String())
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "authorization=synthetic")
	exp, err := NewOTLPExporter(context.Background(), nil)
	require.NoError(t, err)
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	_, s := tp.Tracer("grpc-test").Start(context.Background(), "grpc-wire-span")
	s.End()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, tp.ForceFlush(ctx))
	require.Equal(t, "synthetic", <-c.auth)
	require.Equal(t, "grpc-wire-span", (<-c.requests).ResourceSpans[0].ScopeSpans[0].Spans[0].Name)
}

func TestOTLPSelectionAndDisabled(t *testing.T) {
	for _, tc := range []struct {
		name, selector, sdk string
		cfg                 *configs.OpenTelemetryConfig
		wantNil, wantErr    bool
	}{
		{name: "unconfigured", wantNil: true, wantErr: true},
		{name: "none", selector: "none", cfg: &configs.OpenTelemetryConfig{OTLP: &configs.OTLPConfig{Protocol: "bad"}, Stdout: &configs.StdoutConfig{Enable: true}}, wantNil: true, wantErr: true},
		{name: "sdk overrides invalid config", sdk: " TrUe ", selector: "bad", wantNil: true, wantErr: true},
		{name: "otlp", selector: "otlp"},
		{name: "console", selector: "console"},
		{name: "combined", selector: "otlp,console"},
		{name: "mixed none", selector: "none,otlp", wantNil: true, wantErr: true},
		{name: "unknown", selector: "secret-invalid", wantNil: true, wantErr: true},
		{name: "explicit config", cfg: &configs.OpenTelemetryConfig{OTLP: &configs.OTLPConfig{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanOTLPEnv(t)
			t.Setenv("OTEL_TRACES_EXPORTER", tc.selector)
			t.Setenv("OTEL_SDK_DISABLED", tc.sdk)
			exp, err := NewMultiExporter(context.Background(), tc.cfg)
			require.Equal(t, tc.wantErr, err != nil)
			require.Equal(t, tc.wantNil, exp == nil)
			if err != nil {
				require.NotContains(t, err.Error(), "secret-invalid")
			}
			if exp != nil {
				require.NoError(t, exp.Shutdown(context.Background()))
			}
		})
	}
}

func TestOTLPInvalidConfigurationRedacted(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"ENDPOINT", "https://user:secret@localhost"}, {"ENDPOINT", "http://localhost/?secret"},
		{"ENDPOINT", ":secret"}, {"PROTOCOL", "http/json-secret"},
		{"HEADERS", "authorization=secret%zz"}, {"HEADERS", "authorization=secret%0A"},
		{"HEADERS", "secret"}, {"TIMEOUT", "secret"}, {"TIMEOUT", "-1"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			cleanOTLPEnv(t)
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_"+tc.key, tc.value)
			exp, err := NewOTLPExporter(context.Background(), nil)
			require.Error(t, err)
			require.Nil(t, exp)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestOTLPFailureIsAsynchronousAndRedacted(t *testing.T) {
	cleanOTLPEnv(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "secret-collector-response", http.StatusBadRequest)
	}))
	defer srv.Close()
	exp, err := NewOTLPExporter(context.Background(), &configs.OTLPConfig{Endpoint: srv.URL, Timeout: 100 * time.Millisecond})
	require.NoError(t, err)
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(time.Hour)))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	_, s := tp.Tracer("test").Start(context.Background(), "business")
	s.End()
	require.Zero(t, calls.Load())
	// Direct export exposes only a stable error, never the collector response.
	err = exp.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{tracetest.SpanStub{Name: "failure"}.Snapshot()})
	require.ErrorIs(t, err, ErrOTLPExport)
	require.NotContains(t, err.Error(), "secret")
	require.ErrorIs(t, tp.ForceFlush(context.Background()), ErrOTLPExport)
	require.EqualValues(t, 2, calls.Load())
}

func TestOTLPTimeoutAndResource(t *testing.T) {
	cleanOTLPEnv(t)
	t.Setenv("OTEL_SERVICE_NAME", "service-wins")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=loses,deployment.environment.name=test")
	orig := otel.GetTracerProvider()
	defer otel.SetTracerProvider(orig)
	otel.SetTracerProvider(noop.NewTracerProvider())
	mem := tracetest.NewInMemoryExporter()
	setGlobalTracerProvider(mem)
	tp := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	defer func() { _ = tp.Shutdown(context.Background()) }()
	_, s := tp.Tracer("test").Start(context.Background(), "resource")
	s.End()
	require.NoError(t, ForceFlush(context.Background()))
	attrs := mem.GetSpans()[0].Resource.Set()
	v, ok := attrs.Value("service.name")
	require.True(t, ok)
	require.Equal(t, "service-wins", v.AsString())
	v, ok = attrs.Value("deployment.environment.name")
	require.True(t, ok)
	require.Equal(t, "test", v.AsString())
	// A caller deadline is respected even when a collector is not responding.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
	defer srv.Close()
	exp, err := NewOTLPExporter(context.Background(), &configs.OTLPConfig{Endpoint: srv.URL, Timeout: time.Second})
	require.NoError(t, err)
	defer func() { _ = exp.Shutdown(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = exp.ExportSpans(ctx, []sdktrace.ReadOnlySpan{tracetest.SpanStub{Name: "timeout"}.Snapshot()})
	require.True(t, errors.Is(err, context.DeadlineExceeded))
	require.Less(t, time.Since(start), time.Second)
}

func TestRegistryConcurrentShutdown(t *testing.T) {
	r := &TraceRegistry{shutdownChan: make(chan struct{})}
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() { defer wg.Done(); r.Shutdown() }()
	}
	wg.Wait()
	select {
	case <-r.shutdownChan:
	default:
		t.Fatal("registry not stopped")
	}
}

func TestOTLPConfigCloneAndTimeoutPrecedence(t *testing.T) {
	cleanOTLPEnv(t)
	cfg := &configs.OpenTelemetryConfig{OTLP: &configs.OTLPConfig{Headers: map[string]string{"authorization": "synthetic"}, Timeout: time.Second}}
	cloned := cfg.Clone()
	cloned.OTLP.Headers["authorization"] = "changed"
	require.Equal(t, "synthetic", cfg.OTLP.Headers["authorization"])
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "2000")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_TIMEOUT", "50")
	resolved, err := resolveOTLP(cfg.OTLP)
	require.NoError(t, err)
	require.Equal(t, 50*time.Millisecond, resolved.Timeout)
	require.Equal(t, time.Second, cfg.OTLP.Timeout)
}

func TestOTLPFailureThenRecovery(t *testing.T) {
	cleanOTLPEnv(t)
	var healthy atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			http.Error(w, "synthetic collector failure", 400)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer srv.Close()
	exp, err := NewOTLPExporter(context.Background(), &configs.OTLPConfig{Endpoint: srv.URL})
	require.NoError(t, err)
	defer func() { _ = exp.Shutdown(context.Background()) }()
	spans := []sdktrace.ReadOnlySpan{tracetest.SpanStub{Name: "recoverable"}.Snapshot()}
	require.ErrorIs(t, exp.ExportSpans(context.Background(), spans), ErrOTLPExport)
	healthy.Store(true)
	require.NoError(t, exp.ExportSpans(context.Background(), spans))
}

func TestOTLPReusesExistingProvider(t *testing.T) {
	cleanOTLPEnv(t)
	orig := otel.GetTracerProvider()
	defer otel.SetTracerProvider(orig)
	host := sdktrace.NewTracerProvider(sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "host-owned"))))
	defer func() { _ = host.Shutdown(context.Background()) }()
	otel.SetTracerProvider(host)
	t.Setenv("OTEL_SERVICE_NAME", "must-not-replace-host")
	exp := tracetest.NewInMemoryExporter()
	setGlobalTracerProvider(exp)
	require.Same(t, host, otel.GetTracerProvider())
	_, s := host.Tracer("host").Start(context.Background(), "retained")
	s.End()
	require.NoError(t, ForceFlush(context.Background()))
	value, ok := exp.GetSpans()[0].Resource.Set().Value("service.name")
	require.True(t, ok)
	require.Equal(t, "host-owned", value.AsString())
}

func TestOTLPMalformedShadowedEnvironmentIsRedacted(t *testing.T) {
	for _, tc := range []struct{ key, invalid, valid string }{
		{"ENDPOINT", "http://%secret", "http://localhost:4318"},
		{"HEADERS", "authorization=secret%zz", "authorization=synthetic"},
		{"TIMEOUT", "secret", "50"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			cleanOTLPEnv(t)
			t.Setenv("OTEL_EXPORTER_OTLP_"+tc.key, tc.invalid)
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_"+tc.key, tc.valid)
			exp, err := NewOTLPExporter(context.Background(), nil)
			require.Nil(t, exp)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}
