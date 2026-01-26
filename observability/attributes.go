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

	"github.com/volcengine/veadk-go/configs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SetCommonAttributes enriches the span with common attributes from context, config, or env.
func SetCommonAttributes(ctx context.Context, span trace.Span) {
	// 1. Fixed attributes
	span.SetAttributes(attribute.String(CozeloopReportSourceKey, DefaultCozeLoopReportSource))

	// 2. Dynamic attributes
	setDynamicAttribute(span, GenAISystemKey, GetModelProvider(ctx), FallbackModelProvider)
	setDynamicAttribute(span, GenAISystemVersionKey, Version, "", InstrumentationKey)
	setDynamicAttribute(span, CozeloopCallTypeKey, GetCallType(ctx), DefaultCozeLoopCallType)
	setDynamicAttribute(span, GenAISessionIdKey, GetSessionId(ctx), FallbackSessionID, SessionIdDotKey)
	setDynamicAttribute(span, GenAIUserIdKey, GetUserId(ctx), FallbackUserID, UserIdDotKey)
	setDynamicAttribute(span, GenAIAppNameKey, GetAppName(ctx), FallbackAppName, AppNameUnderlineKey, AppNameDotKey)
	setDynamicAttribute(span, GenAIAgentNameKey, GetAgentName(ctx), FallbackAgentName, AgentNameKey, AgentNameDotKey)
	setDynamicAttribute(span, GenAIInvocationIdKey, GetInvocationId(ctx), FallbackInvocationID, InvocationIdDotKey)
}

// setDynamicAttribute sets an attribute and its aliases if the value is not empty (or falls back to a default).
func setDynamicAttribute(span trace.Span, key string, val string, fallback string, aliases ...string) {
	v := val
	if v == "" {
		v = fallback
	}
	if v != "" {
		span.SetAttributes(attribute.String(key, v))
		for _, alias := range aliases {
			span.SetAttributes(attribute.String(alias, v))
		}
	}
}

// SetLLMAttributes sets standard GenAI attributes for LLM spans.
func SetLLMAttributes(span trace.Span) {
	span.SetAttributes(
		attribute.String(GenAISpanKindKey, SpanKindLLM),
		attribute.String(GenAIOperationNameKey, "chat"),
	)
}

// SetToolAttributes sets standard GenAI attributes for Tool spans.
func SetToolAttributes(span trace.Span, name string) {
	span.SetAttributes(
		attribute.String(GenAISpanKindKey, SpanKindTool),
		attribute.String(GenAIOperationNameKey, "execute_tool"),
		attribute.String(GenAIToolNameKey, name),
	)
}

// SetAgentAttributes sets standard GenAI attributes for Agent spans.
func SetAgentAttributes(span trace.Span, name string) {
	span.SetAttributes(
		attribute.String(GenAIAgentNameKey, name),
		attribute.String(AgentNameKey, name),    // Alias: agent_name
		attribute.String(AgentNameDotKey, name), // Alias: agent.name
	)
}

// SetWorkflowAttributes sets standard GenAI attributes for Workflow/Root spans.
func SetWorkflowAttributes(span trace.Span) {
	span.SetAttributes(
		attribute.String(GenAISpanKindKey, SpanKindWorkflow),
		attribute.String(GenAIOperationNameKey, "invocation"),
	)
}

func WithSessionId(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ContextKeySessionId, id)
}

func GetSessionId(ctx context.Context) string {
	return getContextString(ctx, ContextKeySessionId, EnvSessionId)
}

func WithUserId(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ContextKeyUserId, id)
}

func GetUserId(ctx context.Context) string {
	return getContextString(ctx, ContextKeyUserId, EnvUserId)
}

func WithAppName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, ContextKeyAppName, name)
}

func GetAppName(ctx context.Context) string {
	return getContextString(ctx, ContextKeyAppName, EnvAppName)
}

func WithAgentName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, ContextKeyAgentName, name)
}

func GetAgentName(ctx context.Context) string {
	return getContextString(ctx, ContextKeyAgentName, EnvAgentName)
}

func WithCallType(ctx context.Context, t string) context.Context {
	return context.WithValue(ctx, ContextKeyCallType, t)
}

func GetCallType(ctx context.Context) string {
	return getContextString(ctx, ContextKeyCallType, EnvCallType)
}

func WithModelProvider(ctx context.Context, p string) context.Context {
	return context.WithValue(ctx, ContextKeyModelProvider, p)
}

func GetModelProvider(ctx context.Context) string {
	return getContextString(ctx, ContextKeyModelProvider, EnvModelProvider)
}

func WithInvocationId(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ContextKeyInvocationId, id)
}

func GetInvocationId(ctx context.Context) string {
	if val, ok := ctx.Value(ContextKeyInvocationId).(string); ok && val != "" {
		return val
	}
	return ""
}

// getContextString retrieves a string value from Context -> Global Config -> Environment Variable.
func getContextString(ctx context.Context, key contextKey, envVar string) string {
	// 1. Try Context
	if val, ok := ctx.Value(key).(string); ok && val != "" {
		return val
	}

	// 2. Try Global Config
	if val := getFromGlobalConfig(key); val != "" {
		return val
	}

	// 3. Fallback to Env Var
	return os.Getenv(envVar)
}

func getFromGlobalConfig(key contextKey) string {
	cfg := configs.GetGlobalConfig()
	if cfg == nil {
		return ""
	}

	switch key {
	case ContextKeyModelProvider:
		if cfg.Model != nil && cfg.Model.Agent != nil {
			return cfg.Model.Agent.Provider
		}
	case ContextKeyAppName:
		if ot := cfg.Observability.OpenTelemetry; ot != nil {
			if ot.CozeLoop != nil && ot.CozeLoop.ServiceName != "" {
				return ot.CozeLoop.ServiceName
			}
			if ot.ApmPlus != nil && ot.ApmPlus.ServiceName != "" {
				return ot.ApmPlus.ServiceName
			}
			if ot.TLS != nil && ot.TLS.ServiceName != "" {
				return ot.TLS.ServiceName
			}
		}
	}
	return ""
}
