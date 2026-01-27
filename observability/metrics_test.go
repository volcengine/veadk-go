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
)

func TestMetricsRecording(t *testing.T) {
	// Setup Manual Reader
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test-meter")

	// Initialize instruments into the global slice (this appends, which is fine for testing)
	InitializeInstruments(meter)

	ctx := context.Background()
	attrs := []attribute.KeyValue{attribute.String("test.key", "test.val")}

	t.Run("RecordTokenUsage", func(t *testing.T) {
		RecordTokenUsage(ctx, 10, 20, attrs...)

		var rm metricdata.ResourceMetrics
		err := reader.Collect(ctx, &rm)
		assert.NoError(t, err)

		// Find the token usage metric
		var foundInput, foundOutput bool
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name == MetricNameLLMTokenUsage {
					data := m.Data.(metricdata.Histogram[float64])
					for _, dp := range data.DataPoints {
						dir, _ := dp.Attributes.Value("token.direction")
						if dir.AsString() == "input" {
							assert.Equal(t, uint64(1), dp.Count)
							assert.Equal(t, 10.0, dp.Sum)
							foundInput = true
						} else if dir.AsString() == "output" {
							assert.Equal(t, uint64(1), dp.Count)
							assert.Equal(t, 20.0, dp.Sum)
							foundOutput = true
						}
					}
				}
			}
		}
		assert.True(t, foundInput, "Input tokens not found")
		assert.True(t, foundOutput, "Output tokens not found")
	})

	t.Run("RecordOperationDuration", func(t *testing.T) {
		RecordOperationDuration(ctx, 1.5, attrs...)

		var rm metricdata.ResourceMetrics
		err := reader.Collect(ctx, &rm)
		assert.NoError(t, err)

		var found bool
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name == MetricNameLLMOperationDuration {
					data := m.Data.(metricdata.Histogram[float64])
					for _, dp := range data.DataPoints {
						if dp.Count > 0 {
							assert.Equal(t, uint64(1), dp.Count)
							assert.Equal(t, 1.5, dp.Sum)
							found = true
						}
					}
				}
			}
		}
		assert.True(t, found, "Operation duration not found")
	})

	t.Run("RecordStreamingTimeToFirstToken", func(t *testing.T) {
		RecordStreamingTimeToFirstToken(ctx, 0.1, attrs...)

		var rm metricdata.ResourceMetrics
		err := reader.Collect(ctx, &rm)
		assert.NoError(t, err)

		var found bool
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name == MetricNameLLMStreamingTimeToFirstToken {
					data := m.Data.(metricdata.Histogram[float64])
					for _, dp := range data.DataPoints {
						if dp.Count > 0 {
							assert.Equal(t, uint64(1), dp.Count)
							assert.Equal(t, 0.1, dp.Sum)
							found = true
						}
					}
				}
			}
		}
		assert.True(t, found, "Streaming time to first token not found")
	})

	t.Run("RecordStreamingTimeToGenerate", func(t *testing.T) {
		RecordStreamingTimeToGenerate(ctx, 2.0, attrs...)

		var rm metricdata.ResourceMetrics
		err := reader.Collect(ctx, &rm)
		assert.NoError(t, err)

		var found bool
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name == MetricNameLLMStreamingTimeToGenerate {
					data := m.Data.(metricdata.Histogram[float64])
					for _, dp := range data.DataPoints {
						if dp.Count > 0 {
							assert.Equal(t, uint64(1), dp.Count)
							assert.Equal(t, 2.0, dp.Sum)
							found = true
						}
					}
				}
			}
		}
		assert.True(t, found, "Streaming time to generate not found")
	})

	t.Run("RecordStreamingTimePerOutputToken", func(t *testing.T) {
		RecordStreamingTimePerOutputToken(ctx, 0.05, attrs...)

		var rm metricdata.ResourceMetrics
		err := reader.Collect(ctx, &rm)
		assert.NoError(t, err)

		var found bool
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name == MetricNameLLMStreamingTimePerOutputToken {
					data := m.Data.(metricdata.Histogram[float64])
					for _, dp := range data.DataPoints {
						if dp.Count > 0 {
							assert.Equal(t, uint64(1), dp.Count)
							assert.Equal(t, 0.05, dp.Sum)
							found = true
						}
					}
				}
			}
		}
		assert.True(t, found, "Streaming time per output token not found")
	})

	t.Run("RecordLLMInvocation", func(t *testing.T) {
		RecordLLMInvocation(ctx, attrs...)

		var rm metricdata.ResourceMetrics
		err := reader.Collect(ctx, &rm)
		assert.NoError(t, err)

		var found bool
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name == MetricNameLLMChatCount {
					data := m.Data.(metricdata.Sum[int64])
					for _, dp := range data.DataPoints {
						assert.Equal(t, int64(1), dp.Value)
						found = true
					}
				}
			}
		}
		assert.True(t, found, "LLM invocation not found")
	})

	t.Run("RecordChatException", func(t *testing.T) {
		RecordChatException(ctx, attrs...)

		var rm metricdata.ResourceMetrics
		err := reader.Collect(ctx, &rm)
		assert.NoError(t, err)

		var found bool
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name == MetricNameLLMCompletionsExceptions {
					data := m.Data.(metricdata.Sum[int64])
					for _, dp := range data.DataPoints {
						assert.Equal(t, int64(1), dp.Value)
						found = true
					}
				}
			}
		}
		assert.True(t, found, "Chat exception not found")
	})

	t.Run("RecordAPMPlusSpanLatency", func(t *testing.T) {
		RecordAPMPlusSpanLatency(ctx, 1.0, attrs...)

		var rm metricdata.ResourceMetrics
		err := reader.Collect(ctx, &rm)
		assert.NoError(t, err)

		var found bool
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name == MetricNameAPMPlusSpanLatency {
					data := m.Data.(metricdata.Histogram[float64])
					for _, dp := range data.DataPoints {
						if dp.Count > 0 {
							assert.Equal(t, uint64(1), dp.Count)
							assert.Equal(t, 1.0, dp.Sum)
							found = true
						}
					}
				}
			}
		}
		assert.True(t, found, "APMPlus span latency not found")
	})

	t.Run("RecordAPMPlusToolTokenUsage", func(t *testing.T) {
		RecordAPMPlusToolTokenUsage(ctx, 5, 10, attrs...)

		var rm metricdata.ResourceMetrics
		err := reader.Collect(ctx, &rm)
		assert.NoError(t, err)

		var foundInput, foundOutput bool
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name == MetricNameAPMPlusToolTokenUsage {
					data := m.Data.(metricdata.Histogram[float64])
					for _, dp := range data.DataPoints {
						dir, _ := dp.Attributes.Value("token.direction")
						if dir.AsString() == "input" {
							assert.Equal(t, uint64(1), dp.Count)
							assert.Equal(t, 5.0, dp.Sum)
							foundInput = true
						} else if dir.AsString() == "output" {
							assert.Equal(t, uint64(1), dp.Count)
							assert.Equal(t, 10.0, dp.Sum)
							foundOutput = true
						}
					}
				}
			}
		}
		assert.True(t, foundInput, "APMPlus tool input tokens not found")
		assert.True(t, foundOutput, "APMPlus tool output tokens not found")
	})
}

func TestRegisterLocalMetrics(t *testing.T) {
	// Since RegisterLocalMetrics uses sync.Once, we can only test it doesn't panic.
	// Logic verification is implicitly done via InitializeInstruments testing above.
	reader := sdkmetric.NewManualReader()
	assert.NotPanics(t, func() {
		RegisterLocalMetrics([]sdkmetric.Reader{reader})
	})
}

// We cannot easily test RegisterGlobalMetrics side effects on otel.GetMeterProvider
// without affecting other tests or global state, but basic execution safety check:
func TestRegisterGlobalMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	assert.NotPanics(t, func() {
		RegisterGlobalMetrics([]sdkmetric.Reader{reader})
	})
}
