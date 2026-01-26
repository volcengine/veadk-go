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
	"runtime/debug"
)

//
// https://volcengine.github.io/veadk-python/observation/span-attributes/
//

const (
	InstrumentationName = "github.com/volcengine/veadk-go"
)

var (
	Version = getVersion()
)

func getVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == InstrumentationName && dep.Version != "" {
				return dep.Version
			}
		}
		// If linked as main module or not found in deps
		if info.Main.Path == InstrumentationName && info.Main.Version != "" {
			return info.Main.Version
		}
	}
	return "<unknown>"
}

// Span names
const (
	SpanInvocation  = "invocation"
	SpanInvokeAgent = "invoke_agent"
	SpanCallLLM     = "call_llm"
	SpanExecuteTool = "execute_tool"
)

// Metric names
const (
	MetricNameTokenUsage        = "gen_ai.client.token.usage"
	MetricNameOperationDuration = "gen_ai.client.operation.duration"
	MetricNameFirstTokenLatency = "gen_ai.client.token.first_token_latency"
)

// General attributes
const (
	GenAISystemKey        = "gen_ai.system"
	GenAISystemVersionKey = "gen_ai.system.version"
	GenAIAgentNameKey     = "gen_ai.agent.name"
	InstrumentationKey    = "openinference.instrumentation.veadk"
	GenAIAppNameKey       = "gen_ai.app.name"
	GenAIUserIdKey        = "gen_ai.user.id"
	GenAISessionIdKey     = "gen_ai.session.id"
	GenAIInvocationIdKey  = "gen_ai.invocation.id"
	
	// CozeLoop / TLS Platform Aliases
	AgentNameKey          = "agent_name"     // Alias of 'gen_ai.agent.name' for CozeLoop platform
	AgentNameDotKey       = "agent.name"     // Alias of 'gen_ai.agent.name' for TLS platform
	AppNameUnderlineKey   = "app_name"       // Alias of gen_ai.app.name for CozeLoop platform
	AppNameDotKey         = "app.name"       // Alias of gen_ai.app.name for TLS platform
	UserIdDotKey          = "user.id"        // Alias of gen_ai.user.id for CozeLoop/TLS platforms
	SessionIdDotKey       = "session.id"     // Alias of gen_ai.session.id for CozeLoop/TLS platforms
	InvocationIdDotKey    = "invocation.id"  // Alias of gen_ai.invocation.id for CozeLoop platform

	CozeloopReportSourceKey = "cozeloop.report.source" // Fixed value: veadk
	CozeloopCallTypeKey     = "cozeloop.call_type"     // CozeLoop call type

	// Environment Variable Keys for Zero-Config Attributes
	EnvModelProvider = "VEADK_MODEL_PROVIDER"
	EnvUserId        = "VEADK_USER_ID"
	EnvSessionId     = "VEADK_SESSION_ID"
	EnvAppName       = "VEADK_APP_NAME"
	EnvCallType      = "VEADK_CALL_TYPE"

	// Default and fallback values
	DefaultCozeLoopCallType     = "None"  // fixed
	DefaultCozeLoopReportSource = "veadk" // fixed
	FallbackAgentName           = "<unknown_agent_name>"
	FallbackAppName             = "<unknown_app_name>"
	FallbackUserID              = "<unknown_user_id>"
	FallbackSessionID           = "<unknown_session_id>"
	FallbackModelProvider       = "<unknown_model_provider>"

	// Span Kind values (GenAI semantic conventions)
	SpanKindWorkflow = "workflow"
	SpanKindLLM      = "llm"
	SpanKindTool     = "tool"
)

// LLM attributes
const (
	GenAIRequestModelKey                  = "gen_ai.request.model"
	GenAIRequestTypeKey                   = "gen_ai.request.type"
	GenAIRequestMaxTokensKey              = "gen_ai.request.max_tokens"
	GenAIRequestTemperatureKey            = "gen_ai.request.temperature"
	GenAIRequestTopPKey                   = "gen_ai.request.top_p"
	GenAIRequestFunctionsKey              = "gen_ai.request.functions"
	GenAIResponseModelKey                 = "gen_ai.response.model"
	GenAIResponseIdKey                    = "gen_ai.response.id"
	GenAIResponseStopReasonKey            = "gen_ai.response.stop_reason"
	GenAIResponseFinishReasonKey          = "gen_ai.response.finish_reason"
	GenAIResponseFinishReasonsKey         = "gen_ai.response.finish_reasons"
	GenAIIsStreamingKey                   = "gen_ai.is_streaming"
	GenAIPromptKey                        = "gen_ai.prompt"
	GenAICompletionKey                    = "gen_ai.completion"
	GenAIUsageInputTokensKey              = "gen_ai.usage.input_tokens"
	GenAIUsageOutputTokensKey             = "gen_ai.usage.output_tokens"
	GenAIUsageTotalTokensKey              = "gen_ai.usage.total_tokens"
	GenAIUsageCacheCreationInputTokensKey = "gen_ai.usage.cache_creation_input_tokens"
	GenAIUsageCacheReadInputTokensKey     = "gen_ai.usage.cache_read_input_tokens"
	GenAIMessagesKey                      = "gen_ai.messages"
	GenAIChoiceKey                        = "gen_ai.choice"
	GenAIResponsePromptTokenCountKey      = "gen_ai.response.prompt_token_count"
	GenAIResponseCandidatesTokenCountKey  = "gen_ai.response.candidates_token_count"

	GenAIInputValueKey  = "input.value"
	GenAIOutputValueKey = "output.value"
)

// Tool attributes
const (
	GenAIOperationNameKey = "gen_ai.operation.name"
	GenAIToolNameKey      = "gen_ai.tool.name"
	GenAIToolInputKey     = "gen_ai.tool.input"
	GenAIToolOutputKey    = "gen_ai.tool.output"
	GenAISpanKindKey      = "gen_ai.span.kind"

	// Platform specific
	CozeloopInputKey  = "cozeloop.input"
	CozeloopOutputKey = "cozeloop.output"
	GenAIInputKey     = "gen_ai.input"
	GenAIOutputKey    = "gen_ai.output"
)

// Context keys for storing runtime values
type contextKey string

const (
	ContextKeySessionId     contextKey = "veadk.session_id"
	ContextKeyUserId        contextKey = "veadk.user_id"
	ContextKeyAppName       contextKey = "veadk.app_name"
	ContextKeyCallType      contextKey = "veadk.call_type"
	ContextKeyModelProvider contextKey = "veadk.model_provider"
	ContextKeyInvocationId  contextKey = "veadk.invocation_id"
)
