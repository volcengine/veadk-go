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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/volcengine/veadk-go/configs"
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
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	// OTELExporterOTLPProtocolEnvKey is the environment variable key for OTLP protocol.
	OTELExporterOTLPProtocolEnvKey = "OTEL_EXPORTER_OTLP_PROTOCOL"
	// OTELExporterOTLPEndpointEnvKey is the environment variable key for OTLP endpoint.
	OTELExporterOTLPEndpointEnvKey = "OTEL_EXPORTER_OTLP_ENDPOINT"
)

var (
	fileWriters sync.Map

	defaultTLSTimeout = 10 * time.Second
)

// tlsTimeout returns the configured TLS timeout duration.
// If timeoutSec is 0, it returns the default timeout (10s).
func tlsTimeout(timeoutSec int) time.Duration {
	if timeoutSec <= 0 {
		return defaultTLSTimeout
	}
	return time.Duration(timeoutSec) * time.Second
}

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

func createTraceClient(ctx context.Context, url, protocol string, headers map[string]string) (sdktrace.SpanExporter, error) {
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
func NewStdoutExporter() (sdktrace.SpanExporter, error) {
	return stdouttrace.New(stdouttrace.WithPrettyPrint())
}

// NewCozeLoopExporter creates an OTLP HTTP exporter for CozeLoop.
func NewCozeLoopExporter(ctx context.Context, cfg *configs.CozeLoopExporterConfig) (sdktrace.SpanExporter, error) {
	endpoint := cfg.Endpoint
	return createTraceClient(ctx, endpoint, "", map[string]string{
		"authorization":         "Bearer " + cfg.APIKey,
		"cozeloop-workspace-id": cfg.ServiceName,
	})
}

// NewAPMPlusExporter creates an OTLP HTTP exporter for APMPlus.
func NewAPMPlusExporter(ctx context.Context, cfg *configs.ApmPlusConfig) (sdktrace.SpanExporter, error) {
	endpoint := cfg.Endpoint
	protocol := cfg.Protocol
	return createTraceClient(ctx, endpoint, protocol, map[string]string{
		"X-ByteAPM-AppKey": cfg.APIKey,
	})
}

// NewTLSExporter creates an OTLP HTTP exporter for Volcano TLS.
func NewTLSExporter(ctx context.Context, cfg *configs.TLSExporterConfig) (sdktrace.SpanExporter, error) {
	endpoint := cfg.Endpoint
	headers := map[string]string{
		"x-tls-otel-tracetopic": cfg.TopicID,
		"x-tls-otel-ak":         cfg.AccessKey,
		"x-tls-otel-sk":         cfg.SecretKey,
		"x-tls-otel-region":     cfg.Region,
	}
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(endpoint),
		otlptracehttp.WithHeaders(headers),
	}
	timeout := tlsTimeout(cfg.Timeout)
	if timeout > 0 {
		opts = append(opts, otlptracehttp.WithTimeout(timeout))
	}
	return otlptracehttp.New(ctx, opts...)
}

// NewFileExporter creates a span exporter that writes traces to a file.
func NewFileExporter(ctx context.Context, cfg *configs.FileConfig) (sdktrace.SpanExporter, error) {
	f := getFileWriter(cfg.Path)
	return stdouttrace.New(stdouttrace.WithWriter(f), stdouttrace.WithPrettyPrint())
}

// NewMultiExporter creates a span exporter that can export to multiple platforms simultaneously.
func NewMultiExporter(ctx context.Context, cfg *configs.OpenTelemetryConfig) (sdktrace.SpanExporter, error) {
	var exporters []sdktrace.SpanExporter
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
		if cfg.TLS.Endpoint == "" || cfg.TLS.AccessKey == "" || cfg.TLS.SecretKey == "" {
			return nil, fmt.Errorf("TLS endpoint, access_key and secret_key are required")
		}
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
	exporters []sdktrace.SpanExporter
}

func (m *multiExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
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

	if cfg.TLS != nil && cfg.TLS.Endpoint != "" && cfg.TLS.AccessKey != "" && cfg.TLS.SecretKey != "" {
		if exp, err := NewTLSMetricExporter(ctx, cfg.TLS); err == nil {
			readers = append(readers, sdkmetric.NewPeriodicReader(exp))
			log.Info("Exporting metrics to TLS", "endpoint", cfg.TLS.Endpoint, "service_name", cfg.TLS.ServiceName)
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
	headers := map[string]string{
		"x-tls-otel-tracetopic": cfg.TopicID,
		"x-tls-otel-ak":         cfg.AccessKey,
		"x-tls-otel-sk":         cfg.SecretKey,
		"x-tls-otel-region":     cfg.Region,
	}
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpointURL(endpoint),
		otlpmetrichttp.WithHeaders(headers),
	}
	timeout := tlsTimeout(cfg.Timeout)
	if timeout > 0 {
		opts = append(opts, otlpmetrichttp.WithTimeout(timeout))
	}
	return otlpmetrichttp.New(ctx, opts...)
}

// NewFileMetricExporter creates a metric exporter that writes metrics to a file.
func NewFileMetricExporter(ctx context.Context, cfg *configs.FileConfig) (sdkmetric.Exporter, error) {
	writer := getFileWriter(cfg.Path)

	return stdoutmetric.New(stdoutmetric.WithWriter(writer), stdoutmetric.WithPrettyPrint())
}

// InMemoryExporter is an in-memory span exporter for testing and local debugging.
// It stores spans in memory and supports retrieval by session ID.
type InMemoryExporter struct {
	mu               sync.Mutex
	spans            []sdktrace.ReadOnlySpan
	traceID          trace.TraceID
	sessionTraceDict map[string][]trace.TraceID
}

// NewInMemoryExporter creates a new in-memory span exporter.
func NewInMemoryExporter() *InMemoryExporter {
	return &InMemoryExporter{
		spans:            make([]sdktrace.ReadOnlySpan, 0),
		sessionTraceDict: make(map[string][]trace.TraceID),
	}
}

// ExportSpans stores spans in memory with session tracking.
func (e *InMemoryExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, span := range spans {
		// Track trace ID
		if span.SpanContext().TraceID().IsValid() {
			e.traceID = span.SpanContext().TraceID()
		} else {
			log.Warn("Span context is missing or invalid, failed to get trace_id")
		}

		// Track session-to-trace mapping from "call_llm" spans
		if span.Name() == "call_llm" {
			attrs := span.Attributes()
			for _, attr := range attrs {
				if attr.Key == "gen_ai.session.id" {
					sessionID := attr.Value.AsString()
					if sessionID != "" {
						traceID := span.SpanContext().TraceID()
						e.sessionTraceDict[sessionID] = append(e.sessionTraceDict[sessionID], traceID)
					}
					break
				}
			}
		}
	}

	e.spans = append(e.spans, spans...)
	return nil
}

// Shutdown clears all stored spans.
func (e *InMemoryExporter) Shutdown(ctx context.Context) error {
	e.Reset()
	return nil
}

// GetSpans returns all collected spans.
func (e *InMemoryExporter) GetSpans() []sdktrace.ReadOnlySpan {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.spans
}

// GetSpansBySession returns spans associated with a specific session ID.
func (e *InMemoryExporter) GetSpansBySession(sessionID string) []sdktrace.ReadOnlySpan {
	e.mu.Lock()
	defer e.mu.Unlock()

	traceIDs, ok := e.sessionTraceDict[sessionID]
	if !ok || len(traceIDs) == 0 {
		return nil
	}

	traceIDSet := make(map[trace.TraceID]struct{}, len(traceIDs))
	for _, tid := range traceIDs {
		traceIDSet[tid] = struct{}{}
	}

	var result []sdktrace.ReadOnlySpan
	for _, span := range e.spans {
		if _, exists := traceIDSet[span.SpanContext().TraceID()]; exists {
			result = append(result, span)
		}
	}
	return result
}

// Reset clears all stored spans and session mappings.
func (e *InMemoryExporter) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = e.spans[:0]
	e.sessionTraceDict = make(map[string][]trace.TraceID)
}

// TraceID returns the current trace ID.
func (e *InMemoryExporter) TraceID() trace.TraceID {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.traceID
}

// ForceFlush is a no-op for in-memory exporter since spans are stored immediately.
func (e *InMemoryExporter) ForceFlush(ctx context.Context) error {
	return nil
}

// Dump exports spans for the given session to a JSON file.
// If sessionID is empty, all spans are exported.
// Returns the file path of the exported JSON.
func (e *InMemoryExporter) Dump(sessionID, dir string) (string, error) {
	var spans []sdktrace.ReadOnlySpan
	if sessionID != "" {
		spans = e.GetSpansBySession(sessionID)
	} else {
		spans = e.GetSpans()
	}

	type spanRecord struct {
		Name         string                 `json:"name"`
		SpanID       string                 `json:"span_id"`
		TraceID      string                 `json:"trace_id"`
		StartTime    string                 `json:"start_time"`
		EndTime      string                 `json:"end_time"`
		Attributes   map[string]interface{} `json:"attributes"`
		ParentSpanID string                 `json:"parent_span_id,omitempty"`
	}

	records := make([]spanRecord, 0, len(spans))
	for _, s := range spans {
		attrs := make(map[string]interface{})
		for _, kv := range s.Attributes() {
			attrs[string(kv.Key)] = kv.Value.AsInterface()
		}

		var parentSpanID string
		if s.Parent().IsValid() {
			parentSpanID = s.Parent().SpanID().String()
		}

		records = append(records, spanRecord{
			Name:         s.Name(),
			SpanID:       s.SpanContext().SpanID().String(),
			TraceID:      s.SpanContext().TraceID().String(),
			StartTime:    s.StartTime().Format(time.RFC3339Nano),
			EndTime:      s.EndTime().Format(time.RFC3339Nano),
			Attributes:   attrs,
			ParentSpanID: parentSpanID,
		})
	}

	data, err := json.MarshalIndent(records, "", "    ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal spans: %w", err)
	}

	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	traceID := e.TraceID().String()
	filename := fmt.Sprintf("veadk_trace_%s_%s.json", sessionID, traceID)
	if sessionID == "" {
		filename = fmt.Sprintf("veadk_trace_%s.json", traceID)
	}
	filePath := filepath.Join(dir, filename)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write trace file %s: %w", filePath, err)
	}

	log.Info(fmt.Sprintf("Dumped %d spans to %s", len(records), filePath))
	return filePath, nil
}
