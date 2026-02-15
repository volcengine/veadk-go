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
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/configs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestGetServiceName(t *testing.T) {
	t.Run("EnvVar", func(t *testing.T) {
		os.Setenv("OTEL_SERVICE_NAME", "env-service")
		defer os.Unsetenv("OTEL_SERVICE_NAME")
		assert.Equal(t, "env-service", getServiceName(&configs.OpenTelemetryConfig{}))
	})

	t.Run("ApmPlus", func(t *testing.T) {
		cfg := &configs.OpenTelemetryConfig{
			ApmPlus: &configs.ApmPlusConfig{ServiceName: "apm-service"},
		}
		assert.Equal(t, "apm-service", getServiceName(cfg))
	})

	t.Run("CozeLoop", func(t *testing.T) {
		cfg := &configs.OpenTelemetryConfig{
			CozeLoop: &configs.CozeLoopExporterConfig{ServiceName: "coze-service"},
		}
		assert.Equal(t, "coze-service", getServiceName(cfg))
	})

	t.Run("TLS", func(t *testing.T) {
		cfg := &configs.OpenTelemetryConfig{
			TLS: &configs.TLSExporterConfig{ServiceName: "tls-service"},
		}
		assert.Equal(t, "tls-service", getServiceName(cfg))
	})

	t.Run("Unknown", func(t *testing.T) {
		assert.Equal(t, "<unknown_service>", getServiceName(&configs.OpenTelemetryConfig{}))
	})
}

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

	// Config with disabled global provider but valid exporter, should return ErrNoExporters.
	cfg := &configs.OpenTelemetryConfig{
		EnableGlobalProvider: false,
		Stdout:               &configs.StdoutConfig{Enable: true},
	}
	err = initWithConfig(context.Background(), cfg)
	assert.ErrorIs(t, err, ErrNoExporters)

	// Config with global provider enabled and stdout
	cfgGlobal := &configs.OpenTelemetryConfig{
		EnableGlobalProvider: true,
		Stdout:               &configs.StdoutConfig{Enable: true},
	}
	err = initWithConfig(context.Background(), cfgGlobal)
	assert.NoError(t, err)

}
