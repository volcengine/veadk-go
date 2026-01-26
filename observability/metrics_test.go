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
				if m.Name == MetricNameTokenUsage {
					data := m.Data.(metricdata.Sum[int64])
					for _, dp := range data.DataPoints {
						dir, _ := dp.Attributes.Value("token.direction")
						if dir.AsString() == "input" {
							assert.Equal(t, int64(10), dp.Value)
							foundInput = true
						} else if dir.AsString() == "output" {
							assert.Equal(t, int64(20), dp.Value)
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

	t.Run("RecordFirstTokenLatency", func(t *testing.T) {
		RecordFirstTokenLatency(ctx, 0.5, attrs...)

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
							assert.Equal(t, 0.5, dp.Sum)
							found = true
						}
					}
				}
			}
		}
		assert.True(t, found, "First token latency not found")
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
