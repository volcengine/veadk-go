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
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// MockSpan is a minimal mock implementation of trace.Span for testing purposes.
type MockSpan struct {
	trace.Span // Embed default NoopSpan to satisfy interface
	Attributes map[attribute.Key]attribute.Value
}

func NewMockSpan() *MockSpan {
	return &MockSpan{
		Span:       noop.Span{},
		Attributes: make(map[attribute.Key]attribute.Value),
	}
}

func (m *MockSpan) SetAttributes(kv ...attribute.KeyValue) {
	for _, a := range kv {
		m.Attributes[a.Key] = a.Value
	}
}

func TestSetSpecificAttributes(t *testing.T) {
	t.Run("Workflow", func(t *testing.T) {
		span := NewMockSpan()
		setWorkflowAttributes(span)
		assert.Equal(t, SpanKindWorkflow, span.Attributes[attribute.Key(AttrGenAISpanKind)].AsString())
		assert.Equal(t, OperationNameChain, span.Attributes[attribute.Key(AttrGenAIOperationName)].AsString())
	})

	t.Run("DynamicAttributeWithFallbackAndAliases", func(t *testing.T) {
		span := NewMockSpan()
		setDynamicAttribute(span, AttrGenAIAgentName, "", FallbackAgentName, AttrAgentName, AttrAgentNameDot)
		assert.Equal(t, FallbackAgentName, span.Attributes[attribute.Key(AttrGenAIAgentName)].AsString())
		assert.Equal(t, FallbackAgentName, span.Attributes[attribute.Key(AttrAgentName)].AsString())
		assert.Equal(t, FallbackAgentName, span.Attributes[attribute.Key(AttrAgentNameDot)].AsString())
	})
}
