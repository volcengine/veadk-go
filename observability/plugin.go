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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
	// 1. Start the 'invocation' span
	newCtx, span := p.tracer.Start(context.Context(ctx), SpanInvocation, trace.WithSpanKind(trace.SpanKindServer))
	log.Debug("BeforeRun created a new invocation span", "span", span.SpanContext())

	// Register internal ADK run span ID -> our veadk invocation span context.
	adkSpan := trace.SpanFromContext(context.Context(ctx))
	if adkSpan.SpanContext().IsValid() {
		GetRegistry().RegisterRunMapping(adkSpan.SpanContext().SpanID(), adkSpan.SpanContext().TraceID(), span.SpanContext(), span)
	}

	// 2. Store in state for AfterRun and children
	_ = ctx.Session().State().Set(stateKeyInvocationSpan, span)
	_ = ctx.Session().State().Set(stateKeyInvocationCtx, newCtx)

	setCommonAttributes(newCtx, span)
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
				elapsed := time.Since(meta.StartTime).Seconds()
				metricAttrs := []attribute.KeyValue{
					attribute.String("gen_ai_operation_name", "chain"),
					attribute.String("gen_ai_operation_type", "workflow"),
					attribute.String("gen_ai.system", GetModelProvider(context.Context(ctx))),
				}
				if p.isMetricsEnabled() {
					RecordOperationDuration(context.Background(), elapsed, metricAttrs...)
					RecordAPMPlusSpanLatency(context.Background(), elapsed, metricAttrs...)
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
	parentCtx := context.Context(ctx)

	if actx, _ := ctx.State().Get(stateKeyInvokeAgentCtx); actx != nil {
		parentCtx = actx.(context.Context)
		log.Debug("BeforeModel get a parent invoke_agent ctx from state", "parentCtx", parentCtx)
	} else if ictx, _ := ctx.State().Get(stateKeyInvocationCtx); ictx != nil {
		parentCtx = ictx.(context.Context)
		log.Debug("BeforeModel get a parent invocation ctx from state", "parentCtx", parentCtx)
	}

	// 2. Start our OWN span to cover the full duration of the call (including streaming).
	// ADK's "call_llm" span will be closed prematurely by the framework on the first chunk.
	// Align with Python: name is "call_llm"
	newCtx, span := p.tracer.Start(parentCtx, SpanCallLLM)
	log.Debug("BeforeModel created a span", "span", span.SpanContext(), "is_recording", span.IsRecording())
	_ = ctx.State().Set(stateKeyStreamingSpan, span)

	adkSpan := trace.SpanFromContext(context.Context(ctx))
	if adkSpan.SpanContext().IsValid() { // Register google's ADK span (currently not implemented) -> our veadk span context.
		GetRegistry().RegisterLLMMapping(adkSpan.SpanContext().SpanID(), adkSpan.SpanContext().TraceID(), span.SpanContext())
	}

	// Group metadata in a single structure for state storage
	meta := p.getSpanMetadata(ctx.State())
	meta.StartTime = time.Now()
	meta.PrevPromptTokens = meta.PromptTokens
	meta.PrevCandidateTokens = meta.CandidateTokens
	meta.PrevTotalTokens = meta.TotalTokens
	meta.ModelName = req.Model
	p.storeSpanMetadata(ctx.State(), meta)

	// Link back to the ADK internal span if it's there.
	// This records the ID of the span started by the ADK framework, which we
	// often bypass to maintain a cleaner hierarchy in our veadk spans.
	adkSpan = trace.SpanFromContext(context.Context(ctx))
	if adkSpan.SpanContext().IsValid() {
		span.SetAttributes(attribute.String("adk.internal_span_id", adkSpan.SpanContext().SpanID().String()))
	}

	setCommonAttributes(newCtx, span)
	// Set GenAI standard span attributes
	setLLMAttributes(span)

	// Record request attributes
	p.setLLMRequestAttributes(ctx, span, req)

	// Capture messages in GenAI format for the span
	messages := p.extractMessages(req)
	var msgAttrs []attribute.KeyValue
	messagesJSON, err := json.Marshal(messages)
	if err == nil {
		msgAttrs = append(msgAttrs, attribute.String(AttrGenAIMessages, string(messagesJSON)))
	}

	// Flatten messages for gen_ai.prompt.[n] attributes (alignment with python)
	msgAttrs = append(msgAttrs, p.flattenPrompt(messages)...)

	// Add input.value (standard for some collectors)
	msgAttrs = append(msgAttrs, attribute.String(AttrGenAIInput, string(messagesJSON)))

	msgAttrs = append(msgAttrs, attribute.String(AttrInputValue, string(messagesJSON)))

	span.SetAttributes(msgAttrs...)

	// Add gen_ai.messages events (system, user, tool, assistant) aligned with Python
	p.addMessageEvents(span, ctx, req)

	// Add gen_ai.content.prompt event (OTEL GenAI convention)
	span.AddEvent(EventGenAIContentPrompt, trace.WithAttributes(
		attribute.String(AttrGenAIPrompt, string(messagesJSON)),
		attribute.String(AttrGenAIInput, string(messagesJSON)),
	))

	return nil, nil
}

func (p *adkObservabilityPlugin) setLLMRequestAttributes(ctx agent.CallbackContext, span trace.Span, req *model.LLMRequest) {
	attrs := []attribute.KeyValue{
		attribute.String(AttrGenAIRequestModel, req.Model),
		attribute.String(AttrGenAIRequestType, "chat"), // Default to chat
		attribute.String(AttrGenAISystem, GetModelProvider(context.Context(ctx))),
	}

	if req.Config != nil {
		if req.Config.Temperature != nil {
			attrs = append(attrs, attribute.Float64(AttrGenAIRequestTemperature, float64(*req.Config.Temperature)))
		}
		if req.Config.TopP != nil {
			attrs = append(attrs, attribute.Float64(AttrGenAIRequestTopP, float64(*req.Config.TopP)))
		}
		if req.Config.MaxOutputTokens > 0 {
			attrs = append(attrs, attribute.Int64(AttrGenAIRequestMaxTokens, int64(req.Config.MaxOutputTokens)))
		}

		funcIdx := 0
		for _, tool := range req.Config.Tools {
			if tool.FunctionDeclarations != nil {
				for _, fn := range tool.FunctionDeclarations {
					prefix := fmt.Sprintf("gen_ai.request.functions.%d.", funcIdx) // Simplified indexing
					attrs = append(attrs, attribute.String(prefix+"name", fn.Name))
					attrs = append(attrs, attribute.String(prefix+"description", fn.Description))
					if fn.Parameters != nil {
						paramsJSON, _ := json.Marshal(fn.Parameters)
						attrs = append(attrs, attribute.String(prefix+"parameters", string(paramsJSON)))
					}
					funcIdx++
				}
			}
		}
	}
	span.SetAttributes(attrs...)
}

// AfterModel is called after the LLM returns.
func (p *adkObservabilityPlugin) AfterModel(ctx agent.CallbackContext, resp *model.LLMResponse, err error) (*model.LLMResponse, error) {
	// 1. Get our managed span from state
	s, _ := ctx.State().Get(stateKeyStreamingSpan)
	if s == nil {
		log.Warn("AfterModel: No streaming span found in state")
		return nil, nil
	}
	span := s.(trace.Span)
	// log.Debug("AfterModel get a trace span from state", "span", span.SpanContext(), "type", fmt.Sprintf("%T", s), "is_recording", span.IsRecording())

	// 2. Wrap the cleanup to ensure span is always ended on error or final chunk.
	// ADK calls AfterModel for EVERY chunk in a stream.
	// resp.Partial is true for intermediate chunks, false for the final one.
	defer func() {
		if err != nil || (resp != nil && !resp.Partial) {
			if span.IsRecording() {
				log.Debug("AfterModel got a partial response", "span", span.SpanContext())
				span.End()
			}
		}
	}()

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		// Record Exceptions metric
		if p.isMetricsEnabled() {
			meta := p.getSpanMetadata(ctx.State())
			metricAttrs := []attribute.KeyValue{
				attribute.String(AttrGenAISystem, GetModelProvider(context.Context(ctx))),
				attribute.String("gen_ai_response_model", meta.ModelName),
				attribute.String("gen_ai_operation_name", "chat"),
				attribute.String("gen_ai_operation_type", "llm"),
				attribute.String("error_type", "error"), // Simple error type
			}
			RecordExceptions(context.Context(ctx), 1, metricAttrs...)
		}
		return nil, nil
	}

	if resp == nil {
		return nil, nil
	}

	if !span.IsRecording() {
		log.Warn("AfterModel: span is not recording", "span", span)
		// Even if not recording, we should still accumulate content for metrics/logs
	}

	// Record responding model
	meta := p.getSpanMetadata(ctx.State())
	// Try to get confirmation from response metadata first (passed from sdk)
	var finalModelName string
	if resp.CustomMetadata != nil {
		if m, ok := resp.CustomMetadata["response_model"].(string); ok {
			finalModelName = m
		}
	}
	// Fallback to request model name if not present in response
	if finalModelName == "" {
		finalModelName = meta.ModelName
	}
	if finalModelName != "" {
		span.SetAttributes(attribute.String(AttrGenAIResponseModel, finalModelName))
	}

	if resp.UsageMetadata != nil {
		p.handleUsage(ctx, span, resp, resp.Partial, finalModelName)
	}

	//  Capture tool calls from response to link future tool spans
	if resp.Content != nil {
		adkSpan := trace.SpanFromContext(context.Context(ctx))
		adkTraceID := trace.TraceID{}
		if adkSpan.SpanContext().IsValid() {
			adkTraceID = adkSpan.SpanContext().TraceID()
		}

		for _, part := range resp.Content.Parts {
			if part.FunctionCall != nil && part.FunctionCall.ID != "" {
				log.Debug(" AfterModel, registering ToolCallID mapping", "tool_call_id", part.FunctionCall.ID, "parent_llm_span_id", span.SpanContext())
				GetRegistry().RegisterToolCallMapping(part.FunctionCall.ID, adkTraceID, span.SpanContext())
			}
		}
	}

	if resp.FinishReason != "" {
		span.SetAttributes(attribute.String(AttrGenAIResponseFinishReason, string(resp.FinishReason)))
	}

	// Record response content
	var currentAcc *genai.Content
	cached, _ := ctx.State().Get(stateKeyStreamingOutput)
	if cached != nil {
		currentAcc = cached.(*genai.Content)
	}

	// ---------------------------------------------------------
	// Metrics: Time to First Token (Streaming Only)
	// ---------------------------------------------------------
	p.recordTimeToFirstToken(ctx, resp, meta, currentAcc, finalModelName)

	if resp.Content != nil {
		currentAcc = p.processStreamingChunk(ctx, resp, currentAcc)
	}

	// For streaming, we update the span attributes with what we have so far
	var fullText string
	if currentAcc != nil {
		fullText = p.updateStreamingSpanAttributes(span, currentAcc)
	}

	// Metrics: Time to Generate (Streaming Only) & Time Per Output Token
	p.recordStreamingGenerationMetrics(ctx, resp, meta, currentAcc, finalModelName)

	// If this is the final chunk, add the completion event
	if !resp.Partial && currentAcc != nil {
		contentJSON, _ := json.Marshal(currentAcc)
		span.AddEvent(EventGenAIContentCompletion, trace.WithAttributes(
			attribute.String(AttrGenAICompletion, string(contentJSON)),
			attribute.String(AttrGenAIOutput, fullText),
		))

		// Add gen_ai.choice event (aligned with Python)
		p.addChoiceEvents(span, currentAcc)
	}

	if !resp.Partial {
		// Record Operation Duration and Latency
		p.recordFinalResponseMetrics(ctx, meta, finalModelName)
	}

	return nil, nil
}

func (p *adkObservabilityPlugin) recordTimeToFirstToken(ctx agent.CallbackContext, resp *model.LLMResponse, meta *spanMetadata, currentAcc *genai.Content, finalModelName string) {
	if resp.Partial && currentAcc == nil && resp.Content != nil {
		// This is the very first chunk
		if !meta.StartTime.IsZero() {
			meta.FirstTokenTime = time.Now()
			p.storeSpanMetadata(ctx.State(), meta)

			if p.isMetricsEnabled() {
				// Record streaming time to first token
				latency := time.Since(meta.StartTime).Seconds()
				metricAttrs := []attribute.KeyValue{
					attribute.String(AttrGenAISystem, GetModelProvider(context.Context(ctx))),
					attribute.String("gen_ai_response_model", finalModelName),
					attribute.String("gen_ai_operation_name", "chat"),
					attribute.String("gen_ai_operation_type", "llm"),
				}
				RecordStreamingTimeToFirstToken(context.Context(ctx), latency, metricAttrs...)
			}
		} else {
			log.Warn("didn't find the start time of span", "meta", meta)
		}
	}
}

func (p *adkObservabilityPlugin) processStreamingChunk(ctx agent.CallbackContext, resp *model.LLMResponse, currentAcc *genai.Content) *genai.Content {
	if currentAcc == nil {
		currentAcc = &genai.Content{Role: resp.Content.Role}
		if currentAcc.Role == "" {
			currentAcc.Role = "model"
		}
	}

	// If this is the final response, our implementation (like OpenAI) often sends the full content.
	// We clear our previous accumulation to avoid duplication in the span attributes.
	// We only do this if the final response actually contains content.
	if !resp.Partial && resp.Content != nil && len(resp.Content.Parts) > 0 {
		currentAcc.Parts = nil
	}

	// Accumulate parts with merging of adjacent text
	for _, part := range resp.Content.Parts {
		// If it's a text part, try to merge with the last part if that was also text
		if part.Text != "" && len(currentAcc.Parts) > 0 {
			lastPart := currentAcc.Parts[len(currentAcc.Parts)-1]
			if lastPart.Text != "" && lastPart.FunctionCall == nil && lastPart.FunctionResponse == nil && lastPart.InlineData == nil {
				lastPart.Text += part.Text
				continue
			}
		}

		// Otherwise append as a new part
		newPart := &genai.Part{}
		*newPart = *part
		currentAcc.Parts = append(currentAcc.Parts, newPart)
	}
	_ = ctx.State().Set(stateKeyStreamingOutput, currentAcc)
	return currentAcc
}

func (p *adkObservabilityPlugin) updateStreamingSpanAttributes(span trace.Span, currentAcc *genai.Content) string {
	// Set output.value to the cumulative text (parity with python)
	var textParts strings.Builder
	textParts.Grow(len(currentAcc.Parts) * 4)
	for _, p := range currentAcc.Parts {
		if p.Text != "" {
			textParts.WriteString(p.Text)
		}
	}
	fullText := textParts.String()
	span.SetAttributes(attribute.String(AttrGenAIOutput, fullText))

	// Add output.value for full JSON representation
	if contentJSON, err := json.Marshal(currentAcc); err == nil {
		span.SetAttributes(attribute.String("output.value", string(contentJSON)))
	}

	// Also set the structured GenAI attributes
	span.SetAttributes(p.flattenCompletion(currentAcc)...)
	return fullText
}

func (p *adkObservabilityPlugin) recordStreamingGenerationMetrics(ctx agent.CallbackContext, resp *model.LLMResponse, meta *spanMetadata, currentAcc *genai.Content, finalModelName string) {
	if !resp.Partial && currentAcc != nil {
		if !meta.StartTime.IsZero() {
			// Time Per Output Token
			// Only valid if we have output tokens and we tracked first token time
			if p.isMetricsEnabled() {
				if meta.CandidateTokens > 0 {
					generateDuration := time.Since(meta.StartTime).Seconds()
					metricAttrs := []attribute.KeyValue{
						attribute.String(AttrGenAISystem, GetModelProvider(context.Context(ctx))),
						attribute.String("gen_ai_response_model", finalModelName),
						attribute.String("gen_ai_operation_name", "chat"),
						attribute.String("gen_ai_operation_type", "llm"),
					}
					RecordStreamingTimeToGenerate(context.Context(ctx), generateDuration, metricAttrs...)

					if generateDuration > 0 {
						timePerToken := generateDuration / float64(meta.CandidateTokens)
						RecordStreamingTimePerOutputToken(context.Context(ctx), timePerToken, metricAttrs...)
					}
				}
			}
		}
	}
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

func (p *adkObservabilityPlugin) handleUsage(ctx agent.CallbackContext, span trace.Span, resp *model.LLMResponse, isStream bool, modelName string) {
	meta := p.getSpanMetadata(ctx.State())

	// 1. Get current call usage
	currentPrompt := int64(resp.UsageMetadata.PromptTokenCount)
	currentCandidate := int64(resp.UsageMetadata.CandidatesTokenCount)
	currentTotal := int64(resp.UsageMetadata.TotalTokenCount)

	if currentTotal == 0 && (currentPrompt > 0 || currentCandidate > 0) {
		currentTotal = currentPrompt + currentCandidate
	}

	// 2. New session total = previous calls total + current call's (latest) usage
	// (Note: in streaming, currentCall usage is cumulative for this call)
	meta.PromptTokens = meta.PrevPromptTokens + currentPrompt
	meta.CandidateTokens = meta.PrevCandidateTokens + currentCandidate
	meta.TotalTokens = meta.PrevTotalTokens + currentTotal

	// 3. Update session-wide totals
	p.storeSpanMetadata(ctx.State(), meta)

	// 4. Set attributes on the current LLM span (only current call's usage)
	attrs := make([]attribute.KeyValue, 0, 7)
	if currentPrompt > 0 {
		attrs = append(attrs, attribute.Int64(AttrGenAIUsageInputTokens, currentPrompt))
		attrs = append(attrs, attribute.Int64(AttrGenAIResponsePromptTokenCount, currentPrompt))
	}
	if currentCandidate > 0 {
		attrs = append(attrs, attribute.Int64(AttrGenAIUsageOutputTokens, currentCandidate))
		attrs = append(attrs, attribute.Int64(AttrGenAIResponseCandidatesTokenCount, currentCandidate))
	}
	if currentTotal > 0 {
		attrs = append(attrs, attribute.Int64(AttrGenAIUsageTotalTokens, currentTotal))
	}

	if resp.UsageMetadata != nil {
		if resp.UsageMetadata.CachedContentTokenCount > 0 {
			attrs = append(attrs, attribute.Int64(AttrGenAIUsageCacheReadInputTokens, int64(resp.UsageMetadata.CachedContentTokenCount)))
		}
		// Always set cache creation to 0 if not provided, for parity with python
		attrs = append(attrs, attribute.Int64(AttrGenAIUsageCacheCreationInputTokens, 0))
	}

	span.SetAttributes(attrs...)

	// Record metrics directly from the plugin logic
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

func (p *adkObservabilityPlugin) addMessageEvents(span trace.Span, ctx agent.CallbackContext, req *model.LLMRequest) {
	// 1. System Message
	if req.Config != nil && req.Config.SystemInstruction != nil {
		sysContent := ""
		for _, part := range req.Config.SystemInstruction.Parts {
			if part.Text != "" {
				sysContent += part.Text
			}
		}
		if sysContent != "" {
			span.AddEvent("gen_ai.system.message", trace.WithAttributes(
				attribute.String("role", "system"),
				attribute.String("content", sysContent),
			))
		}
	}

	// 2. User, Tool, Assistant Messages from History
	for _, content := range req.Contents {
		if content.Role == "user" {
			userEventAttrs := []attribute.KeyValue{
				attribute.String("role", "user"),
			}

			// Check if it's a tool response (which comes in as 'user' role in Gemini/ADK but logically is tool message)
			// Actually ADK structure:
			// User inputs -> Role: user
			// Tool Outputs -> Role: user (FunctionResponse) or "tool" depending on model?
			// Python implementation checks `part.function_response`.

			hasToolResponse := false
			for _, part := range content.Parts {
				if part.FunctionResponse != nil {
					hasToolResponse = true
					// Emit separate event for each tool response
					span.AddEvent("gen_ai.tool.message", trace.WithAttributes(
						attribute.String("role", "tool"),
						attribute.String("id", part.FunctionResponse.ID),
						attribute.String("content", safeMarshal(part.FunctionResponse.Response)),
					))
				}
			}

			if hasToolResponse {
				continue
			}

			// Normal User Message
			for i, part := range content.Parts {
				if part.Text != "" {
					if len(content.Parts) == 1 {
						userEventAttrs = append(userEventAttrs, attribute.String("content", sanitizeUTF8(part.Text)))
					} else {
						userEventAttrs = append(userEventAttrs, attribute.String("parts."+strconv.Itoa(i)+".type", "text"))
						userEventAttrs = append(userEventAttrs, attribute.String("parts."+strconv.Itoa(i)+".text", sanitizeUTF8(part.Text)))
					}
				}
				if part.InlineData != nil && len(part.InlineData.Data) > 0 {
					// Image/Blob handling
					prefix := "parts." + strconv.Itoa(i)
					if len(content.Parts) == 1 {
						prefix = "parts.0"
					}
					userEventAttrs = append(userEventAttrs, attribute.String(prefix+".type", "image_url"))
					// MIME type or display name mapping
					userEventAttrs = append(userEventAttrs, attribute.String(prefix+".image_url.url", part.InlineData.MIMEType))
				}
			}
			span.AddEvent("gen_ai.user.message", trace.WithAttributes(userEventAttrs...))

		} else if content.Role == "model" {
			assistantEventAttrs := []attribute.KeyValue{
				attribute.String("role", "assistant"),
			}
			for i, part := range content.Parts {
				if part.Text != "" {
					assistantEventAttrs = append(assistantEventAttrs, attribute.String("parts."+strconv.Itoa(i)+".type", "text"))
					assistantEventAttrs = append(assistantEventAttrs, attribute.String("parts."+strconv.Itoa(i)+".text", sanitizeUTF8(part.Text)))
				}
				if part.FunctionCall != nil {
					// Tool Calls
					prefix := "tool_calls.0" // Assuming single tool call per part or simplifying
					assistantEventAttrs = append(assistantEventAttrs, attribute.String(prefix+".id", part.FunctionCall.ID))
					assistantEventAttrs = append(assistantEventAttrs, attribute.String(prefix+".type", "function"))
					assistantEventAttrs = append(assistantEventAttrs, attribute.String(prefix+".function.name", part.FunctionCall.Name))
					assistantEventAttrs = append(assistantEventAttrs, attribute.String(prefix+".function.arguments", safeMarshal(part.FunctionCall.Args)))
				}
			}
			span.AddEvent("gen_ai.assistant.message", trace.WithAttributes(assistantEventAttrs...))
		}
	}
}

func (p *adkObservabilityPlugin) addChoiceEvents(span trace.Span, content *genai.Content) {
	for i, part := range content.Parts {
		attrs := make([]attribute.KeyValue, 0, 2)
		if part.Text != "" {
			attrs = append(attrs, attribute.String("message.parts."+strconv.Itoa(i)+".type", "text"))
			attrs = append(attrs, attribute.String("message.parts."+strconv.Itoa(i)+".text", sanitizeUTF8(part.Text)))
		}
		if len(attrs) > 0 {
			span.AddEvent("gen_ai.choice", trace.WithAttributes(attrs...))
		}
	}
}

// extractMessages converts ADK model.LLMRequest contents into a JSON-compatible message list.
func (p *adkObservabilityPlugin) extractMessages(req *model.LLMRequest) []map[string]any {
	var messages []map[string]any
	for _, content := range req.Contents {
		role := content.Role
		if role == "model" {
			role = "assistant"
		}

		msg := map[string]any{
			"role": role,
		}

		var textParts []string
		var toolCalls []map[string]any
		var toolResponses []map[string]any

		for _, part := range content.Parts {
			if part.Text != "" {
				textParts = append(textParts, sanitizeUTF8(part.Text))
			}
			if part.FunctionCall != nil {
				toolCalls = append(toolCalls, map[string]any{
					"id":   part.FunctionCall.ID,
					"type": "function",
					"function": map[string]any{
						"name":      part.FunctionCall.Name,
						"arguments": safeMarshal(part.FunctionCall.Args),
					},
				})
			}
			if part.FunctionResponse != nil {
				toolResponses = append(toolResponses, map[string]any{
					"id":      part.FunctionResponse.ID,
					"name":    part.FunctionResponse.Name,
					"content": safeMarshal(part.FunctionResponse.Response),
				})
			}
		}

		if len(textParts) > 0 {
			msg["content"] = strings.Join(textParts, "")
		}
		if len(toolCalls) > 0 {
			msg["tool_calls"] = toolCalls
		}
		if len(toolResponses) > 0 {
			// In standard GenAI, tool responses are often represented separate messages or differently.
			// Alignment with veadk-python usually means following their structure.
			msg["tool_responses"] = toolResponses
		}

		messages = append(messages, msg)
	}
	return messages
}

func (p *adkObservabilityPlugin) flattenPrompt(messages []map[string]any) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	idx := 0
	for _, msg := range messages {
		// In Python, each piece of content/part increments the index.
		// Since we already merged text parts in extractMessages, we just process each message here.
		// If we wanted exact parity for multi-part messages, we'd need to change extractMessages.
		// For now, this is a good approximation that matches the role/content flat structure.
		prefix := "gen_ai.prompt." + strconv.Itoa(idx)
		if role, ok := msg["role"].(string); ok {
			attrs = append(attrs, attribute.String(prefix+".role", role))
		}
		if content, ok := msg["content"].(string); ok {
			attrs = append(attrs, attribute.String(prefix+".content", content))
		}

		if toolCalls, ok := msg["tool_calls"].([]map[string]any); ok {
			for j, tc := range toolCalls {
				tcPrefix := prefix + ".tool_calls." + strconv.Itoa(j)
				if id, ok := tc["id"].(string); ok {
					attrs = append(attrs, attribute.String(tcPrefix+".id", id))
				}
				if t, ok := tc["type"].(string); ok {
					attrs = append(attrs, attribute.String(tcPrefix+".type", t))
				}
				if fn, ok := tc["function"].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok {
						attrs = append(attrs, attribute.String(tcPrefix+".function.name", name))
					}
					if args, ok := fn["arguments"].(string); ok {
						attrs = append(attrs, attribute.String(tcPrefix+".function.arguments", args))
					}
				}
			}
		}

		if toolResponses, ok := msg["tool_responses"].([]map[string]any); ok {
			for j, tr := range toolResponses {
				trPrefix := prefix + ".tool_responses." + strconv.Itoa(j)
				if id, ok := tr["id"].(string); ok {
					attrs = append(attrs, attribute.String(trPrefix+".id", id))
				}
				if name, ok := tr["name"].(string); ok {
					attrs = append(attrs, attribute.String(trPrefix+".name", name))
				}
				if content, ok := tr["content"].(string); ok {
					attrs = append(attrs, attribute.String(trPrefix+".content", content))
				}
			}
		}
		idx++
	}
	return attrs
}

func (p *adkObservabilityPlugin) flattenCompletion(content *genai.Content) []attribute.KeyValue {
	var attrs []attribute.KeyValue

	role := content.Role
	if role == "model" {
		role = "assistant"
	}

	for idx, part := range content.Parts {
		prefix := "gen_ai.completion." + strconv.Itoa(idx)
		attrs = append(attrs, attribute.String(prefix+".role", role))

		if part.Text != "" {
			attrs = append(attrs, attribute.String(prefix+".content", sanitizeUTF8(part.Text)))
		}
		if part.FunctionCall != nil {
			tcPrefix := prefix + ".tool_calls.0"
			attrs = append(attrs, attribute.String(tcPrefix+".id", part.FunctionCall.ID))
			attrs = append(attrs, attribute.String(tcPrefix+".type", "function"))
			attrs = append(attrs, attribute.String(tcPrefix+".function.name", part.FunctionCall.Name))
			attrs = append(attrs, attribute.String(tcPrefix+".function.arguments", safeMarshal(part.FunctionCall.Args)))
		}
	}

	return attrs
}

// BeforeTool is called before a tool is executed.
func (p *adkObservabilityPlugin) BeforeTool(ctx tool.Context, tool tool.Tool, args map[string]any) (map[string]any, error) {
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
	agentName := ctx.AgentName()
	if agentName == "" {
		agentName = FallbackAgentName
	}

	// 1. Get the parent context from state to maintain hierarchy
	parentCtx := context.Context(ctx)
	if ictx, _ := ctx.State().Get(stateKeyInvocationCtx); ictx != nil {
		parentCtx = ictx.(context.Context)
	}

	// 2. Start the 'invoke_agent' span manually.
	// Since we can't easily wrap the Agent interface due to internal methods,
	// we use the plugin to start our span.
	spanName := SpanInvokeAgent + " " + agentName
	newCtx, span := p.tracer.Start(parentCtx, spanName)

	// Register internal ADK's agent span ID -> our veadk agent span context.
	adkSpan := trace.SpanFromContext(context.Context(ctx))
	if adkSpan.SpanContext().IsValid() {
		GetRegistry().RegisterAgentMapping(adkSpan.SpanContext().SpanID(), adkSpan.SpanContext().TraceID(), span.SpanContext())
	}

	// 3. Store in state for AfterAgent and children
	_ = ctx.State().Set(stateKeyInvokeAgentSpan, span)
	_ = ctx.State().Set(stateKeyInvokeAgentCtx, newCtx)

	// 4. Set attributes
	setCommonAttributes(newCtx, span)
	setWorkflowAttributes(span)
	setAgentAttributes(span, agentName)

	// Capture input if available (propagated from BeforeRun via state or context?)
	// Note: BeforeRun captures UserContent, but for nested agents, input might be passed differently.
	// For now, if UserContent is available in this context, log it.
	if userContent := ctx.UserContent(); userContent != nil {
		if jsonIn, err := json.Marshal(userContent); err == nil {
			val := string(jsonIn)
			span.SetAttributes(attribute.String(AttrGenAIInput, val))
		}
	}

	return nil, nil
}

// AfterAgent is called after an agent execution.
func (p *adkObservabilityPlugin) AfterAgent(ctx agent.CallbackContext) (*genai.Content, error) {
	// 1. End the span
	if s, _ := ctx.State().Get(stateKeyInvokeAgentSpan); s != nil {
		span := s.(trace.Span)
		if span.IsRecording() {
			// Try to capture output if available in state (propagated from AfterRun or internal execution)
			if cached, _ := ctx.State().Get(stateKeyStreamingOutput); cached != nil {
				if jsonOut, err := json.Marshal(cached); err == nil {
					val := string(jsonOut)
					span.SetAttributes(attribute.String(AttrGenAIOutput, val))
				}
			}
			span.End()
		}
	}
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

// sanitizeUTF8 removes or replaces invalid UTF-8 characters from a string
func sanitizeUTF8(s string) string {
	// If the string is already valid UTF-8, return it as is
	if len(s) == 0 {
		return s
	}

	// Replace invalid UTF-8 sequences with Unicode replacement character
	return string([]rune(s))
}

func safeMarshal(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}

	return string(b)
}

const (
	stateKeyInvocationSpan  = "veadk.observability.invocation_span"
	stateKeyInvocationCtx   = "veadk.observability.invocation_ctx"
	stateKeyInvokeAgentCtx  = "veadk.observability.invoke_agent_ctx"
	stateKeyInvokeAgentSpan = "veadk.observability.invoke_agent_span"

	stateKeyMetadata        = "veadk.observability.metadata"
	stateKeyStreamingOutput = "veadk.observability.streaming_output"
	stateKeyStreamingSpan   = "veadk.observability.streaming_span"
)

// spanMetadata groups various observational data points in a single structure
// to keep the ADK State clean.
type spanMetadata struct {
	StartTime           time.Time
	FirstTokenTime      time.Time
	PromptTokens        int64
	CandidateTokens     int64
	TotalTokens         int64
	PrevPromptTokens    int64
	PrevCandidateTokens int64
	PrevTotalTokens     int64
	ModelName           string
}
