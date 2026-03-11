package observability

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
)

func TestClassifySemanticSpanKind(t *testing.T) {
	tests := []struct {
		name     string
		spanName string
		expect   semanticSpanKind
	}{
		{name: "invocation", spanName: SpanInvocation, expect: semanticSpanInvocation},
		{name: "agent", spanName: SpanPrefixInvokeAgent + "planner", expect: semanticSpanAgent},
		{name: "llm by generate_content prefix", spanName: SpanPrefixGenerateContent + "model", expect: semanticSpanLLM},
		{name: "llm by call_llm", spanName: SpanCallLLM, expect: semanticSpanLLM},
		{name: "tool", spanName: SpanPrefixExecuteTool + "search", expect: semanticSpanTool},
		{name: "unknown", spanName: "custom_span", expect: semanticSpanUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := classifySemanticSpanKind(tt.spanName)
			assert.Equal(t, tt.expect, actual)
		})
	}
}

func TestGetStringAttribute(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String("k1", "v1"),
		attribute.String("empty", ""),
	}

	t.Run("returns matched value", func(t *testing.T) {
		assert.Equal(t, "v1", getStringAttribute(attrs, "k1", "fallback"))
	})

	t.Run("returns fallback when key missing", func(t *testing.T) {
		assert.Equal(t, "fallback", getStringAttribute(attrs, "missing", "fallback"))
	})

	t.Run("returns fallback when value empty", func(t *testing.T) {
		assert.Equal(t, "fallback", getStringAttribute(attrs, "empty", "fallback"))
	})
}
