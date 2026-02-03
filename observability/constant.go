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

// InstrumentationName is the name of this instrumentation package.
const (
	InstrumentationName = "github.com/volcengine/veadk-go"
)

var (
	// Version is the version of this instrumentation package.
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
	SpanInvokeAgent = "invoke_agent" // Will be suffixed with name in code
	SpanCallLLM     = "call_llm"
	SpanExecuteTool = "execute_tool" // Will be suffixed with name in code
)

// Metric names
const (
	MetricNameChatCount                   = "gen_ai.chat.count"
	MetricNameTokenUsage                  = "gen_ai.client.token.usage"
	MetricNameOperationDuration           = "gen_ai.client.operation.duration"
	MetricNameExceptions                  = "gen_ai.chat_completions.exceptions"
	MetricNameFirstTokenLatency           = "gen_ai.chat_completions.streaming_time_to_first_token"
	MetricNameStreamingTimeToGenerate     = "gen_ai.chat_completions.streaming_time_to_generate"
	MetricNameStreamingTimePerOutputToken = "gen_ai.chat_completions.streaming_time_per_output_token"

	// APMPlus specific metrics
	MetricNameAPMPlusSpanLatency    = "apmplus_span_latency"
	MetricNameAPMPlusToolTokenUsage = "apmplus_tool_token_usage"
)

// General attributes
const (
	AttrGenAISystem        = "gen_ai.system"
	AttrGenAISystemVersion = "gen_ai.system.version"
	AttrGenAIAgentName     = "gen_ai.agent.name"
	AttrInstrumentation    = "openinference.instrumentation.veadk"
	AttrGenAIAppName       = "gen_ai.app.name"
	AttrGenAIUserID        = "gen_ai.user.id"
	AttrGenAISessionID     = "gen_ai.session.id"
	AttrGenAIInvocationID  = "gen_ai.invocation.id"
	AttrAgentName          = "agent_name"    // Alias of 'gen_ai.agent.name' for CozeLoop platform
	AttrAgentNameDot       = "agent.name"    // Alias of 'gen_ai.agent.name' for TLS platform
	AttrAppNameUnderline   = "app_name"      // Alias of gen_ai.app.name for CozeLoop platform
	AttrAppNameDot         = "app.name"      // Alias of gen_ai.app.name for TLS platform
	AttrUserID             = "user.id"       // Alias of gen_ai.user.id for CozeLoop/TLS platforms
	AttrSessionID          = "session.id"    // Alias of gen_ai.session.id for CozeLoop/TLS platforms
	AttrInvocationID       = "invocation.id" // Alias of gen_ai.invocation.id for CozeLoop platform

	AttrErrorType            = "error.type"
	AttrCozeloopReportSource = "cozeloop.report.source" // Fixed value: veadk
	AttrCozeloopCallType     = "cozeloop.call_type"     // CozeLoop call type

	// Environment Variable Keys for Zero-Config Attributes
	EnvModelProvider = "VEADK_MODEL_PROVIDER"
	EnvUserID        = "VEADK_USER_ID"
	EnvSessionID     = "VEADK_SESSION_ID"
	EnvAppName       = "VEADK_APP_NAME"
	EnvCallType      = "VEADK_CALL_TYPE"
	EnvAgentName     = "VEADK_AGENT_NAME"

	// Default and fallback values
	DefaultCozeLoopCallType     = "None"  // fixed
	DefaultCozeLoopReportSource = "veadk" // fixed
	FallbackAgentName           = "<unknown_agent_name>"
	FallbackAppName             = "<unknown_app_name>"
	FallbackUserID              = "<unknown_user_id>"
	FallbackSessionID           = "<unknown_session_id>"
	FallbackModelProvider       = "<unknown_model_provider>"
	FallbackInvocationID        = "<unknown_invocation_id>"

	// Span Kind values (GenAI semantic conventions)
	SpanKindWorkflow = "workflow"
	SpanKindLLM      = "llm"
	SpanKindTool     = "tool"
)

// LLM attributes
const (
	AttrGenAIRequestModel                  = "gen_ai.request.model"
	AttrGenAIRequestType                   = "gen_ai.request.type"
	AttrGenAIRequestMaxTokens              = "gen_ai.request.max_tokens"
	AttrGenAIRequestTemperature            = "gen_ai.request.temperature"
	AttrGenAIRequestTopP                   = "gen_ai.request.top_p"
	AttrGenAIRequestFunctions              = "gen_ai.request.functions"
	AttrGenAIResponseModel                 = "gen_ai.response.model"
	AttrGenAIResponseID                    = "gen_ai.response.id"
	AttrGenAIResponseStopReason            = "gen_ai.response.stop_reason"
	AttrGenAIResponseFinishReason          = "gen_ai.response.finish_reason"
	AttrGenAIResponseFinishReasons         = "gen_ai.response.finish_reasons"
	AttrGenAIIsStreaming                   = "gen_ai.is_streaming"
	AttrGenAIPrompt                        = "gen_ai.prompt"
	AttrGenAICompletion                    = "gen_ai.completion"
	AttrGenAIUsageInputTokens              = "gen_ai.usage.input_tokens"
	AttrGenAIUsageOutputTokens             = "gen_ai.usage.output_tokens"
	AttrGenAIUsageTotalTokens              = "gen_ai.usage.total_tokens"
	AttrGenAIUsageCacheCreationInputTokens = "gen_ai.usage.cache_creation_input_tokens"
	AttrGenAIUsageCacheReadInputTokens     = "gen_ai.usage.cache_read_input_tokens"
	AttrGenAIMessages                      = "gen_ai.messages"
	AttrGenAIChoice                        = "gen_ai.choice"
	AttrGenAIResponsePromptTokenCount      = "gen_ai.response.prompt_token_count"
	AttrGenAIResponseCandidatesTokenCount  = "gen_ai.response.candidates_token_count"
	AttrGenAITokenType                     = "gen_ai_token_type" // Metric specific: underscore

	AttrInputValue  = "input.value"
	AttrOutputValue = "output.value"
)

// Event names
const (
	EventGenAIContentPrompt     = "gen_ai.content.prompt"
	EventGenAIContentCompletion = "gen_ai.content.completion"
)

// Tool attributes
const (
	AttrGenAIOperationName   = "gen_ai.operation.name"
	AttrGenAIOperationType   = "gen_ai.operation.type"
	AttrGenAIToolName        = "gen_ai.tool.name"
	AttrGenAIToolDescription = "gen_ai.tool.description"
	AttrGenAIToolInput       = "gen_ai.tool.input"
	AttrGenAIToolOutput      = "gen_ai.tool.output"
	AttrGenAISpanKind        = "gen_ai.span.kind"
	AttrGenAIToolCallID      = "gen_ai.tool.call.id"

	// Platform specific
	AttrCozeloopInput  = "cozeloop.input"
	AttrCozeloopOutput = "cozeloop.output"
	AttrGenAIInput     = "gen_ai.input"
	AttrGenAIOutput    = "gen_ai.output"
)

// Context keys for storing runtime values
type contextKey string

// Context keys for storing runtime values
const (
	ContextKeySessionID     contextKey = "veadk.session_id"
	ContextKeyUserID        contextKey = "veadk.user_id"
	ContextKeyAppName       contextKey = "veadk.app_name"
	ContextKeyAgentName     contextKey = "veadk.agent_name"
	ContextKeyCallType      contextKey = "veadk.call_type"
	ContextKeyModelProvider contextKey = "veadk.model_provider"
	ContextKeyInvocationID  contextKey = "veadk.invocation_id"
)
