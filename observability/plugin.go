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
	"encoding/json"
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
	log.Debug("BeforeRun created a new invocation span", "span", span.SpanContext())

	// Register internal ADK run span ID -> our veadk invocation span context.
	adkSpan := trace.SpanFromContext(context.Context(ctx))
	if adkSpan.SpanContext().IsValid() {
		GetRegistry().RegisterRunMapping(adkSpan.SpanContext().SpanID(), adkSpan.SpanContext().TraceID(), span.SpanContext(), span)
	}

	// 2. Store in state for AfterRun
	_ = ctx.Session().State().Set(stateKeyInvocationSpan, span)

	setCommonAttributesFromInvocation(ctx, span)
	setWorkflowAttributes(span)

	// Record start time for metrics
	meta := &spanMetadata{
		StartTime: time.Now(),
	}
	p.storeSpanMetadata(ctx.Session().State(), meta)

	// Capture input from UserContent
	if userContent := ctx.UserContent(); userContent != nil {
		if jsonIn, err := json.Marshal(userContent); err == nil {
			val := string(jsonIn)
			span.SetAttributes(
				attribute.String(AttrInputValue, val),
				attribute.String(AttrGenAIInput, val),
			)
		}
	}

	return nil, nil
}

// AfterRun is called after an agent run ends.
func (p *adkObservabilityPlugin) AfterRun(ctx agent.InvocationContext) {
	log.Debug("After Run", "InvocationID", ctx.InvocationID(), "SessionID", ctx.Session().ID(), "UserID", ctx.Session().UserID())
	// 1. End the span
	if s, _ := ctx.Session().State().Get(stateKeyInvocationSpan); s != nil {
		span := s.(trace.Span)
		log.Debug("AfterRun get a span from state", "span", span, "isRecording", span.IsRecording())

		if span.IsRecording() {
			// Capture final output if available
			if cached, _ := ctx.Session().State().Get(stateKeyStreamingOutput); cached != nil {
				if jsonOut, err := json.Marshal(cached); err == nil {
					val := string(jsonOut)
					span.SetAttributes(
						attribute.String(AttrOutputValue, val),
						attribute.String(AttrGenAIOutput, val),
					)
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
						attribute.String("gen_ai_operation_name", "chain"),
						attribute.String("gen_ai_operation_type", "workflow"),
						attribute.String("gen_ai.system", GetModelProvider(context.Context(ctx))),
					}
					RecordOperationDuration(context.Background(), elapsed, metricAttrs...)
					RecordAPMPlusSpanLatency(context.Background(), elapsed, metricAttrs...)

					if isAgentKitRuntime {
						agentKitsAttrs := []attribute.KeyValue{
							attribute.String("gen_ai_operation_name", "chain"),
							attribute.String("gen_ai_operation_type", "workflow"),
						}

						var errorCode string
						eventLen := ctx.Session().Events().Len()
						if eventLen > 0 {
							lastEvent := ctx.Session().Events().At(eventLen - 1)
							errorCode = lastEvent.ErrorCode
						}
						if errorCode != "" {
							agentKitsAttrs = append(agentKitsAttrs, attribute.String("error_type", errorCode))
						}
						RecordAgentKitDuration(context.Background(), elapsed, agentKitsAttrs...)
					}
				}
			}

			// Clean up from global map with delay to allow children to be exported.
			// Since we have multiple exporters, we wait long enough for all of them to finish.
			adkSpan := trace.SpanFromContext(context.Context(ctx))
			if adkSpan.SpanContext().IsValid() {
				id := adkSpan.SpanContext().SpanID()
				tid := adkSpan.SpanContext().TraceID()
				veadkTraceID := span.SpanContext().SpanID()
				GetRegistry().ScheduleCleanup(tid, id, veadkTraceID)
			}

			span.End()
		}
	}
}

// BeforeModel is called before the LLM is called.
func (p *adkObservabilityPlugin) BeforeModel(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
	log.Debug("BeforeModel",
		"InvocationID", ctx.InvocationID(), "SessionID", ctx.SessionID(), "UserID", ctx.UserID(), "AgentName", ctx.AgentName(), "AppName", ctx.AppName())
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
				attribute.String("gen_ai_operation_name", "chat"),
				attribute.String("gen_ai_operation_type", "llm"),
				attribute.String("error_type", "error"),
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
		p.handleUsageWithoutSpan(ctx, resp, finalModelName)
	}

	if resp.Content != nil {
		if !resp.Partial {
			_ = ctx.State().Set(stateKeyStreamingOutput, resp.Content)
		}

		parentSC := trace.SpanContext{}
		if s, _ := ctx.State().Get(stateKeyInvocationSpan); s != nil {
			if span, ok := s.(trace.Span); ok {
				parentSC = span.SpanContext()
			}
		}

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
			attribute.String("gen_ai_operation_name", "chat"),
			attribute.String("gen_ai_operation_type", "llm"),
		}
		if p.isMetricsEnabled() {
			RecordOperationDuration(context.Context(ctx), duration, metricAttrs...)
			RecordAPMPlusSpanLatency(context.Context(ctx), duration, metricAttrs...)
		}
	}
}

func (p *adkObservabilityPlugin) handleUsageWithoutSpan(ctx agent.CallbackContext, resp *model.LLMResponse, modelName string) {
	meta := p.getSpanMetadata(ctx.State())

	currentPrompt := int64(resp.UsageMetadata.PromptTokenCount)
	currentCandidate := int64(resp.UsageMetadata.CandidatesTokenCount)
	currentTotal := int64(resp.UsageMetadata.TotalTokenCount)

	if currentTotal == 0 && (currentPrompt > 0 || currentCandidate > 0) {
		currentTotal = currentPrompt + currentCandidate
	}

	meta.PromptTokens = meta.PrevPromptTokens + currentPrompt
	meta.CandidateTokens = meta.PrevCandidateTokens + currentCandidate
	meta.TotalTokens = meta.PrevTotalTokens + currentTotal
	p.storeSpanMetadata(ctx.State(), meta)

	if p.isMetricsEnabled() {
		metricAttrs := []attribute.KeyValue{
			attribute.String(AttrGenAISystem, GetModelProvider(ctx)),
			attribute.String("gen_ai_response_model", modelName),
			attribute.String("gen_ai_operation_name", "chat"),
			attribute.String("gen_ai_operation_type", "llm"),
		}
		RecordChatCount(context.Context(ctx), 1, metricAttrs...)
		if currentTotal > 0 && (currentPrompt > 0 || currentCandidate > 0) {
			RecordTokenUsage(context.Context(ctx), currentPrompt, currentCandidate, metricAttrs...)
		}
	}
}

// BeforeTool is called before a tool is executed.
func (p *adkObservabilityPlugin) BeforeTool(ctx tool.Context, tool tool.Tool, args map[string]any) (map[string]any, error) {
	log.Debug("BeforeTool",
		"InvocationID", ctx.InvocationID(), "SessionID", ctx.SessionID(), "UserID", ctx.UserID(), "AgentName", ctx.AgentName(), "AppName", ctx.AppName())
	// Note: In Google ADK-go, the execute_tool span is often not available in the context at this stage.
	// We rely on VeADKTranslatedExporter (translator.go) to reconstruct tool attributes from the
	// span after it is ended and exported.

	// Maintain metadata for metrics calculation
	meta := p.getSpanMetadata(ctx.State())
	meta.StartTime = time.Now()
	p.storeSpanMetadata(ctx.State(), meta)
	return nil, nil
}

// AfterTool is called after a tool is executed.
func (p *adkObservabilityPlugin) AfterTool(ctx tool.Context, tool tool.Tool, args, result map[string]any, err error) (map[string]any, error) {
	log.Debug("AfterTool",
		"InvocationID", ctx.InvocationID(), "SessionID", ctx.SessionID(), "UserID", ctx.UserID(), "AgentName", ctx.AgentName(), "AppName", ctx.AppName())
	// Metrics recording only
	meta := p.getSpanMetadata(ctx.State())
	if !meta.StartTime.IsZero() {
		duration := time.Since(meta.StartTime).Seconds()
		metricAttrs := []attribute.KeyValue{
			attribute.String("gen_ai_operation_name", tool.Name()),
			attribute.String("gen_ai_operation_type", "tool"),
			attribute.String(AttrGenAISystem, GetModelProvider(context.Context(ctx))),
		}
		if p.isMetricsEnabled() {
			RecordOperationDuration(context.Background(), duration, metricAttrs...)
			RecordAPMPlusSpanLatency(context.Background(), duration, metricAttrs...)
		}

		if p.isMetricsEnabled() {
			// Tool Token Usage (Estimated)

			// Input Chars
			var inputChars int64
			if argsJSON, err := json.Marshal(args); err == nil {
				inputChars = int64(len(argsJSON))
			}

			// Output Chars
			var outputChars int64
			if resultJSON, err := json.Marshal(result); err == nil {
				outputChars = int64(len(resultJSON))
			}

			if inputChars > 0 {
				RecordAPMPlusToolTokenUsage(context.Background(), inputChars/4, append(metricAttrs, attribute.String("token_type", "input"))...)
			}
			if outputChars > 0 {
				RecordAPMPlusToolTokenUsage(context.Background(), outputChars/4, append(metricAttrs, attribute.String("token_type", "output"))...)
			}
		}
	}

	return nil, nil
}

// BeforeAgent is called before an agent execution.
func (p *adkObservabilityPlugin) BeforeAgent(ctx agent.CallbackContext) (*genai.Content, error) {
	log.Debug("BeforeAgent",
		"InvocationID", ctx.InvocationID(), "SessionID", ctx.SessionID(), "UserID", ctx.UserID(), "AgentName", ctx.AgentName(), "AppName", ctx.AppName())
	return nil, nil
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
