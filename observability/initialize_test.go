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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/configs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSetGlobalTracerProvider(t *testing.T) {
	// Save original provider to restore
	orig := otel.GetTracerProvider()
	defer otel.SetTracerProvider(orig)

	exporter := tracetest.NewInMemoryExporter()
	// Just verifies no panic and provider is updated
	setGlobalTracerProvider(exporter)

	// Ensure we can start a span
	ctx := context.Background()
	tr := otel.Tracer("test")
	_, span := tr.Start(ctx, "test-span")
	span.End()

	// Force flush
	if tp, ok := otel.GetTracerProvider().(*trace.TracerProvider); ok {
		tp.ForceFlush(ctx)
	}

	spans := exporter.GetSpans()
	assert.Len(t, spans, 1)
}

func TestInitializeWithConfig(t *testing.T) {
	// Nil config should return ErrNoExporters
	err := initWithConfig(context.Background(), nil)
	assert.ErrorIs(t, err, ErrNoExporters)

	// Config without exporters should return ErrNoExporters.
	cfg := &configs.OpenTelemetryConfig{
		EnableMetrics: nil,
	}
	err = initWithConfig(context.Background(), cfg)
	assert.ErrorIs(t, err, ErrNoExporters)

	// Config with stdout exporter should initialize traces.
	cfgGlobal := &configs.OpenTelemetryConfig{
		Stdout: &configs.StdoutConfig{Enable: true},
	}
	err = initWithConfig(context.Background(), cfgGlobal)
	assert.NoError(t, err)

}
