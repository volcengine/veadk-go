package observability

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
)

func TestAppendLLMEventsFromAttributes_BuildsPromptAndCompletionEvents(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String(ADKAttrLLMRequestName, `{"role":"user","parts":[{"text":"hello"}]}`),
		attribute.String(ADKAttrLLMResponseName, `{"role":"model","parts":[{"text":"hi"}]}`),
	}

	out := appendLLMEventsFromAttributes(attrs, nil, time.Unix(1700000000, 0))
	assert.Len(t, out, 4)
	assert.Equal(t, EventGenAIUserMessage, out[0].Name)
	assert.Equal(t, EventGenAIContentPrompt, out[1].Name)
	assert.Equal(t, EventGenAIChoice, out[2].Name)
	assert.Equal(t, EventGenAIContentCompletion, out[3].Name)
}

func TestAppendLLMEventsFromAttributes_DeduplicatesExistingEvents(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String(AttrInputValue, `{"parts":[{"text":"hello"}]}`),
		attribute.String(AttrOutputValue, `{"parts":[{"text":"hi"}]}`),
	}
	base := []trace.Event{
		{Name: EventGenAIUserMessage},
		{Name: EventGenAIChoice},
	}

	out := appendLLMEventsFromAttributes(attrs, base, time.Unix(1700000000, 0))
	assert.Len(t, out, 4)
	assert.Equal(t, EventGenAIUserMessage, out[0].Name)
	assert.Equal(t, EventGenAIChoice, out[1].Name)
	assert.Equal(t, EventGenAIContentPrompt, out[2].Name)
	assert.Equal(t, EventGenAIContentCompletion, out[3].Name)
}
