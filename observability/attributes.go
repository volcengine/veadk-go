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
	"strings"

	"github.com/volcengine/veadk-go/configs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/agent"
)

var isAgentKitRuntime = checkAgentKitRuntime()

// setCommonAttributesFromInvocation enriches the span with identity attributes from
// agent.InvocationContext, following ADK canonical sources.
func setCommonAttributesFromInvocation(ctx agent.InvocationContext, span trace.Span) {
	var sessionID, userID, appName string
	if s := ctx.Session(); s != nil {
		sessionID = s.ID()
		userID = s.UserID()
		appName = s.AppName()
	}

	setCommonAttributesWithIdentity(
		context.Context(ctx),
		span,
		sessionID,
		userID,
		appName,
		ctx.InvocationID(),
	)
}

func setCommonAttributesWithIdentity(ctx context.Context, span trace.Span, sessionID, userID, appName, invocationID string) {
	// 1. Fixed attributes
	span.SetAttributes(attribute.String(AttrCozeloopReportSource, DefaultCozeLoopReportSource))

	// 2. Dynamic attributes
	setDynamicAttribute(span, AttrGenAISystem, GetModelProvider(ctx), FallbackModelProvider)
	setDynamicAttribute(span, AttrGenAISystemVersion, Version, "", AttrInstrumentation)
	setDynamicAttribute(span, AttrCozeloopCallType, GetCallType(ctx), DefaultCozeLoopCallType)
	setDynamicAttribute(span, AttrGenAISessionID, sessionID, FallbackSessionID, AttrSessionID)
	setDynamicAttribute(span, AttrGenAIUserID, userID, FallbackUserID, AttrUserID)
	setDynamicAttribute(span, AttrGenAIAppName, appName, FallbackAppName, AttrAppNameUnderline, AttrAppNameDot)
	setDynamicAttribute(span, AttrGenAIInvocationID, invocationID, FallbackInvocationID, AttrInvocationID)
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

// setWorkflowAttributes sets standard GenAI attributes for Workflow/Root spans.
func setWorkflowAttributes(span trace.Span) {
	span.SetAttributes(
		attribute.String(AttrGenAISpanKind, SpanKindWorkflow),
		attribute.String(AttrGenAIOperationName, OperationNameChain),
	)
}

// GetCallType retrieves the Call Type from the context or environment variables.
func GetCallType(ctx context.Context) string {
	return getContextString(ctx, ContextKeyCallType, EnvCallType)
}

// GetModelProvider retrieves the Model Provider from the context or environment variables.
func GetModelProvider(ctx context.Context) string {
	return getContextString(ctx, ContextKeyModelProvider, EnvModelProvider)
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

// OTEL_RESOURCE_ATTRIBUTES="instance.id=123456,apmplus.business_carrier=agentkit_runtime"
func checkAgentKitRuntime() bool {
	otelAttrStr := os.Getenv("OTEL_RESOURCE_ATTRIBUTES")
	if otelAttrStr == "" {
		return false
	}

	otelAttrs := strings.Split(otelAttrStr, ",")
	for _, attr := range otelAttrs {
		kv := strings.Split(attr, "=")
		if len(kv) == 2 && kv[0] == "apmplus.business_carrier" && kv[1] == "agentkit_runtime" {
			return true
		}
	}

	return false
}
