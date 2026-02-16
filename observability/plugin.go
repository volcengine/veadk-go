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
	"time"

	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

const (
	PluginName = "veadk-observability"
)

// NewPlugin creates a new observability plugin for ADK.
// It returns a *plugin.Plugin that can be registered in launcher.Config or agent.Config.
func NewPlugin(opts ...Option) *plugin.Plugin {
	// use global config by default. deep copy to avoid mutating global config.
	observabilityConfig := configs.GetGlobalConfig().Observability.Clone()
	for _, opt := range opts {
		opt(observabilityConfig)
	}

	if err := Init(context.Background(), observabilityConfig); err != nil {
		log.Warn("Return a noop plugin", "error", err)
		return noOpPlugin(PluginName)
	}

	p := &adkObservabilityPlugin{
		config: observabilityConfig,
		tracer: otel.Tracer(InstrumentationName),
	}

	// no need to check the error as it is always nil.
	pluginInstance, _ := plugin.New(plugin.Config{
		Name:                PluginName,
		BeforeRunCallback:   p.BeforeRun,
		AfterRunCallback:    p.AfterRun,
		BeforeAgentCallback: p.BeforeAgent,
		AfterAgentCallback:  p.AfterAgent,
		BeforeModelCallback: p.BeforeModel,
		AfterModelCallback:  p.AfterModel,
		BeforeToolCallback:  p.BeforeTool,
		AfterToolCallback:   p.AfterTool,
	})
	return pluginInstance
}

func noOpPlugin(name string) *plugin.Plugin {
	// Return a no-op plugin to avoid panic in the agent if the user adds it to the plugin list.
	// Since no callbacks are registered, it will have zero overhead during execution.
	p, _ := plugin.New(plugin.Config{
		Name: name,
	})
	return p
}

// Option defines a functional option for the ADKObservabilityPlugin.
type Option func(config *configs.ObservabilityConfig)

// WithEnableMetrics creates an Option to manually control metrics recording.
func WithEnableMetrics(enable bool) Option {
	return func(cfg *configs.ObservabilityConfig) {
		enableVal := enable
		cfg.OpenTelemetry.EnableMetrics = &enableVal
	}
}

type adkObservabilityPlugin struct {
	config *configs.ObservabilityConfig

	tracer trace.Tracer // global tracer
}

func (p *adkObservabilityPlugin) isMetricsEnabled() bool {
	if p.config == nil || p.config.OpenTelemetry == nil || p.config.OpenTelemetry.EnableMetrics == nil {
		return false
	}
	return *p.config.OpenTelemetry.EnableMetrics
}

// BeforeRun is called before an agent run starts.
func (p *adkObservabilityPlugin) BeforeRun(ctx agent.InvocationContext) (*genai.Content, error) {
	log.Debug("Before Run", "InvocationID", ctx.InvocationID(), "SessionID", ctx.Session().ID(), "UserID", ctx.Session().UserID())
	// 1. Start the 'invocation' span
	_, span := p.tracer.Start(context.Context(ctx), SpanInvocation, trace.WithSpanKind(trace.SpanKindServer))

	// 2. Store in state for AfterRun
	_ = ctx.Session().State().Set(stateKeyInvocationSpan, span)
	GetRegistry().RegisterInvocationSpan(span)

	setCommonAttributesFromInvocation(ctx, span)
	setWorkflowAttributes(span)

	// Record start time for metrics
	meta := &spanMetadata{
		StartTime: time.Now(),
	}
	p.storeSpanMetadata(ctx.Session().State(), meta)

	// Capture input from UserContent
	if userContent := ctx.UserContent(); userContent != nil {
		if val := serializeContentForTelemetry(userContent); val != "" {
			span.SetAttributes(
				attribute.String(AttrInputValue, val),
				attribute.String(AttrGenAIInput, val),
			)
			span.AddEvent(EventGenAIUserMessage, trace.WithAttributes(
				attribute.String(AttrGenAIMessages, val),
			))
			span.AddEvent(EventGenAIContentPrompt, trace.WithAttributes(
				attribute.String(AttrInputValue, val),
			))
		}
	}

	return nil, nil
}

// AfterRun is called after an agent run ends.
func (p *adkObservabilityPlugin) AfterRun(ctx agent.InvocationContext) {
	log.Debug("After Run", "InvocationID", ctx.InvocationID(), "SessionID", ctx.Session().ID(), "UserID", ctx.Session().UserID())
	// 1. End the span
	s, _ := ctx.Session().State().Get(stateKeyInvocationSpan)
	if s == nil {
		return
	}

	span := s.(trace.Span)
	log.Debug("AfterRun get a span from state", "span", span, "isRecording", span.IsRecording())

	if span.IsRecording() {
		// Capture final output if available
		if cached, _ := ctx.Session().State().Get(stateKeyStreamingOutput); cached != nil {
			if content, ok := cached.(*genai.Content); ok {
				if val := serializeContentForTelemetry(content); val != "" {
					span.SetAttributes(
						attribute.String(AttrOutputValue, val),
						attribute.String(AttrGenAIOutput, val),
					)
					span.AddEvent(EventGenAIChoice, trace.WithAttributes(
						attribute.String(AttrGenAIChoice, val),
					))
					span.AddEvent(EventGenAIContentCompletion, trace.WithAttributes(
						attribute.String(AttrOutputValue, val),
					))
				}
			}
		}
		// Capture accumulated token usage for the root invocation span
		meta := p.getSpanMetadata(ctx.Session().State())

		if meta.PromptTokens > 0 {
			span.SetAttributes(attribute.Int64(AttrGenAIUsageInputTokens, meta.PromptTokens))
		}
		if meta.CandidateTokens > 0 {
			span.SetAttributes(attribute.Int64(AttrGenAIUsageOutputTokens, meta.CandidateTokens))
		}
		if meta.TotalTokens > 0 {
			span.SetAttributes(attribute.Int64(AttrGenAIUsageTotalTokens, meta.TotalTokens))
		}

		// Record final metrics for invocation
		if !meta.StartTime.IsZero() {
			if p.isMetricsEnabled() {
				elapsed := time.Since(meta.StartTime).Seconds()
				metricAttrs := []attribute.KeyValue{
					attribute.String(MetricAttrGenAIOperationName, OperationNameChain),
					attribute.String(MetricAttrGenAIOperationType, OperationTypeWorkflow),
					attribute.String(AttrGenAISystem, GetModelProvider(context.Context(ctx))),
				}
				RecordOperationDuration(context.Background(), elapsed, metricAttrs...)
				RecordAPMPlusSpanLatency(context.Background(), elapsed, metricAttrs...)

				if isAgentKitRuntime {
					agentKitsAttrs := []attribute.KeyValue{
						attribute.String(MetricAttrGenAIOperationName, OperationNameChain),
						attribute.String(MetricAttrGenAIOperationType, OperationTypeWorkflow),
					}

					var errorCode string
					eventLen := ctx.Session().Events().Len()
					if eventLen > 0 {
						lastEvent := ctx.Session().Events().At(eventLen - 1)
						errorCode = lastEvent.ErrorCode
					}
					if errorCode != "" {
						agentKitsAttrs = append(agentKitsAttrs, attribute.String(MetricAttrErrorType, errorCode))
					}
					RecordAgentKitDuration(context.Background(), elapsed, agentKitsAttrs...)
				}
			}
		}

		// Clean up from global map with delay to allow children to be exported.
		// Since we have multiple exporters, we wait long enough for all of them to finish.
		adkSpan := trace.SpanFromContext(context.Context(ctx))
		if adkSpan.SpanContext().IsValid() {
			tid := adkSpan.SpanContext().TraceID()
			veadkInvocationSpanID := span.SpanContext().SpanID()
			GetRegistry().ScheduleCleanup(tid, veadkInvocationSpanID)
		}

		span.End()
	}

}

// BeforeModel is called before the LLM is called.
func (p *adkObservabilityPlugin) BeforeModel(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
	log.Debug("BeforeModel",
		"InvocationID", ctx.InvocationID(), "SessionID", ctx.SessionID(), "UserID", ctx.UserID(), "AgentName", ctx.AgentName(), "AppName", ctx.AppName())
	p.tryBridgeTraceMappingFromCallback(ctx, "BeforeModel")
	// New ADK emits model/tool spans natively. Plugin only keeps metadata for metrics and invocation aggregation.
	meta := p.getSpanMetadata(ctx.State())
	meta.StartTime = time.Now()
	meta.PrevPromptTokens = meta.PromptTokens
	meta.PrevCandidateTokens = meta.CandidateTokens
	meta.PrevTotalTokens = meta.TotalTokens
	meta.ModelName = req.Model
	p.storeSpanMetadata(ctx.State(), meta)
	return nil, nil
}

// AfterModel is called after the LLM returns.
func (p *adkObservabilityPlugin) AfterModel(ctx agent.CallbackContext, resp *model.LLMResponse, err error) (*model.LLMResponse, error) {
	log.Debug("AfterModel",
		"InvocationID", ctx.InvocationID(), "SessionID", ctx.SessionID(), "UserID", ctx.UserID(), "AgentName", ctx.AgentName(), "AppName", ctx.AppName())
	meta := p.getSpanMetadata(ctx.State())

	if err != nil {
		if p.isMetricsEnabled() {
			metricAttrs := []attribute.KeyValue{
				attribute.String(AttrGenAISystem, GetModelProvider(context.Context(ctx))),
				attribute.String("gen_ai_response_model", meta.ModelName),
				attribute.String(MetricAttrGenAIOperationName, OperationNameChat),
				attribute.String(MetricAttrGenAIOperationType, OperationTypeLLM),
				attribute.String(MetricAttrErrorType, ErrorTypeError),
			}
			RecordExceptions(context.Context(ctx), 1, metricAttrs...)
		}
		return nil, nil
	}

	if resp == nil {
		return nil, nil
	}

	finalModelName := meta.ModelName
	if resp.CustomMetadata != nil {
		if m, ok := resp.CustomMetadata["response_model"].(string); ok && m != "" {
			finalModelName = m
		}
	}

	if resp.UsageMetadata != nil {
		p.accumulateLLMUsageAndRecordMetrics(ctx, resp, finalModelName)
	}

	if resp.Content != nil {
		if !resp.Partial {
			_ = ctx.State().Set(stateKeyStreamingOutput, resp.Content)
		}

		parentSC, _ := getInvocationSpanContextFromState(ctx.State())

		adkSpan := trace.SpanFromContext(context.Context(ctx))
		adkTraceID := trace.TraceID{}
		if adkSpan.SpanContext().IsValid() {
			adkTraceID = adkSpan.SpanContext().TraceID()
		}

		for _, part := range resp.Content.Parts {
			if part.FunctionCall != nil && part.FunctionCall.ID != "" && parentSC.IsValid() {
				GetRegistry().RegisterToolCallMapping(part.FunctionCall.ID, adkTraceID, parentSC)
			}
		}
	}

	if !resp.Partial {
		p.recordFinalResponseMetrics(ctx, meta, finalModelName)
	}

	return nil, nil
}

func (p *adkObservabilityPlugin) recordFinalResponseMetrics(ctx agent.CallbackContext, meta *spanMetadata, finalModelName string) {
	if !meta.StartTime.IsZero() {
		duration := time.Since(meta.StartTime).Seconds()
		metricAttrs := []attribute.KeyValue{
			attribute.String(AttrGenAISystem, GetModelProvider(context.Context(ctx))),
			attribute.String("gen_ai_response_model", finalModelName),
			attribute.String(MetricAttrGenAIOperationName, OperationNameChat),
			attribute.String(MetricAttrGenAIOperationType, OperationTypeLLM),
		}
		if p.isMetricsEnabled() {
			RecordOperationDuration(context.Context(ctx), duration, metricAttrs...)
			RecordAPMPlusSpanLatency(context.Context(ctx), duration, metricAttrs...)
		}
	}
}

// accumulateLLMUsageAndRecordMetrics aggregates per-call LLM usage into invocation-level metadata,
// then emits LLM usage metrics for the current response.
//
// This remains in plugin callbacks because invocation-level accumulation requires cross-callback state.
func (p *adkObservabilityPlugin) accumulateLLMUsageAndRecordMetrics(ctx agent.CallbackContext, resp *model.LLMResponse, modelName string) {
	meta := p.getSpanMetadata(ctx.State())

	currentPrompt := int64(resp.UsageMetadata.PromptTokenCount)
	currentCandidate := int64(resp.UsageMetadata.CandidatesTokenCount)
	currentTotal := int64(resp.UsageMetadata.TotalTokenCount)

	meta.PromptTokens, meta.CandidateTokens, meta.TotalTokens = mergeUsageTotals(
		meta.PrevPromptTokens,
		meta.PrevCandidateTokens,
		meta.PrevTotalTokens,
		currentPrompt,
		currentCandidate,
		currentTotal,
	)
	p.storeSpanMetadata(ctx.State(), meta)

	if p.isMetricsEnabled() {
		metricAttrs := []attribute.KeyValue{
			attribute.String(AttrGenAISystem, GetModelProvider(ctx)),
			attribute.String("gen_ai_response_model", modelName),
			attribute.String(MetricAttrGenAIOperationName, OperationNameChat),
			attribute.String(MetricAttrGenAIOperationType, OperationTypeLLM),
		}
		RecordChatCount(context.Context(ctx), 1, metricAttrs...)
		if currentTotal > 0 && (currentPrompt > 0 || currentCandidate > 0) {
			RecordTokenUsage(context.Context(ctx), currentPrompt, currentCandidate, metricAttrs...)
		}
	}
}

func mergeUsageTotals(prevPrompt, prevCandidate, prevTotal, currentPrompt, currentCandidate, currentTotal int64) (int64, int64, int64) {
	if currentTotal == 0 && (currentPrompt > 0 || currentCandidate > 0) {
		currentTotal = currentPrompt + currentCandidate
	}

	return prevPrompt + currentPrompt, prevCandidate + currentCandidate, prevTotal + currentTotal
}

// BeforeTool is a lightweight debug-only callback.
// Tool span metrics and token estimation are handled in span processor / translator paths.
func (p *adkObservabilityPlugin) BeforeTool(ctx tool.Context, tool tool.Tool, args map[string]any) (map[string]any, error) {
	log.Debug("BeforeTool",
		"InvocationID", ctx.InvocationID(), "SessionID", ctx.SessionID(), "UserID", ctx.UserID(), "AgentName", ctx.AgentName(), "AppName", ctx.AppName(),
		"ToolName", tool.Name(), "ToolArgs", args)
	return nil, nil
}

// AfterTool is a lightweight debug-only callback.
// Tool span metrics and token estimation are handled in span processor / translator paths.
func (p *adkObservabilityPlugin) AfterTool(ctx tool.Context, tool tool.Tool, args map[string]any, result map[string]any, err error) (map[string]any, error) {
	log.Debug("AfterTool",
		"InvocationID", ctx.InvocationID(), "SessionID", ctx.SessionID(), "UserID", ctx.UserID(), "AgentName", ctx.AgentName(), "AppName", ctx.AppName(),
		"ToolName", tool.Name(), "ToolArgs", args, "ToolResult", result, "ToolError", err)

	return nil, nil
}

// BeforeAgent is called before an agent execution.
// This is the primary trace-bridging point for adk trace -> veadk invocation trace.
// BeforeModel keeps an idempotent bridge as a secondary safety net.
func (p *adkObservabilityPlugin) BeforeAgent(ctx agent.CallbackContext) (*genai.Content, error) {
	log.Debug("BeforeAgent",
		"InvocationID", ctx.InvocationID(), "SessionID", ctx.SessionID(), "UserID", ctx.UserID(), "AgentName", ctx.AgentName(), "AppName", ctx.AppName())
	p.tryBridgeTraceMappingFromCallback(ctx, "BeforeAgent")
	return nil, nil
}

func (p *adkObservabilityPlugin) tryBridgeTraceMappingFromCallback(ctx agent.CallbackContext, stage string) {
	adkSC := trace.SpanFromContext(context.Context(ctx)).SpanContext()
	veadkInvocationSC, ok := getInvocationSpanContextFromState(ctx.State())
	if !ok {
		log.Debug("Skip trace mapping bridge: invocation span missing in state", "stage", stage)
		return
	}

	if registerTraceMappingIfPossible(GetRegistry(), adkSC, veadkInvocationSC) {
		log.Debug("Bridged adk trace to veadk invocation trace",
			"stage", stage,
			"adk_trace_id", adkSC.TraceID().String(),
			"veadk_trace_id", veadkInvocationSC.TraceID().String(),
		)
	}
}

func registerTraceMappingIfPossible(registry *TraceRegistry, adkSC, veadkSC trace.SpanContext) bool {
	if registry == nil || !adkSC.IsValid() || !veadkSC.IsValid() {
		return false
	}
	registry.RegisterTraceMapping(adkSC.TraceID(), veadkSC.TraceID())
	return true
}

func getInvocationSpanContextFromState(state session.State) (trace.SpanContext, bool) {
	if s, _ := state.Get(stateKeyInvocationSpan); s != nil {
		if span, ok := s.(trace.Span); ok {
			sc := span.SpanContext()
			if sc.IsValid() {
				return sc, true
			}
		}
	}
	return trace.SpanContext{}, false
}

// AfterAgent is called after an agent execution.
func (p *adkObservabilityPlugin) AfterAgent(ctx agent.CallbackContext) (*genai.Content, error) {
	log.Debug("AfterAgent",
		"InvocationID", ctx.InvocationID(), "SessionID", ctx.SessionID(), "UserID", ctx.UserID(), "AgentName", ctx.AgentName(), "AppName", ctx.AppName())
	return nil, nil
}

func (p *adkObservabilityPlugin) getSpanMetadata(state session.State) *spanMetadata {
	val, _ := state.Get(stateKeyMetadata)
	if meta, ok := val.(*spanMetadata); ok {
		return meta
	}
	return &spanMetadata{}
}

func (p *adkObservabilityPlugin) storeSpanMetadata(state session.State, meta *spanMetadata) {
	_ = state.Set(stateKeyMetadata, meta)
}

const (
	stateKeyInvocationSpan = "veadk.observability.invocation_span"

	stateKeyMetadata        = "veadk.observability.metadata"
	stateKeyStreamingOutput = "veadk.observability.streaming_output"
)

// spanMetadata groups various observational data points in a single structure
// to keep the ADK State clean.
type spanMetadata struct {
	StartTime           time.Time
	PromptTokens        int64
	CandidateTokens     int64
	TotalTokens         int64
	PrevPromptTokens    int64
	PrevCandidateTokens int64
	PrevTotalTokens     int64
	ModelName           string
}
