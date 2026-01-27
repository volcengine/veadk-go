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
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSpanEnrichmentProcessor(t *testing.T) {
	// 1. Setup Metrics to verify side effects of OnEnd
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	// Initialize instruments into global slice for this test
	InitializeInstruments(mp.Meter("processor-test"))

	// 2. Setup Tracer with Processor
	exporter := tracetest.NewInMemoryExporter()
	processor := &SpanEnrichmentProcessor{}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(processor),
		sdktrace.WithSyncer(exporter),
	)
	tracer := tp.Tracer("test-tracer")

	ctx := context.Background()

	t.Run("LLM Span", func(t *testing.T) {
		_, span := tracer.Start(ctx, SpanCallLLM)
		// Add usage attributes
		span.SetAttributes(
			attribute.Int64(GenAIUsageInputTokensKey, 100),
			attribute.Int64(GenAIUsageOutputTokensKey, 200),
			attribute.Int64(GenAIResponsePromptTokenCountKey, 10), // should be ignored if UsageInputTokensKey is present? or accumulated? Logic says:
			// case GenAIUsageInputTokens, GenAIResponsePromptTokenCount: input = val
			// So last write wins or depends on iteration order.
			// Let's stick to standard keys for now.
		)
		span.End()

		spans := exporter.GetSpans()
		if assert.Len(t, spans, 1) {
			s := spans[0]
			assert.Equal(t, SpanCallLLM, s.Name)
			// Check enriched attributes
			var foundKind, foundOp bool
			for _, a := range s.Attributes {
				if a.Key == GenAISpanKindKey && a.Value.AsString() == SpanKindLLM {
					foundKind = true
				}
				if a.Key == GenAIOperationNameKey && a.Value.AsString() == "chat" {
					foundOp = true
				}
			}
			assert.True(t, foundKind, "GenAISpanKind should be LLM")
			assert.True(t, foundOp, "GenAIOperationName should be chat")
		}
		exporter.Reset()

		// Verify Metrics
		var rm metricdata.ResourceMetrics
		err := reader.Collect(ctx, &rm)
		assert.NoError(t, err)

		var foundToken, foundDuration bool
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name == MetricNameLLMTokenUsage {
					data := m.Data.(metricdata.Histogram[float64])
					// We expect at least some data points
					if len(data.DataPoints) > 0 {
						foundToken = true
					}
				}
				if m.Name == MetricNameLLMOperationDuration {
					foundDuration = true
				}
			}
		}
		assert.True(t, foundToken, "Token usage metric should be recorded")
		assert.True(t, foundDuration, "Duration metric should be recorded")
	})

	t.Run("Tool Span", func(t *testing.T) {
		_, span := tracer.Start(ctx, SpanExecuteTool+" my_tool")
		span.End()

		spans := exporter.GetSpans()
		if assert.Len(t, spans, 1) {
			s := spans[0]
			// Check enriched attributes
			var foundToolName bool
			for _, a := range s.Attributes {
				if a.Key == GenAIToolNameKey && a.Value.AsString() == "my_tool" {
					foundToolName = true
				}
			}
			assert.True(t, foundToolName, "Tool name mismatch")
		}
		exporter.Reset()
	})

	t.Run("Agent Span", func(t *testing.T) {
		_, span := tracer.Start(ctx, SpanInvokeAgent+" my_agent")
		span.End()

		spans := exporter.GetSpans()
		if assert.Len(t, spans, 1) {
			s := spans[0]
			var foundAgentName bool
			for _, a := range s.Attributes {
				if a.Key == GenAIAgentNameKey && a.Value.AsString() == "my_agent" {
					foundAgentName = true
				}
			}
			assert.True(t, foundAgentName, "Agent name mismatch")
		}
		exporter.Reset()
	})

	t.Run("Lifecycle", func(t *testing.T) {
		assert.NoError(t, processor.ForceFlush(ctx))
		assert.NoError(t, processor.Shutdown(ctx))
	})
}
