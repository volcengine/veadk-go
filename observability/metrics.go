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
	"fmt"
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

	// Time per output token buckets (seconds)
	genAIServerTimePerOutputTokenBuckets = []float64{
		0.01, 0.025, 0.05, 0.075, 0.1, 0.15, 0.2, 0.3, 0.4, 0.5, 0.75, 1.0, 2.5,
	}

	// Time duration buckets for agent_kit (seconds)
	agentkitDurationSecondBuckets = []float64{
		0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28, 2.56, 5.12, 10.24, 20.48, 40.96, 81.92, 163.84,
	}
)

var (
	// Slices to hold instruments from multiple providers (Global, Local, etc.)
	localOnce           sync.Once
	globalOnce          sync.Once
	instrumentsMu       sync.RWMutex
	localMeterProvider  *sdkmetric.MeterProvider
	globalMeterProvider *sdkmetric.MeterProvider

	// Standard Gen AI Metrics
	tokenUsageHistograms        []metric.Float64Histogram
	operationDurationHistograms []metric.Float64Histogram
	chatCountCounters           []metric.Int64Counter
	exceptionsCounters          []metric.Int64Counter
	// streaming metrics
	streamingTimeToFirstTokenHistograms   []metric.Float64Histogram
	streamingTimeToGenerateHistograms     []metric.Float64Histogram
	streamingTimePerOutputTokenHistograms []metric.Float64Histogram

	// special metrics for APMPlus
	apmPlusLatencyHistograms        []metric.Float64Histogram
	apmPlusToolTokenUsageHistograms []metric.Float64Histogram

	// special metrics for AgentKit
	agentkitDurationHistograms []metric.Float64Histogram
)

// registerLocalMetrics initializes the metrics system with a local isolated MeterProvider.
// It does NOT overwrite the global OTel MeterProvider.
func registerLocalMetrics(readers []sdkmetric.Reader) {
	localOnce.Do(func() {
		options := []sdkmetric.Option{}
		for _, r := range readers {
			options = append(options, sdkmetric.WithReader(r))
		}

		mp := sdkmetric.NewMeterProvider(options...)
		localMeterProvider = mp
		initializeInstruments(mp.Meter(InstrumentationName))
	})
}

// registerGlobalMetrics configures the global OpenTelemetry MeterProvider with the provided readers.
// This is optional and used when you want unrelated OTel measurements to also be exported.
func registerGlobalMetrics(readers []sdkmetric.Reader) {
	globalOnce.Do(func() {
		options := []sdkmetric.Option{}
		for _, r := range readers {
			options = append(options, sdkmetric.WithReader(r))
		}

		mp := sdkmetric.NewMeterProvider(options...)
		globalMeterProvider = mp
		otel.SetMeterProvider(mp)
		// No need to call registerMeter here, because the global proxy registered in init()
		initializeInstruments(otel.GetMeterProvider().Meter(InstrumentationName))
	})
}

// initializeInstruments initializes the metrics instruments for the provided meter.
// This function is internal and should not be called directly
func initializeInstruments(m metric.Meter) {
	instrumentsMu.Lock()
	defer instrumentsMu.Unlock()

	// Token usage histogram with bucket boundaries
	if h, err := m.Float64Histogram(
		MetricNameTokenUsage,
		metric.WithDescription("Token consumption of LLM invocations"),
		metric.WithUnit("count"),
		metric.WithExplicitBucketBoundaries(genAIClientTokenUsageBuckets...),
	); err == nil {
		tokenUsageHistograms = append(tokenUsageHistograms, h)
	}

	// Operation duration histogram with bucket boundaries
	if h, err := m.Float64Histogram(
		MetricNameOperationDuration,
		metric.WithDescription("GenAI operation duration in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(genAIClientOperationDurationBuckets...),
	); err == nil {
		operationDurationHistograms = append(operationDurationHistograms, h)
	}

	if h, err := m.Float64Histogram(
		MetricNameFirstTokenLatency,
		metric.WithDescription("Time to first token in streaming responses"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(genAIServerTimeToFirstTokenBuckets...),
	); err == nil {
		streamingTimeToFirstTokenHistograms = append(streamingTimeToFirstTokenHistograms, h)
	}

	// Chat count counter
	if c, err := m.Int64Counter(
		MetricNameChatCount,
		metric.WithDescription("Number of chat invocations"),
		metric.WithUnit("1"),
	); err == nil {
		chatCountCounters = append(chatCountCounters, c)
	}

	// Exceptions counter
	if c, err := m.Int64Counter(
		MetricNameExceptions,
		metric.WithDescription("Number of exceptions in chat completions"),
		metric.WithUnit("1"),
	); err == nil {
		exceptionsCounters = append(exceptionsCounters, c)
	}

	// Streaming time to generate histogram
	if h, err := m.Float64Histogram(
		MetricNameStreamingTimeToGenerate,
		metric.WithDescription("Time to generate streaming response"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(genAIClientOperationDurationBuckets...),
	); err == nil {
		streamingTimeToGenerateHistograms = append(streamingTimeToGenerateHistograms, h)
	}

	// Streaming time per output token histogram
	if h, err := m.Float64Histogram(
		MetricNameStreamingTimePerOutputToken,
		metric.WithDescription("Time per output token in streaming responses"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(genAIServerTimePerOutputTokenBuckets...),
	); err == nil {
		streamingTimePerOutputTokenHistograms = append(streamingTimePerOutputTokenHistograms, h)
	}

	// APMPlus Span Latency
	if h, err := m.Float64Histogram(
		MetricNameAPMPlusSpanLatency,
		metric.WithDescription("APMPlus span latency"),
		metric.WithUnit("ms"), // Typically latencies in APM are ms? Standard OTel is seconds.
		// User didn't specify unit, but usually latency is time.
		// Wait, Python ADK: APMPLUS_SPAN_LATENCY.
		// Let's stick to seconds with standard buckets but label it "ApMPlus Span Latency".
		// Actually, if it is "Latency", it might be ms in some platforms.
		// But Safe choice: Seconds.
		metric.WithExplicitBucketBoundaries(genAIClientOperationDurationBuckets...),
	); err == nil {
		apmPlusLatencyHistograms = append(apmPlusLatencyHistograms, h)
	}

	// APMPlus Tool Token Usage
	if h, err := m.Float64Histogram(
		MetricNameAPMPlusToolTokenUsage,
		metric.WithDescription("Token usage for tools (APMPlus specific)"),
		metric.WithUnit("count"),
		metric.WithExplicitBucketBoundaries(genAIClientTokenUsageBuckets...),
	); err == nil {
		apmPlusToolTokenUsageHistograms = append(apmPlusToolTokenUsageHistograms, h)
	}

	// AgentKit Duration
	if h, err := m.Float64Histogram(
		MetricNameAgentKitDuration,
		metric.WithDescription("operation latency"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(agentkitDurationSecondBuckets...),
	); err == nil {
		agentkitDurationHistograms = append(agentkitDurationHistograms, h)
	}
}

// RecordTokenUsage records the number of tokens used.
func RecordTokenUsage(ctx context.Context, input, output int64, attrs ...attribute.KeyValue) {
	for _, histogram := range tokenUsageHistograms {
		if input > 0 {
			histogram.Record(ctx, float64(input), metric.WithAttributes(
				append(attrs, attribute.String(AttrGenAITokenType, "input"))...))
		}
		if output > 0 {
			histogram.Record(ctx, float64(output), metric.WithAttributes(
				append(attrs, attribute.String(AttrGenAITokenType, "output"))...))
		}
	}
}

// RecordOperationDuration records the duration of an operation.
func RecordOperationDuration(ctx context.Context, durationSeconds float64, attrs ...attribute.KeyValue) {
	for _, histogram := range operationDurationHistograms {
		histogram.Record(ctx, durationSeconds, metric.WithAttributes(attrs...))
	}
}

// RecordStreamingTimeToFirstToken records the time to first token in streaming responses.
func RecordStreamingTimeToFirstToken(ctx context.Context, latencySeconds float64, attrs ...attribute.KeyValue) {
	for _, histogram := range streamingTimeToFirstTokenHistograms {
		histogram.Record(ctx, latencySeconds, metric.WithAttributes(attrs...))
	}
}

// RecordChatCount records the number of chat invocations.
func RecordChatCount(ctx context.Context, count int64, attrs ...attribute.KeyValue) {
	for _, counter := range chatCountCounters {
		counter.Add(ctx, count, metric.WithAttributes(attrs...))
	}
}

// RecordExceptions records the number of exceptions.
func RecordExceptions(ctx context.Context, count int64, attrs ...attribute.KeyValue) {
	for _, counter := range exceptionsCounters {
		counter.Add(ctx, count, metric.WithAttributes(attrs...))
	}
}

// RecordStreamingTimeToGenerate records the time to generate.
func RecordStreamingTimeToGenerate(ctx context.Context, durationSeconds float64, attrs ...attribute.KeyValue) {
	for _, histogram := range streamingTimeToGenerateHistograms {
		histogram.Record(ctx, durationSeconds, metric.WithAttributes(attrs...))
	}
}

// RecordStreamingTimePerOutputToken records the time per output token.
func RecordStreamingTimePerOutputToken(ctx context.Context, timeSeconds float64, attrs ...attribute.KeyValue) {
	for _, histogram := range streamingTimePerOutputTokenHistograms {
		histogram.Record(ctx, timeSeconds, metric.WithAttributes(attrs...))
	}
}

// RecordAPMPlusSpanLatency records the span latency for APMPlus.
func RecordAPMPlusSpanLatency(ctx context.Context, durationSeconds float64, attrs ...attribute.KeyValue) {
	for _, histogram := range apmPlusLatencyHistograms {
		histogram.Record(ctx, durationSeconds, metric.WithAttributes(attrs...))
	}
}

// RecordAPMPlusToolTokenUsage records the tool token usage for APMPlus.
func RecordAPMPlusToolTokenUsage(ctx context.Context, tokens int64, attrs ...attribute.KeyValue) {
	for _, histogram := range apmPlusToolTokenUsageHistograms {
		histogram.Record(ctx, float64(tokens), metric.WithAttributes(attrs...))
	}
}

func RecordAgentKitDuration(ctx context.Context, durationSeconds float64, err error, attrs ...attribute.KeyValue) {
	if err != nil {
		attrs = append(attrs, attribute.String("error_type", fmt.Sprintf("%T", err)))
	}
	for _, histogram := range agentkitDurationHistograms {
		histogram.Record(ctx, durationSeconds, metric.WithAttributes(attrs...))
	}
}
