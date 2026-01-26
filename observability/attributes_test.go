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
	"os"
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

func TestContextAttributes(t *testing.T) {
	ctx := context.Background()

	t.Run("SessionId", func(t *testing.T) {
		assert.Equal(t, "", GetSessionId(ctx))
		ctxWithId := WithSessionId(ctx, "test-session")
		assert.Equal(t, "test-session", GetSessionId(ctxWithId))
	})

	t.Run("UserId", func(t *testing.T) {
		assert.Equal(t, "", GetUserId(ctx))
		ctxWithId := WithUserId(ctx, "test-user")
		assert.Equal(t, "test-user", GetUserId(ctxWithId))
	})

	t.Run("AppName", func(t *testing.T) {
		assert.Equal(t, "", GetAppName(ctx))
		ctxWithName := WithAppName(ctx, "test-app")
		assert.Equal(t, "test-app", GetAppName(ctxWithName))
	})
}

func TestEnvFallback(t *testing.T) {
	os.Setenv(EnvAppName, "env-app")
	defer os.Unsetenv(EnvAppName)

	ctx := context.Background()
	assert.Equal(t, "env-app", GetAppName(ctx))

	// Context should still win
	ctxWithApp := WithAppName(ctx, "ctx-app")
	assert.Equal(t, "ctx-app", GetAppName(ctxWithApp))
}

func TestSetCommonAttributes(t *testing.T) {
	ctx := context.Background()
	ctx = WithAppName(ctx, "my-app")
	ctx = WithUserId(ctx, "u123")
	ctx = WithSessionId(ctx, "s456")
	ctx = WithModelProvider(ctx, "doubao")
	ctx = WithInvocationId(ctx, "inv789")

	span := NewMockSpan()
	SetCommonAttributes(ctx, span)

	// Check fixed attributes
	assert.Equal(t, DefaultCozeLoopReportSource, span.Attributes[attribute.Key(CozeloopReportSourceKey)].AsString())

	// Check dynamic attributes
	assert.Equal(t, "doubao", span.Attributes[attribute.Key(GenAISystemKey)].AsString())
	assert.Equal(t, Version, span.Attributes[attribute.Key(GenAISystemVersionKey)].AsString())
	assert.Equal(t, Version, span.Attributes[attribute.Key(InstrumentationKey)].AsString())

	// Check aliases
	assert.Equal(t, "my-app", span.Attributes[attribute.Key(GenAIAppNameKey)].AsString())
	assert.Equal(t, "my-app", span.Attributes[attribute.Key(AppNameUnderlineKey)].AsString())
	assert.Equal(t, "my-app", span.Attributes[attribute.Key(AppNameDotKey)].AsString())

	assert.Equal(t, "u123", span.Attributes[attribute.Key(GenAIUserIdKey)].AsString())
	assert.Equal(t, "u123", span.Attributes[attribute.Key(UserIdDotKey)].AsString())

	assert.Equal(t, "s456", span.Attributes[attribute.Key(GenAISessionIdKey)].AsString())
	assert.Equal(t, "s456", span.Attributes[attribute.Key(SessionIdDotKey)].AsString())

	assert.Equal(t, "inv789", span.Attributes[attribute.Key(GenAIInvocationIdKey)].AsString())
	assert.Equal(t, "inv789", span.Attributes[attribute.Key(InvocationIdDotKey)].AsString())
}

func TestSetSpecificAttributes(t *testing.T) {
	t.Run("LLM", func(t *testing.T) {
		span := NewMockSpan()
		SetLLMAttributes(span)
		assert.Equal(t, SpanKindLLM, span.Attributes[attribute.Key(GenAISpanKindKey)].AsString())
		assert.Equal(t, "chat", span.Attributes[attribute.Key(GenAIOperationNameKey)].AsString())
	})

	t.Run("Tool", func(t *testing.T) {
		span := NewMockSpan()
		SetToolAttributes(span, "my-tool")
		assert.Equal(t, SpanKindTool, span.Attributes[attribute.Key(GenAISpanKindKey)].AsString())
		assert.Equal(t, "execute_tool", span.Attributes[attribute.Key(GenAIOperationNameKey)].AsString())
		assert.Equal(t, "my-tool", span.Attributes[attribute.Key(GenAIToolNameKey)].AsString())
	})

	t.Run("Agent", func(t *testing.T) {
		span := NewMockSpan()
		SetAgentAttributes(span, "my-agent")
		assert.Equal(t, "my-agent", span.Attributes[attribute.Key(GenAIAgentNameKey)].AsString())
		assert.Equal(t, "my-agent", span.Attributes[attribute.Key(AgentNameKey)].AsString())
		assert.Equal(t, "my-agent", span.Attributes[attribute.Key(AgentNameDotKey)].AsString())
	})
}
