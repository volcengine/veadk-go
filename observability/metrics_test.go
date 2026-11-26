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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMetricsRecording(t *testing.T) {
	// Setup Manual Reader
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test-meter")

	// Initialize instruments into the global slice (this appends, which is fine for testing)
	initializeInstruments(meter)

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
				if m.Name == MetricNameTokenUsage {
					data := m.Data.(metricdata.Histogram[float64])
					for _, dp := range data.DataPoints {
						dir, _ := dp.Attributes.Value("gen_ai_token_type")
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
				if m.Name == MetricNameOperationDuration {
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
				if m.Name == MetricNameFirstTokenLatency {
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

	t.Run("RecordAgentKitDurationWithError", func(t *testing.T) {
		RecordAgentKitDuration(ctx, 2.5, attrs...)

		var rm metricdata.ResourceMetrics
		err := reader.Collect(ctx, &rm)
		assert.NoError(t, err)

		var found bool
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name == MetricNameAgentKitDuration {
					data := m.Data.(metricdata.Histogram[float64])
					for _, dp := range data.DataPoints {
						if dp.Count > 0 {
							assert.Equal(t, uint64(1), dp.Count)
							assert.Equal(t, 2.5, dp.Sum)
							found = true
						}
					}
				}
			}
		}
		assert.True(t, found, "AgentKit duration not found")
	})
}

func TestRegisterMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	assert.NotPanics(t, func() {
		registerMetrics([]sdkmetric.Reader{reader})
	})
}

type tpotFailingHistogram struct{ metric.Float64Histogram }

func (tpotFailingHistogram) Record(context.Context, float64, ...metric.RecordOption) {
	panic("fixture metric failure")
}

func TestStreamingTPOTHistogramContract(t *testing.T) {
	ctx := context.Background()
	attrs := []attribute.KeyValue{
		attribute.String(AttrGenAIRequestModel, "fixture-model"),
		attribute.String(AttrGenAIOperationName, "chat"),
	}
	instrumentsMu.Lock()
	original := streamingTimePerOutputTokenHistograms
	streamingTimePerOutputTokenHistograms = nil
	instrumentsMu.Unlock()
	t.Cleanup(func() {
		instrumentsMu.Lock()
		streamingTimePerOutputTokenHistograms = original
		instrumentsMu.Unlock()
	})
	for _, value := range []float64{.01, 0} {
		t.Run("seconds_and_zero", func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			defer func() { require.NoError(t, mp.Shutdown(ctx)) }()
			initializeInstruments(mp.Meter("tpot-contract"))
			// A failing sink cannot suppress a later healthy sink.
			instrumentsMu.Lock()
			histogram := streamingTimePerOutputTokenHistograms[len(streamingTimePerOutputTokenHistograms)-1]
			streamingTimePerOutputTokenHistograms = []metric.Float64Histogram{tpotFailingHistogram{}, histogram}
			instrumentsMu.Unlock()
			require.NotPanics(t, func() { RecordStreamingTimePerOutputToken(ctx, value, attrs...) })
			var rm metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(ctx, &rm))
			found := false
			for _, scope := range rm.ScopeMetrics {
				for _, m := range scope.Metrics {
					if m.Name != MetricNameStreamingTimePerOutputToken {
						continue
					}
					require.Equal(t, "s", m.Unit)
					points := m.Data.(metricdata.Histogram[float64]).DataPoints
					require.Len(t, points, 1)
					require.Equal(t, uint64(1), points[0].Count)
					require.Equal(t, value, points[0].Sum)
					require.Equal(t, attribute.NewSet(attrs...), points[0].Attributes)
					found = true
				}
			}
			require.True(t, found)
		})
	}
	t.Run("disabled_metrics", func(t *testing.T) {
		instrumentsMu.Lock()
		streamingTimePerOutputTokenHistograms = nil
		instrumentsMu.Unlock()
		require.NotPanics(t, func() { RecordStreamingTimePerOutputToken(ctx, .01, attrs...) })
	})
	t.Run("initialization_and_recording_are_race_free", func(t *testing.T) {
		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer func() { require.NoError(t, mp.Shutdown(ctx)) }()
		var wg sync.WaitGroup
		wg.Go(func() { initializeInstruments(mp.Meter("tpot-concurrent")) })
		wg.Go(func() {
			for range 100 {
				RecordStreamingTimePerOutputToken(ctx, .01, attrs...)
			}
		})
		wg.Wait()
	})
}
