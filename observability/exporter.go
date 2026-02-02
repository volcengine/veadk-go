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
	"os"
	"path/filepath"
	"strings"
	"sync"

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
	"go.opentelemetry.io/otel/sdk/trace"
)

const (
	OTELExporterOTLPProtocolEnvKey = "OTEL_EXPORTER_OTLP_PROTOCOL"
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
	endpoint := cfg.Endpoint
	return createTraceClient(ctx, endpoint, "", map[string]string{
		"x-tls-otel-tracetopic": cfg.TopicID,
		"x-tls-otel-ak":         cfg.AccessKey,
		"x-tls-otel-sk":         cfg.SecretKey,
		"x-tls-otel-region":     cfg.Region,
	})
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
		}
	}

	if cfg.File != nil && cfg.File.Path != "" {
		if exp, err := NewFileExporter(ctx, cfg.File); err == nil {
			exporters = append(exporters, exp)
			log.Info(fmt.Sprintf("Exporting spans to File: %s", cfg.File.Path))
		}
	}

	if cfg.ApmPlus != nil && cfg.ApmPlus.Endpoint != "" && cfg.ApmPlus.APIKey != "" {
		if exp, err := NewAPMPlusExporter(ctx, cfg.ApmPlus); err == nil {
			exporters = append(exporters, exp)
			log.Info("Exporting spans to APMPlus", "endpoint", cfg.ApmPlus.Endpoint, "service_name", cfg.ApmPlus.ServiceName)
		}
	}

	if cfg.CozeLoop != nil && cfg.CozeLoop.Endpoint != "" && cfg.CozeLoop.APIKey != "" {
		if exp, err := NewCozeLoopExporter(ctx, cfg.CozeLoop); err == nil {
			exporters = append(exporters, exp)
			log.Info("Exporting spans to CozeLoop", "endpoint", cfg.CozeLoop.Endpoint, "service_name", cfg.CozeLoop.ServiceName)
		}
	}

	if cfg.TLS != nil && cfg.TLS.Endpoint != "" && cfg.TLS.AccessKey != "" && cfg.TLS.SecretKey != "" {
		if exp, err := NewTLSExporter(ctx, cfg.TLS); err == nil {
			exporters = append(exporters, exp)
			log.Info("Exporting spans to TLS", "endpoint", cfg.TLS.Endpoint, "service_name", cfg.TLS.ServiceName)
		}
	}

	log.Debug("trace data will be exported", "exporter count", len(exporters))

	if len(exporters) == 0 {
		log.Info("No exporters to export observability data")
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
