package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
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

func TestRecordLLMRequestOnCurrentSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := tp.Tracer(InstrumentationName).Start(context.Background(), SpanCallLLM)
	recordLLMRequestOnCurrentSpan(ctx, &model.LLMRequest{
		Model:    "test-model",
		Contents: genai.Text("hello"),
		Config: &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText("be brief", genai.RoleUser),
		},
	})
	span.End()

	spans := exporter.GetSpans()
	assert.Len(t, spans, 1)
	attrs := spanAttributesByKey(spans[0].Attributes)
	assert.Contains(t, attrs[AttrInputValue].AsString(), "hello")
	assert.Contains(t, attrs[AttrGenAIInput].AsString(), "hello")
	assert.Contains(t, attrs[AttrGenAIPrompt].AsString(), "be brief")
	assert.Equal(t, "test-model", attrs[AttrGenAIRequestModel].AsString())

	assert.Len(t, spans[0].Events, 2)
	assert.Equal(t, EventGenAIUserMessage, spans[0].Events[0].Name)
	assert.Equal(t, EventGenAIContentPrompt, spans[0].Events[1].Name)
}

func TestRecordLLMResponseOnCurrentSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := tp.Tracer(InstrumentationName).Start(context.Background(), SpanCallLLM)
	recordLLMResponseOnCurrentSpan(ctx, &model.LLMResponse{
		Content: genai.NewContentFromText("hi there", genai.RoleModel),
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     3,
			CandidatesTokenCount: 4,
			TotalTokenCount:      7,
		},
	}, "response-model")
	span.End()

	spans := exporter.GetSpans()
	assert.Len(t, spans, 1)
	attrs := spanAttributesByKey(spans[0].Attributes)
	assert.Contains(t, attrs[AttrOutputValue].AsString(), "hi there")
	assert.Contains(t, attrs[AttrGenAIOutput].AsString(), "hi there")
	assert.Contains(t, attrs[AttrGenAICompletion].AsString(), "hi there")
	assert.Equal(t, "response-model", attrs[AttrGenAIResponseModel].AsString())
	assert.Equal(t, int64(3), attrs[AttrGenAIUsageInputTokens].AsInt64())
	assert.Equal(t, int64(4), attrs[AttrGenAIUsageOutputTokens].AsInt64())
	assert.Equal(t, int64(7), attrs[AttrGenAIUsageTotalTokens].AsInt64())

	assert.Len(t, spans[0].Events, 2)
	assert.Equal(t, EventGenAIChoice, spans[0].Events[0].Name)
	assert.Equal(t, EventGenAIContentCompletion, spans[0].Events[1].Name)
}

func spanAttributesByKey(attrs []attribute.KeyValue) map[string]attribute.Value {
	out := make(map[string]attribute.Value, len(attrs))
	for _, attr := range attrs {
		out[string(attr.Key)] = attr.Value
	}
	return out
}
