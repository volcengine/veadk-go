package observability

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/trace"
)

func TestMergeUsageTotals(t *testing.T) {
	t.Run("use provided total", func(t *testing.T) {
		prompt, candidate, total := mergeUsageTotals(100, 200, 300, 10, 20, 40)
		assert.Equal(t, int64(110), prompt)
		assert.Equal(t, int64(220), candidate)
		assert.Equal(t, int64(340), total)
	})

	t.Run("derive total when missing", func(t *testing.T) {
		prompt, candidate, total := mergeUsageTotals(100, 200, 300, 10, 20, 0)
		assert.Equal(t, int64(110), prompt)
		assert.Equal(t, int64(220), candidate)
		assert.Equal(t, int64(330), total)
	})

	t.Run("all zeros", func(t *testing.T) {
		prompt, candidate, total := mergeUsageTotals(0, 0, 0, 0, 0, 0)
		assert.Equal(t, int64(0), prompt)
		assert.Equal(t, int64(0), candidate)
		assert.Equal(t, int64(0), total)
	})
}

func TestRegisterTraceMappingIfPossible(t *testing.T) {
	t.Run("register when both span contexts are valid", func(t *testing.T) {
		registry := GetRegistry()

		adkTraceID, _ := trace.TraceIDFromHex("11111111111111111111111111111111")
		adkSpanID, _ := trace.SpanIDFromHex("1111111111111111")
		veadkTraceID, _ := trace.TraceIDFromHex("22222222222222222222222222222222")
		veadkSpanID, _ := trace.SpanIDFromHex("2222222222222222")

		adkSC := trace.NewSpanContext(trace.SpanContextConfig{TraceID: adkTraceID, SpanID: adkSpanID, TraceFlags: trace.FlagsSampled})
		veadkSC := trace.NewSpanContext(trace.SpanContextConfig{TraceID: veadkTraceID, SpanID: veadkSpanID, TraceFlags: trace.FlagsSampled})

		ok := registerTraceMappingIfPossible(registry, adkSC, veadkSC)
		assert.True(t, ok)

		mappedTraceID, exists := registry.GetVeadkTraceID(adkTraceID)
		assert.True(t, exists)
		assert.Equal(t, veadkTraceID, mappedTraceID)
	})

	t.Run("skip when adk span context is invalid", func(t *testing.T) {
		registry := GetRegistry()

		veadkTraceID, _ := trace.TraceIDFromHex("33333333333333333333333333333333")
		veadkSpanID, _ := trace.SpanIDFromHex("3333333333333333")
		veadkSC := trace.NewSpanContext(trace.SpanContextConfig{TraceID: veadkTraceID, SpanID: veadkSpanID, TraceFlags: trace.FlagsSampled})

		ok := registerTraceMappingIfPossible(registry, trace.SpanContext{}, veadkSC)
		assert.False(t, ok)
	})
}
