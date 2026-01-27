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

	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Bucket boundaries for histograms, aligned with Python ADK
var (
	// Token usage buckets (count)
	genAIClientTokenUsageBuckets = []float64{
		1, 4, 16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864,
	}

	// Operation duration buckets (seconds)
	genAIClientOperationDurationBuckets = []float64{
		0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28, 2.56, 5.12, 10.24, 20.48, 40.96, 81.92,
	}

	// First token latency buckets (seconds)
	genAIServerTimeToFirstTokenBuckets = []float64{
		0.001, 0.005, 0.01, 0.02, 0.04, 0.06, 0.08, 0.1, 0.25, 0.5, 0.75, 1.0, 2.5, 5.0, 7.5, 10.0,
	}
)

var (
	// Slices to hold instruments from multiple providers (Global, Local, etc.)
	localOnce     sync.Once
	globalOnce    sync.Once
	instrumentsMu sync.RWMutex

	// Standard Gen AI Metrics
	tokenUsageHistograms                  []metric.Float64Histogram
	operationDurationHistograms           []metric.Float64Histogram
	streamingTimeToFirstTokenHistograms   []metric.Float64Histogram
	streamingTimeToGenerateHistograms     []metric.Float64Histogram
	streamingTimePerOutputTokenHistograms []metric.Float64Histogram
	llmInvokeCounters                     []metric.Int64Counter
	chatExceptionCounters                 []metric.Int64Counter

	// APMPlus Custom Metrics
	apmplusSpanLatencyHistograms    []metric.Float64Histogram
	apmplusToolTokenUsageHistograms []metric.Float64Histogram
)

// RegisterLocalMetrics initializes the metrics system with a local isolated MeterProvider.
// It does NOT overwrite the global OTel MeterProvider.
func RegisterLocalMetrics(readers []sdkmetric.Reader) {
	localOnce.Do(func() {
		options := []sdkmetric.Option{}
		for _, r := range readers {
			options = append(options, sdkmetric.WithReader(r))
		}

		mp := sdkmetric.NewMeterProvider(options...)
		InitializeInstruments(mp.Meter(InstrumentationName))
	})
}

// RegisterGlobalMetrics configures the global OpenTelemetry MeterProvider with the provided readers.
// This is optional and used when you want unrelated OTel measurements to also be exported.
func RegisterGlobalMetrics(readers []sdkmetric.Reader) {
	globalOnce.Do(func() {
		options := []sdkmetric.Option{}
		for _, r := range readers {
			options = append(options, sdkmetric.WithReader(r))
		}

		mp := sdkmetric.NewMeterProvider(options...)
		otel.SetMeterProvider(mp)
		// No need to call registerMeter here, because the global proxy registered in init()
		InitializeInstruments(otel.GetMeterProvider().Meter(InstrumentationName))
	})
}

// InitializeInstruments initializes the metrics instruments for the provided meter.
func InitializeInstruments(m metric.Meter) {
	instrumentsMu.Lock()
	defer instrumentsMu.Unlock()

	// Token usage histogram with bucket boundaries
	if h, err := m.Float64Histogram(
		MetricNameLLMTokenUsage,
		metric.WithDescription("Token consumption of LLM invocations"),
		metric.WithUnit("count"),
		metric.WithExplicitBucketBoundaries(genAIClientTokenUsageBuckets...),
	); err == nil {
		tokenUsageHistograms = append(tokenUsageHistograms, h)
	}

	// Operation duration histogram with bucket boundaries
	if h, err := m.Float64Histogram(
		MetricNameLLMOperationDuration,
		metric.WithDescription("GenAI operation duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(genAIClientOperationDurationBuckets...),
	); err == nil {
		operationDurationHistograms = append(operationDurationHistograms, h)
	}

	// Streaming time to first token histogram
	if h, err := m.Float64Histogram(
		MetricNameLLMStreamingTimeToFirstToken,
		metric.WithDescription("Time to first token in streaming responses"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(genAIServerTimeToFirstTokenBuckets...),
	); err == nil {
		streamingTimeToFirstTokenHistograms = append(streamingTimeToFirstTokenHistograms, h)
	}

	// Streaming time to generate histogram
	if h, err := m.Float64Histogram(
		MetricNameLLMStreamingTimeToGenerate,
		metric.WithDescription("Total time to generate streaming responses"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(genAIClientOperationDurationBuckets...),
	); err == nil {
		streamingTimeToGenerateHistograms = append(streamingTimeToGenerateHistograms, h)
	}

	// Streaming time per output token histogram
	if h, err := m.Float64Histogram(
		MetricNameLLMStreamingTimePerOutputToken,
		metric.WithDescription("Average time per output token in streaming responses"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(genAIServerTimeToFirstTokenBuckets...),
	); err == nil {
		streamingTimePerOutputTokenHistograms = append(streamingTimePerOutputTokenHistograms, h)
	}

	// LLM invocation counter
	if c, err := m.Int64Counter(
		MetricNameLLMChatCount,
		metric.WithDescription("Number of LLM invocations"),
		metric.WithUnit("count"),
	); err == nil {
		llmInvokeCounters = append(llmInvokeCounters, c)
	}

	// Chat exception counter
	if c, err := m.Int64Counter(
		MetricNameLLMCompletionsExceptions,
		metric.WithDescription("Number of exceptions occurred during chat completions"),
		metric.WithUnit("count"),
	); err == nil {
		chatExceptionCounters = append(chatExceptionCounters, c)
	}

	// APMPlus span latency histogram
	if h, err := m.Float64Histogram(
		MetricNameAPMPlusSpanLatency,
		metric.WithDescription("Span execution time for performance analysis"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(genAIClientOperationDurationBuckets...),
	); err == nil {
		apmplusSpanLatencyHistograms = append(apmplusSpanLatencyHistograms, h)
	}

	// APMPlus tool token usage histogram
	if h, err := m.Float64Histogram(
		MetricNameAPMPlusToolTokenUsage,
		metric.WithDescription("Tool-specific token consumption tracking"),
		metric.WithUnit("count"),
		metric.WithExplicitBucketBoundaries(genAIClientTokenUsageBuckets...),
	); err == nil {
		apmplusToolTokenUsageHistograms = append(apmplusToolTokenUsageHistograms, h)
	}
}

// RecordTokenUsage records the number of tokens used.
func RecordTokenUsage(ctx context.Context, input, output int64, attrs ...attribute.KeyValue) {
	for _, histogram := range tokenUsageHistograms {
		if input > 0 {
			histogram.Record(ctx, float64(input), metric.WithAttributes(
				append(attrs, attribute.String("token.direction", "input"))...))
		}
		if output > 0 {
			histogram.Record(ctx, float64(output), metric.WithAttributes(
				append(attrs, attribute.String("token.direction", "output"))...))
		}
	}
}

// RecordOperationDuration records the duration of an operation.
func RecordOperationDuration(ctx context.Context, durationSeconds float64, attrs ...attribute.KeyValue) {
	for _, histogram := range operationDurationHistograms {
		histogram.Record(ctx, durationSeconds, metric.WithAttributes(attrs...))
	}
}

// RecordFirstTokenLatency records the latency to the first token.
// This function is maintained for backward compatibility
func RecordFirstTokenLatency(ctx context.Context, latencySeconds float64, attrs ...attribute.KeyValue) {
	for _, histogram := range streamingTimeToFirstTokenHistograms {
		histogram.Record(ctx, latencySeconds, metric.WithAttributes(attrs...))
	}
}

// RecordLLMInvocation records an LLM invocation.
func RecordLLMInvocation(ctx context.Context, attrs ...attribute.KeyValue) {
	for _, counter := range llmInvokeCounters {
		counter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordChatException records a chat exception.
func RecordChatException(ctx context.Context, attrs ...attribute.KeyValue) {
	for _, counter := range chatExceptionCounters {
		counter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// RecordStreamingTimeToFirstToken records the time to first token in streaming responses.
func RecordStreamingTimeToFirstToken(ctx context.Context, latencySeconds float64, attrs ...attribute.KeyValue) {
	for _, histogram := range streamingTimeToFirstTokenHistograms {
		histogram.Record(ctx, latencySeconds, metric.WithAttributes(attrs...))
	}
}

// RecordStreamingTimeToGenerate records the total time to generate streaming responses.
func RecordStreamingTimeToGenerate(ctx context.Context, durationSeconds float64, attrs ...attribute.KeyValue) {
	for _, histogram := range streamingTimeToGenerateHistograms {
		histogram.Record(ctx, durationSeconds, metric.WithAttributes(attrs...))
	}
}

// RecordStreamingTimePerOutputToken records the average time per output token in streaming responses.
func RecordStreamingTimePerOutputToken(ctx context.Context, durationSeconds float64, attrs ...attribute.KeyValue) {
	for _, histogram := range streamingTimePerOutputTokenHistograms {
		histogram.Record(ctx, durationSeconds, metric.WithAttributes(attrs...))
	}
}

// RecordAPMPlusSpanLatency records the span latency for APMPlus.
func RecordAPMPlusSpanLatency(ctx context.Context, latencySeconds float64, attrs ...attribute.KeyValue) {
	for _, histogram := range apmplusSpanLatencyHistograms {
		histogram.Record(ctx, latencySeconds, metric.WithAttributes(attrs...))
	}
}

// RecordAPMPlusToolTokenUsage records the tool token usage for APMPlus.
func RecordAPMPlusToolTokenUsage(ctx context.Context, input, output int64, attrs ...attribute.KeyValue) {
	for _, histogram := range apmplusToolTokenUsageHistograms {
		if input > 0 {
			histogram.Record(ctx, float64(input), metric.WithAttributes(
				append(attrs, attribute.String("token.direction", "input"))...))
		}
		if output > 0 {
			histogram.Record(ctx, float64(output), metric.WithAttributes(
				append(attrs, attribute.String("token.direction", "output"))...))
		}
	}
}
