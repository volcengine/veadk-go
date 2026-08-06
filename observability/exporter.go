// Copyright (c) 2025 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/integrations/ve_tls"
	"github.com/volcengine/veadk-go/log"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	olog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
)

const (
	// OTELExporterOTLPProtocolEnvKey is the environment variable key for OTLP protocol.
	OTELExporterOTLPProtocolEnvKey = "OTEL_EXPORTER_OTLP_PROTOCOL"
	// OTELExporterOTLPEndpointEnvKey is the environment variable key for OTLP endpoint.
	OTELExporterOTLPEndpointEnvKey = "OTEL_EXPORTER_OTLP_ENDPOINT"
)

var (
	fileWriters sync.Map
)

func createLogClient(ctx context.Context, url, protocol string, headers map[string]string) (olog.Exporter, error) {
	if protocol == "" {
		protocol = os.Getenv(OTELExporterOTLPProtocolEnvKey)
	}

	if url == "" {
		return nil, errors.New("OTEL_EXPORTER_OTLP_ENDPOINT is not set")
	}

	switch {
	case strings.HasPrefix(protocol, "http"):
		return otlploghttp.New(ctx, otlploghttp.WithEndpointURL(url), otlploghttp.WithHeaders(headers))
	default:
		return otlploggrpc.New(ctx, otlploggrpc.WithEndpointURL(url), otlploggrpc.WithHeaders(headers))
	}
}

func createTraceClient(ctx context.Context, url, protocol string, headers map[string]string) (trace.SpanExporter, error) {
	if protocol == "" {
		protocol = os.Getenv(OTELExporterOTLPProtocolEnvKey)
	}

	if url == "" {
		url = os.Getenv(OTELExporterOTLPEndpointEnvKey)
	}

	switch {
	case strings.HasPrefix(protocol, "http"):
		return otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(url), otlptracehttp.WithHeaders(headers))
	default:
		return otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(url), otlptracegrpc.WithHeaders(headers))
	}
}

func createMetricClient(ctx context.Context, url, protocol string, headers map[string]string) (sdkmetric.Exporter, error) {
	if protocol == "" {
		protocol = os.Getenv(OTELExporterOTLPProtocolEnvKey)
	}

	if url == "" {
		url = os.Getenv(OTELExporterOTLPEndpointEnvKey)
	}

	switch {
	case strings.HasPrefix(protocol, "http"):
		return otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(url), otlpmetrichttp.WithHeaders(headers))
	default:
		return otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpointURL(url), otlpmetricgrpc.WithHeaders(headers))
	}
}

func getFileWriter(path string) io.Writer {
	if path == "" {
		log.Warn("No path provided for file writer, using io.Discard")
		return io.Discard
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		log.Warn("Failed to resolve absolute path, using original", "path", path, "err", err)
		absPath = path
	}

	if fileWriter, ok := fileWriters.Load(absPath); ok {
		return fileWriter.(io.Writer)
	}

	// Ensure directory exists
	if dir := filepath.Dir(absPath); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Warn("Failed to create directory for exporter", "path", absPath, "err", err)
		}
	}

	f, err := os.OpenFile(absPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Warn("Failed to open file for exporter, will use io.Discard instead", "path", absPath, "err", err)
		return io.Discard
	}

	writers, _ := fileWriters.LoadOrStore(absPath, f)
	return writers.(io.Writer)
}

// NewStdoutExporter creates a simple stdout exporter with pretty printing.
func NewStdoutExporter() (trace.SpanExporter, error) {
	return stdouttrace.New(stdouttrace.WithPrettyPrint())
}

// NewCozeLoopExporter creates an OTLP HTTP exporter for CozeLoop.
func NewCozeLoopExporter(ctx context.Context, cfg *configs.CozeLoopExporterConfig) (trace.SpanExporter, error) {
	endpoint := cfg.Endpoint
	return createTraceClient(ctx, endpoint, "", map[string]string{
		"authorization":         "Bearer " + cfg.APIKey,
		"cozeloop-workspace-id": cfg.ServiceName,
	})
}

// NewAPMPlusExporter creates an OTLP HTTP exporter for APMPlus.
func NewAPMPlusExporter(ctx context.Context, cfg *configs.ApmPlusConfig) (trace.SpanExporter, error) {
	endpoint := cfg.Endpoint
	protocol := cfg.Protocol
	return createTraceClient(ctx, endpoint, protocol, map[string]string{
		"X-ByteAPM-AppKey": cfg.APIKey,
	})
}

// NewTLSExporter creates an OTLP HTTP exporter for Volcano TLS.
func NewTLSExporter(ctx context.Context, cfg *configs.TLSExporterConfig) (trace.SpanExporter, error) {
	prepared, dynamicAuth, err := prepareTLSExporterConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	headers := tlsOTLPHeaders(prepared)
	if !dynamicAuth {
		return otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(prepared.Endpoint), otlptracehttp.WithHeaders(headers))
	}
	return otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(prepared.Endpoint),
		otlptracehttp.WithHeaders(headers),
		otlptracehttp.WithHTTPClient(&http.Client{
			Transport: &tlsOTLPAuthRoundTripper{base: http.DefaultTransport},
		}),
	)
}

// PrepareTLSExporterConfig fills TLS observability defaults and ensures a trace topic.
func PrepareTLSExporterConfig(ctx context.Context, cfg *configs.TLSExporterConfig) (*configs.TLSExporterConfig, error) {
	prepared, _, err := prepareTLSExporterConfig(ctx, cfg)
	return prepared, err
}

func prepareTLSExporterConfig(ctx context.Context, cfg *configs.TLSExporterConfig) (*configs.TLSExporterConfig, bool, error) {
	if cfg == nil {
		return nil, false, fmt.Errorf("TLS observability config is required")
	}
	out := cfg.Clone()
	out.Region = firstNonEmpty(out.Region, os.Getenv(configs.EnvObservabilityOpenTelemetryTLSRegion), os.Getenv("REGION"), os.Getenv("AGENTKIT_TOOL_REGION"), ve_tls.DefaultRegion)
	out.ServiceName = firstNonEmpty(out.ServiceName, os.Getenv(configs.EnvObservabilityOpenTelemetryTLSServiceName), os.Getenv(configs.EnvOtelServiceName), ve_tls.DefaultTraceInstanceName)
	out.Endpoint = ve_tls.NormalizeOTLPEndpoint(firstNonEmpty(out.Endpoint, os.Getenv(configs.EnvObservabilityOpenTelemetryTLSEndpoint), os.Getenv(OTELExporterOTLPEndpointEnvKey)), out.Region)
	out.AccessKey = firstNonEmpty(out.AccessKey, os.Getenv(configs.EnvObservabilityOpenTelemetryTLSAccessKey))
	out.SecretKey = firstNonEmpty(out.SecretKey, os.Getenv(configs.EnvObservabilityOpenTelemetryTLSSecretKey))
	if out.AccessKey != "" || out.SecretKey != "" {
		if out.AccessKey == "" || out.SecretKey == "" {
			return nil, false, fmt.Errorf("TLS access key and secret key must be configured together")
		}
	}
	out.SessionToken = firstNonEmpty(out.SessionToken, os.Getenv(configs.EnvObservabilityOpenTelemetryTLSSessionToken))
	out.ProjectID = firstNonEmpty(out.ProjectID, os.Getenv(configs.EnvObservabilityOpenTelemetryTLSProjectID))
	out.ProjectName = firstNonEmpty(out.ProjectName, os.Getenv(configs.EnvObservabilityOpenTelemetryTLSProjectName))
	out.TraceInstanceID = firstNonEmpty(out.TraceInstanceID, os.Getenv(configs.EnvObservabilityOpenTelemetryTLSTraceInstanceID))
	out.TraceInstanceName = firstNonEmpty(out.TraceInstanceName, os.Getenv(configs.EnvObservabilityOpenTelemetryTLSTraceInstanceName))
	out.APIEndpoint = ve_tls.NormalizeAPIEndpoint(firstNonEmpty(out.APIEndpoint, os.Getenv(configs.EnvObservabilityOpenTelemetryTLSAPIEndpoint), ve_tls.APIEndpointFromOTLPEndpoint(out.Endpoint, out.Region)), out.Region)

	if strings.TrimSpace(out.TopicID) == "" {
		if !tlsAutoCreateEnabled(out) {
			return nil, false, fmt.Errorf("TLS trace topic is required when %s=false", configs.EnvObservabilityOpenTelemetryTLSAutoCreate)
		}
		client, err := ve_tls.New(&ve_tls.Config{
			AK:           out.AccessKey,
			SK:           out.SecretKey,
			SessionToken: out.SessionToken,
			Region:       out.Region,
			Endpoint:     out.APIEndpoint,
		})
		if err != nil {
			return nil, false, err
		}
		if strings.TrimSpace(out.TraceInstanceID) != "" {
			instance, err := client.DescribeTracingInstanceContext(ctx, out.TraceInstanceID)
			if err != nil {
				return nil, false, fmt.Errorf("describe TLS trace instance %q failed: %w", out.TraceInstanceID, err)
			}
			out.TopicID = strings.TrimSpace(instance.TraceTopicID)
		} else {
			projectID := strings.TrimSpace(out.ProjectID)
			if projectID == "" {
				projectName := firstNonEmpty(out.ProjectName, ve_tls.DefaultProjectNameForService(out.ServiceName))
				var err error
				projectID, err = client.EnsureLogProjectContext(ctx, projectName)
				if err != nil {
					return nil, false, fmt.Errorf("ensure TLS log project %q failed: %w", projectName, err)
				}
				out.ProjectID = projectID
			}
			instanceName := firstNonEmpty(out.TraceInstanceName, ve_tls.DefaultTraceInstanceNameForService(out.ServiceName))
			instance, err := client.EnsureTracingInstanceContext(ctx, projectID, instanceName)
			if err != nil {
				return nil, false, fmt.Errorf("ensure TLS trace instance %q failed: %w", instanceName, err)
			}
			out.TraceInstanceID = firstNonEmpty(out.TraceInstanceID, instance.TraceInstanceID)
			out.TraceInstanceName = firstNonEmpty(out.TraceInstanceName, instance.TraceInstanceName)
			out.TopicID = strings.TrimSpace(instance.TraceTopicID)
		}
	}
	if strings.TrimSpace(out.TopicID) == "" {
		return nil, false, fmt.Errorf("TLS trace topic is required")
	}
	dynamicAuth := strings.TrimSpace(out.AccessKey) == "" || strings.TrimSpace(out.SecretKey) == ""
	return out, dynamicAuth, nil
}

func tlsAutoCreateEnabled(cfg *configs.TLSExporterConfig) bool {
	if cfg != nil && cfg.AutoCreate != nil {
		return *cfg.AutoCreate
	}
	value := strings.TrimSpace(os.Getenv(configs.EnvObservabilityOpenTelemetryTLSAutoCreate))
	if value == "" {
		return true
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return true
	}
	return parsed
}

func tlsOTLPHeaders(cfg *configs.TLSExporterConfig) map[string]string {
	headers := map[string]string{
		"x-tls-otel-tracetopic": strings.TrimSpace(cfg.TopicID),
		"x-tls-otel-region":     strings.TrimSpace(cfg.Region),
	}
	if strings.TrimSpace(cfg.AccessKey) != "" && strings.TrimSpace(cfg.SecretKey) != "" {
		headers["x-tls-otel-ak"] = strings.TrimSpace(cfg.AccessKey)
		headers["x-tls-otel-sk"] = strings.TrimSpace(cfg.SecretKey)
		addTLSSessionTokenMap(headers, cfg.SessionToken)
	}
	return headers
}

type tlsOTLPAuthRoundTripper struct {
	base http.RoundTripper
}

func (t *tlsOTLPAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	ak, sk, sessionToken := veauth.GetAuthInfo()
	if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
		return nil, fmt.Errorf("TLS OTLP credentials are required")
	}
	next := req.Clone(req.Context())
	next.Header = req.Header.Clone()
	next.Header.Set("x-tls-otel-ak", strings.TrimSpace(ak))
	next.Header.Set("x-tls-otel-sk", strings.TrimSpace(sk))
	addTLSSessionTokenHeaders(next.Header, sessionToken)
	base := http.RoundTripper(http.DefaultTransport)
	if t != nil && t.base != nil {
		base = t.base
	}
	return base.RoundTrip(next)
}

func addTLSSessionTokenHeaders(headers http.Header, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	headers.Set("x-tls-otel-token", token)
	headers.Set("X-Security-Token", token)
}

func addTLSSessionTokenMap(headers map[string]string, token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	headers["x-tls-otel-token"] = token
	headers["X-Security-Token"] = token
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// NewFileExporter creates a span exporter that writes traces to a file.
func NewFileExporter(ctx context.Context, cfg *configs.FileConfig) (trace.SpanExporter, error) {
	f := getFileWriter(cfg.Path)
	return stdouttrace.New(stdouttrace.WithWriter(f), stdouttrace.WithPrettyPrint())
}

// NewMultiExporter creates a span exporter that can export to multiple platforms simultaneously.
func NewMultiExporter(ctx context.Context, cfg *configs.OpenTelemetryConfig) (trace.SpanExporter, error) {
	var exporters []trace.SpanExporter
	if cfg.Stdout != nil && cfg.Stdout.Enable {
		if exp, err := NewStdoutExporter(); err == nil {
			exporters = append(exporters, exp)
			log.Info("Exporting spans to Stdout")
		} else {
			return nil, err
		}
	}

	if cfg.File != nil && cfg.File.Path != "" {
		if exp, err := NewFileExporter(ctx, cfg.File); err == nil {
			exporters = append(exporters, exp)
			log.Info(fmt.Sprintf("Exporting spans to File: %s", cfg.File.Path))
		} else {
			return nil, err
		}
	}

	if cfg.ApmPlus != nil {
		if cfg.ApmPlus.Endpoint == "" || cfg.ApmPlus.APIKey == "" {
			return nil, fmt.Errorf("APMPlus endpoint and api_key are required")
		}
		if exp, err := NewAPMPlusExporter(ctx, cfg.ApmPlus); err == nil {
			exporters = append(exporters, exp)
			log.Info("Exporting spans to APMPlus", "endpoint", cfg.ApmPlus.Endpoint, "service_name", cfg.ApmPlus.ServiceName)
		} else {
			return nil, err
		}
	}

	if cfg.CozeLoop != nil {
		if cfg.CozeLoop.Endpoint == "" || cfg.CozeLoop.APIKey == "" {
			return nil, fmt.Errorf("CozeLoop endpoint and api_key are required")
		}
		if exp, err := NewCozeLoopExporter(ctx, cfg.CozeLoop); err == nil {
			exporters = append(exporters, exp)
			log.Info("Exporting spans to CozeLoop", "endpoint", cfg.CozeLoop.Endpoint, "service_name", cfg.CozeLoop.ServiceName)
		} else {
			return nil, err
		}
	}

	if cfg.TLS != nil {
		if exp, err := NewTLSExporter(ctx, cfg.TLS); err == nil {
			exporters = append(exporters, exp)
			log.Info("Exporting spans to TLS", "endpoint", cfg.TLS.Endpoint, "service_name", cfg.TLS.ServiceName)
		} else {
			return nil, err
		}
	}

	log.Debug("trace data will be exported", "exporter count", len(exporters))

	if len(exporters) == 0 {
		log.Info("No exporters to export observability data")
		return nil, ErrNoExporters
	}

	if len(exporters) == 1 {
		return exporters[0], nil
	}

	return &multiExporter{exporters: exporters}, nil
}

type multiExporter struct {
	exporters []trace.SpanExporter
}

func (m *multiExporter) ExportSpans(ctx context.Context, spans []trace.ReadOnlySpan) error {
	var errs []error
	for _, e := range m.exporters {
		if err := e.ExportSpans(ctx, spans); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *multiExporter) Shutdown(ctx context.Context) error {
	var errs []error
	for _, e := range m.exporters {
		if err := e.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// NewMetricReader creates one or more metric readers based on the provided configuration.
func NewMetricReader(ctx context.Context, cfg *configs.OpenTelemetryConfig) ([]sdkmetric.Reader, error) {
	var readers []sdkmetric.Reader

	if cfg.Stdout != nil && cfg.Stdout.Enable {
		if exp, err := stdoutmetric.New(); err == nil {
			readers = append(readers, sdkmetric.NewPeriodicReader(exp))
			log.Info("Exporting metrics to Stdout")
		}
	}

	if cfg.File != nil && cfg.File.Path != "" {
		if exp, err := NewFileMetricExporter(ctx, cfg.File); err == nil {
			readers = append(readers, sdkmetric.NewPeriodicReader(exp))
			log.Info(fmt.Sprintf("Exporting metrics to File: %s", cfg.File.Path))
		}
	}

	if cfg.ApmPlus != nil && cfg.ApmPlus.Endpoint != "" && cfg.ApmPlus.APIKey != "" {
		if exp, err := NewAPMPlusMetricExporter(ctx, cfg.ApmPlus); err == nil {
			readers = append(readers, sdkmetric.NewPeriodicReader(exp))
			log.Info("Exporting metrics to APMPlus", "endpoint", cfg.ApmPlus.Endpoint, "service_name", cfg.ApmPlus.ServiceName)
		}
	}

	log.Debug("metric data will be exported", "exporter count", len(readers))

	return readers, nil
}

// NewCozeLoopMetricExporter creates an OTLP Metric exporter for CozeLoop.
func NewCozeLoopMetricExporter(ctx context.Context, cfg *configs.CozeLoopExporterConfig) (sdkmetric.Exporter, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		return nil, fmt.Errorf("CozeLoop exporter endpoint is required")
	}

	return createMetricClient(ctx, endpoint, "", map[string]string{
		"authorization":         "Bearer " + cfg.APIKey,
		"cozeloop-workspace-id": cfg.ServiceName,
	})
}

// NewAPMPlusMetricExporter creates an OTLP Metric exporter for APMPlus.
// Supports automatic gRPC (4317) detection.
func NewAPMPlusMetricExporter(ctx context.Context, cfg *configs.ApmPlusConfig) (sdkmetric.Exporter, error) {
	endpoint := cfg.Endpoint
	protocol := cfg.Protocol
	return createMetricClient(ctx, endpoint, protocol, map[string]string{
		"X-ByteAPM-AppKey": cfg.APIKey,
	})

}

// NewTLSMetricExporter creates an OTLP Metric exporter for Volcano TLS.
func NewTLSMetricExporter(ctx context.Context, cfg *configs.TLSExporterConfig) (sdkmetric.Exporter, error) {
	endpoint := cfg.Endpoint

	return createMetricClient(ctx, endpoint, "", map[string]string{
		"x-tls-otel-tracetopic": cfg.TopicID,
		"x-tls-otel-ak":         cfg.AccessKey,
		"x-tls-otel-sk":         cfg.SecretKey,
		"x-tls-otel-region":     cfg.Region,
	})
}

// NewFileMetricExporter creates a metric exporter that writes metrics to a file.
func NewFileMetricExporter(ctx context.Context, cfg *configs.FileConfig) (sdkmetric.Exporter, error) {
	writer := getFileWriter(cfg.Path)

	return stdoutmetric.New(stdoutmetric.WithWriter(writer), stdoutmetric.WithPrettyPrint())
}
