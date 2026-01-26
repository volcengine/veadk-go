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

package exporter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/trace"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

var (
	fileWriters sync.Map
)

func getFileWriter(path string) io.Writer {
	if path == "" {
		return io.Discard
	}
	if fileWriter, ok := fileWriters.Load(path); ok {
		return fileWriter.(io.Writer)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return io.Discard
	}

	writers, _ := fileWriters.LoadOrStore(path, f)
	return writers.(io.Writer)
}

// NewStdoutExporter creates a simple stdout exporter with pretty printing.
func NewStdoutExporter() (trace.SpanExporter, error) {
	return stdouttrace.New(stdouttrace.WithPrettyPrint())
}

// NewCozeLoopExporter creates an OTLP HTTP exporter for CozeLoop.
func NewCozeLoopExporter(ctx context.Context, cfg *configs.CozeLoopConfig) (trace.SpanExporter, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		return nil, fmt.Errorf("CozeLoop exporter endpoint is required")
	}

	options := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithHeaders(map[string]string{
			"authorization":         "Bearer " + cfg.APIKey,
			"cozeloop-workspace-id": cfg.ServiceName,
		}),
	}

	if !strings.HasPrefix(endpoint, "https://") {
		options = append(options, otlptracehttp.WithInsecure())
	}

	return otlptrace.New(ctx, otlptracehttp.NewClient(options...))
}

// NewAPMPlusExporter creates an OTLP HTTP exporter for APMPlus.
func NewAPMPlusExporter(ctx context.Context, cfg *configs.ApmPlusConfig) (trace.SpanExporter, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		return nil, fmt.Errorf("APMPlus exporter endpoint is required")
	}

	options := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithHeaders(map[string]string{
			"x-byteapm-appkey": cfg.APIKey,
		}),
	}

	if !strings.HasPrefix(endpoint, "https://") {
		options = append(options, otlptracehttp.WithInsecure())
	}

	return otlptrace.New(ctx, otlptracehttp.NewClient(options...))
}

// NewTLSExporter creates an OTLP HTTP exporter for Volcano TLS.
func NewTLSExporter(ctx context.Context, cfg *configs.TLSExporterConfig) (trace.SpanExporter, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		return nil, fmt.Errorf("TLS exporter endpoint is required")
	}

	options := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithHeaders(map[string]string{
			"x-tls-otel-tracetopic": cfg.TopicID,
			"x-tls-otel-ak":         cfg.AccessKey,
			"x-tls-otel-sk":         cfg.SecretKey,
			"x-tls-otel-region":     cfg.Region,
		}),
	}

	if !strings.HasPrefix(endpoint, "https://") {
		options = append(options, otlptracehttp.WithInsecure())
	}

	return otlptrace.New(ctx, otlptracehttp.NewClient(options...))
}

// NewFileExporter creates a span exporter that writes traces to a file.
func NewFileExporter(ctx context.Context, cfg *configs.FileConfig) (trace.SpanExporter, error) {
	f := getFileWriter(cfg.Path)
	return stdouttrace.New(stdouttrace.WithWriter(f), stdouttrace.WithPrettyPrint())
}

// NewMultiExporter creates a span exporter that can export to multiple platforms simultaneously.
func NewMultiExporter(ctx context.Context, cfg *configs.OpenTelemetryConfig) (trace.SpanExporter, error) {
	var exporters []trace.SpanExporter

	// 1. Explicit Exporter Types (Stdout/File)
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

	// 2. Platform Exporters (Can be multiple)
	if cfg.CozeLoop != nil && cfg.CozeLoop.APIKey != "" {
		if exp, err := NewCozeLoopExporter(ctx, cfg.CozeLoop); err == nil {
			exporters = append(exporters, exp)
			log.Info("Exporting spans to CozeLoop", "endpoint", cfg.CozeLoop.Endpoint, "service_name", cfg.CozeLoop.ServiceName)
		}
	}
	if cfg.ApmPlus != nil && cfg.ApmPlus.APIKey != "" {
		if exp, err := NewAPMPlusExporter(ctx, cfg.ApmPlus); err == nil {
			exporters = append(exporters, exp)
			log.Info("Exporting spans to APMPlus", "endpoint", cfg.ApmPlus.Endpoint, "service_name", cfg.ApmPlus.ServiceName)
		}
	}
	if cfg.TLS != nil && cfg.TLS.AccessKey != "" && cfg.TLS.SecretKey != "" {
		if exp, err := NewTLSExporter(ctx, cfg.TLS); err == nil {
			exporters = append(exporters, exp)
			log.Info("Exporting spans to TLS", "endpoint", cfg.TLS.Endpoint, "service_name", cfg.TLS.ServiceName)
		}
	}

	if len(exporters) == 0 {
		return nil, nil // Or return a Noop exporter?
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
		if exp, err := stdoutmetric.New(stdoutmetric.WithPrettyPrint()); err == nil {
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

	if cfg.CozeLoop != nil && cfg.CozeLoop.APIKey != "" {
		if exp, err := NewCozeLoopMetricExporter(ctx, cfg.CozeLoop); err == nil {
			readers = append(readers, sdkmetric.NewPeriodicReader(exp))
			log.Info("Exporting metrics to CozeLoop", "endpoint", cfg.CozeLoop.Endpoint, "service_name", cfg.CozeLoop.ServiceName)
		}
	}
	if cfg.ApmPlus != nil && cfg.ApmPlus.APIKey != "" {
		if exp, err := NewAPMPlusMetricExporter(ctx, cfg.ApmPlus); err == nil {
			readers = append(readers, sdkmetric.NewPeriodicReader(exp))
			log.Info("Exporting metrics to APMPlus", "endpoint", cfg.ApmPlus.Endpoint, "service_name", cfg.ApmPlus.ServiceName)
		}
	}
	if cfg.TLS != nil && cfg.TLS.AccessKey != "" && cfg.TLS.SecretKey != "" {
		if exp, err := NewTLSMetricExporter(ctx, cfg.TLS); err == nil {
			readers = append(readers, sdkmetric.NewPeriodicReader(exp))
			log.Info("Exporting metrics to TLS", "endpoint", cfg.TLS.Endpoint, "service_name", cfg.TLS.ServiceName)
		}
	}

	if len(readers) == 0 {
		return nil, fmt.Errorf("no valid metric configuration found")
	}
	return readers, nil
}

// NewCozeLoopMetricExporter creates an OTLP Metric exporter for CozeLoop.
func NewCozeLoopMetricExporter(ctx context.Context, cfg *configs.CozeLoopConfig) (sdkmetric.Exporter, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		return nil, fmt.Errorf("CozeLoop exporter endpoint is required")
	}

	// CozeLoop usually uses HTTP/HTTPS
	options := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(endpoint),
		otlpmetrichttp.WithHeaders(map[string]string{
			"authorization":         "Bearer " + cfg.APIKey,
			"cozeloop-workspace-id": cfg.ServiceName,
		}),
	}

	if !strings.HasPrefix(endpoint, "https://") {
		options = append(options, otlpmetrichttp.WithInsecure())
	}

	return otlpmetrichttp.New(ctx, options...)
}

// NewAPMPlusMetricExporter creates an OTLP Metric exporter for APMPlus.
// Supports automatic gRPC (4317) detection.
func NewAPMPlusMetricExporter(ctx context.Context, cfg *configs.ApmPlusConfig) (sdkmetric.Exporter, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		return nil, fmt.Errorf("APMPlus exporter endpoint is required")
	}

	// Default to HTTP
	options := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(endpoint),
		otlpmetrichttp.WithHeaders(map[string]string{
			"x-byteapm-appkey": cfg.APIKey,
		}),
	}

	if !strings.HasPrefix(endpoint, "https://") {
		options = append(options, otlpmetrichttp.WithInsecure())
	}
	return otlpmetrichttp.New(ctx, options...)
}

// NewTLSMetricExporter creates an OTLP Metric exporter for Volcano TLS.
func NewTLSMetricExporter(ctx context.Context, cfg *configs.TLSExporterConfig) (sdkmetric.Exporter, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		return nil, fmt.Errorf("TLS exporter endpoint is required")
	}

	options := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(endpoint),
		otlpmetrichttp.WithHeaders(map[string]string{
			"x-tls-otel-tracetopic": cfg.TopicID,
			"x-tls-otel-ak":         cfg.AccessKey,
			"x-tls-otel-sk":         cfg.SecretKey,
			"x-tls-otel-region":     cfg.Region,
		}),
	}
	return otlpmetrichttp.New(ctx, options...)
}

// NewFileMetricExporter creates a metric exporter that writes metrics to a file.
func NewFileMetricExporter(ctx context.Context, cfg *configs.FileConfig) (sdkmetric.Exporter, error) {
	writer := getFileWriter(cfg.Path)

	return stdoutmetric.New(stdoutmetric.WithWriter(writer), stdoutmetric.WithPrettyPrint())
}
