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
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"
	"google.golang.org/adk/telemetry"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var (
	initConfigOnce sync.Once
	initErr        error
	// ErrNoExporters is returned when no exporters are configured.
	ErrNoExporters = errors.New("observability disabled: no exporters configured")
)

// Init initializes the observability system using the global configuration.
// Users usually don't need to call this function directly unless they want to override the default global configuration.
// NewPlugin will call this function to initialize observability once.
func Init(ctx context.Context, cfg *configs.ObservabilityConfig) error {
	initConfigOnce.Do(func() {
		// In veadk-go, config loading might depend on loggers which might depend on global tracer
		// or vice versa. We ensure InitConfig is called, and then initialize based on that.
		var otelCfg *configs.OpenTelemetryConfig
		if cfg != nil {
			otelCfg = cfg.OpenTelemetry
		}

		if otelCfg == nil || !hasEnabledExporters(otelCfg) {
			log.Info("No observability config found or no exporters enabled, observability data will not be exported")
			initErr = ErrNoExporters
			return
		}

		handleSignals(ctx)

		initErr = initWithConfig(ctx, otelCfg)
		if initErr == nil {
			log.Info("Initializing TraceProvider and MetricsProvider based on observability config")
		}
	})
	return initErr
}

func hasEnabledExporters(cfg *configs.OpenTelemetryConfig) bool {
	if cfg == nil {
		return false
	}
	if cfg.Stdout != nil && cfg.Stdout.Enable {
		return true
	}
	if cfg.File != nil && cfg.File.Path != "" {
		return true
	}
	if cfg.ApmPlus != nil {
		return true
	}
	if cfg.CozeLoop != nil {
		return true
	}
	if cfg.TLS != nil {
		return true
	}
	return false
}

// Shutdown shuts down the observability system, flushing all spans and metrics.
func Shutdown(ctx context.Context) error {
	log.Info("Shut down TracerProvider and MeterProvider")
	var errs []error

	// 0. End all active root invocation spans to ensure they are recorded and flushed.
	// This handles cases like Ctrl+C or premature exit where defer blocks might not run.
	GetRegistry().EndAllInvocationSpans()
	GetRegistry().Shutdown()

	// 1. Shutdown TracerProvider
	tp := otel.GetTracerProvider()
	if sdkTP, ok := tp.(*sdktrace.TracerProvider); ok {
		if err := sdkTP.ForceFlush(ctx); err != nil {
			log.Error("Failed to force flush TracerProvider", "err", err)
			errs = append(errs, err)
		}

		if err := sdkTP.Shutdown(ctx); err != nil {
			log.Error("Failed to shutdown TracerProvider", "err", err)
			errs = append(errs, err)
		}
	} else {
		log.Info("Global TracerProvider is not an SDK TracerProvider, skipping shutdown")
	}

	// 2. Shutdown local MeterProvider if exists
	if localMeterProvider != nil {
		if err := localMeterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	// 3. Shutdown global MeterProvider if exists
	if globalMeterProvider != nil {
		if err := globalMeterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// initWithConfig automatically initializes the observability system based on the provided configuration.
// It creates the appropriate exporter and calls RegisterExporter.
func initWithConfig(ctx context.Context, cfg *configs.OpenTelemetryConfig) error {
	var errs []error
	err := initializeTraceProvider(ctx, cfg)
	if err != nil {
		errs = append(errs, err)
	}

	err = initializeMeterProvider(ctx, cfg)
	if err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func newVeadkExporter(exp sdktrace.SpanExporter) sdktrace.SpanExporter {
	return &VeADKTranslatedExporter{SpanExporter: exp}
}

// AddSpanExporter registers an exporter to Google ADK's local telemetry.
func AddSpanExporter(exp sdktrace.SpanExporter) {
	telemetry.RegisterSpanProcessor(sdktrace.NewBatchSpanProcessor(newVeadkExporter(exp)))
}

// AddGlobalSpanExporter registers an exporter toglobal TracerProvider.
func AddGlobalSpanExporter(exp sdktrace.SpanExporter) {
	globalTP := otel.GetTracerProvider()
	if sdkTP, ok := globalTP.(*sdktrace.TracerProvider); ok {
		sdkTP.RegisterSpanProcessor(sdktrace.NewBatchSpanProcessor(newVeadkExporter(exp)))
	}
}

// setGlobalTracerProvider configures the global OpenTelemetry TracerProvider.
func setGlobalTracerProvider(exp sdktrace.SpanExporter, spanProcessors ...sdktrace.SpanProcessor) {
	// Always wrap with VeADKTranslatedExporter to ensure ADK-internal spans are correctly mapped
	translatedExp := newVeadkExporter(exp)

	// Default processors
	allProcessors := append([]sdktrace.SpanProcessor{}, spanProcessors...)

	// Use BatchSpanProcessor for all exporters to ensure performance and batching.
	finalProcessor := sdktrace.NewBatchSpanProcessor(translatedExp)

	// 1. Try to register with existing TracerProvider if it's an SDK TracerProvider
	globalTP := otel.GetTracerProvider()
	if sdkTP, ok := globalTP.(*sdktrace.TracerProvider); ok {
		log.Info("Registering ADK Processors to existing global TracerProvider")
		for _, sp := range allProcessors {
			sdkTP.RegisterSpanProcessor(sp)
		}
		sdkTP.RegisterSpanProcessor(finalProcessor)
		return
	}

	// 2. Fallback: Create a new global TracerProvider
	log.Info("Creating a new global TracerProvider")
	var opts []sdktrace.TracerProviderOption
	for _, sp := range allProcessors {
		opts = append(opts, sdktrace.WithSpanProcessor(sp))
	}

	tp := sdktrace.NewTracerProvider(
		append(opts, sdktrace.WithSpanProcessor(finalProcessor))...,
	)

	otel.SetTracerProvider(tp)
}

func setupLocalTracer(ctx context.Context, cfg *configs.OpenTelemetryConfig) error {
	if cfg == nil {
		return nil
	}

	exp, err := NewMultiExporter(ctx, cfg)
	if err != nil {
		return err
	}

	AddSpanExporter(exp)
	return nil
}

func setupGlobalTracer(ctx context.Context, cfg *configs.OpenTelemetryConfig) error {
	log.Info("Registering ADK Global TracerProvider")

	globalExp, err := NewMultiExporter(ctx, cfg)
	if err != nil {
		return err
	}

	if globalExp != nil {
		setGlobalTracerProvider(globalExp)
	}
	return nil
}

func initializeTraceProvider(ctx context.Context, cfg *configs.OpenTelemetryConfig) error {
	var errs []error
	if cfg != nil && cfg.EnableLocalProvider {
		err := setupLocalTracer(ctx, cfg)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if cfg != nil && cfg.EnableGlobalProvider {
		err := setupGlobalTracer(ctx, cfg)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func initializeMeterProvider(ctx context.Context, cfg *configs.OpenTelemetryConfig) error {
	var errs []error
	if cfg == nil || cfg.EnableMetrics == nil || !*cfg.EnableMetrics {
		log.Info("Meter provider is not enabled")
		return nil
	}

	if cfg.EnableLocalProvider {
		readers, err := NewMetricReader(ctx, cfg)
		if err != nil {
			errs = append(errs, err)
		}
		registerLocalMetrics(readers)
	}

	if cfg.EnableGlobalProvider {
		globalReaders, err := NewMetricReader(ctx, cfg)
		if err != nil {
			errs = append(errs, err)
		}
		registerGlobalMetrics(globalReaders)
	}
	return errors.Join(errs...)
}

// handleSignals registers a signal handler to ensure observability data is flushed on exit.
func handleSignals(ctx context.Context) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan

		// Trigger shutdown which will flush all processors (including BatchSpanProcessor)
		_ = Shutdown(ctx)
		os.Exit(0)
	}()
}
