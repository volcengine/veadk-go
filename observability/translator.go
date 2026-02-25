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
	"strings"
	"time"

	"github.com/volcengine/veadk-go/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

var (
	// ADKAttributeKeyMap maps ADK-specific attributes to standard GenAI attributes.
	ADKAttributeKeyMap = map[string]string{
		ADKAttrLLMRequestName:   AttrInputValue,
		ADKAttrLLMResponseName:  AttrOutputValue,
		ADKAttrToolCallArgsName: AttrGenAIToolInput,
		ADKAttrToolResponseName: AttrGenAIToolOutput,
		ADKAttrInvocationID:     AttrGenAIInvocationID,
		ADKAttrSessionID:        AttrGenAISessionID,
	}
)

// VeADKTranslatedExporter wraps a SpanExporter and remaps ADK attributes to standard fields.
type VeADKTranslatedExporter struct {
	trace.SpanExporter
}

// ExportSpans filters and translates spans before exporting them to the underlying exporter.
func (e *VeADKTranslatedExporter) ExportSpans(ctx context.Context, spans []trace.ReadOnlySpan) error {
	if e.SpanExporter == nil {
		return nil
	}

	translated := make([]trace.ReadOnlySpan, 0, len(spans))

	for _, s := range spans {
		ts := &translatedSpan{ReadOnlySpan: s}
		translated = append(translated, ts)

		if isToolSpanForTraceMapping(s) {
			registerTraceMappingFromToolCall(s)
		}
	}

	if len(translated) == 0 {
		return nil
	}

	return e.SpanExporter.ExportSpans(ctx, translated)
}

func registerTraceMappingFromToolCall(span trace.ReadOnlySpan) {
	if !isToolSpanForTraceMapping(span) {
		return
	}

	toolCallID := findToolCallID(span.Attributes())
	if toolCallID == "" {
		return
	}

	adkTraceID := span.SpanContext().TraceID()
	if !adkTraceID.IsValid() {
		return
	}

	registry := GetRegistry()
	if veadkParentSC, ok := registry.GetVeadkParentContextByToolCallID(toolCallID); ok {
		registry.RegisterTraceMapping(adkTraceID, veadkParentSC.TraceID())
		log.Debug("Matched tool via ToolCallID, established TraceID mapping",
			"tool_call_id", toolCallID,
			"adk_trace_id", adkTraceID.String(),
			"veadk_trace_id", veadkParentSC.TraceID().String(),
		)
	}
}

func isToolSpanForTraceMapping(span trace.ReadOnlySpan) bool {
	return classifyTranslatedSpanKind(span.Name()) == translatedSpanTool
}

// translatedSpan wraps a ReadOnlySpan and intercepts calls to Attributes().
type translatedSpan struct {
	trace.ReadOnlySpan
}

type translatedSpanKind int

const (
	translatedSpanUnknown translatedSpanKind = iota
	translatedSpanInvocation
	translatedSpanAgent
	translatedSpanLLM
	translatedSpanTool
)

type toolSpanRawData struct {
	ToolName     string
	ToolDesc     string
	ToolArgs     string
	ToolCallID   string
	ToolResponse string
}

func (p *translatedSpan) Attributes() []attribute.KeyValue {
	attrs := p.ReadOnlySpan.Attributes()
	kind := classifyTranslatedSpanKind(p.ReadOnlySpan.Name())
	existingKeys, raw := scanToolSpanRawData(attrs)

	newAttrs := p.processAttributesByKind(kind, attrs, existingKeys)
	newAttrs = p.appendToolReconstructedAttributes(kind, newAttrs, raw)
	newAttrs = appendToolSpanKindAttribute(newAttrs, raw)

	// If it's an LLM span and has request model but no response model, set response model to request model
	if kind == translatedSpanLLM {
		reqModel := getStringAttrFromList(newAttrs, AttrGenAIRequestModel, "")
		respModel := getStringAttrFromList(newAttrs, AttrGenAIResponseModel, "")
		if reqModel != "" && respModel == "" {
			newAttrs = append(newAttrs, attribute.String(AttrGenAIResponseModel, reqModel))
		}
	}

	return newAttrs
}

func scanToolSpanRawData(attrs []attribute.KeyValue) (map[string]bool, toolSpanRawData) {
	existingKeys := make(map[string]bool)
	raw := toolSpanRawData{}

	for _, kv := range attrs {
		key := string(kv.Key)
		existingKeys[key] = true
		switch key {
		case AttrGenAIToolName:
			raw.ToolName = kv.Value.AsString()
		case AttrGenAIToolDescription:
			raw.ToolDesc = kv.Value.AsString()
		case ADKAttrToolCallArgsName:
			raw.ToolArgs = kv.Value.AsString()
		case AttrGenAIToolCallID:
			raw.ToolCallID = kv.Value.AsString()
		case ADKAttrToolResponseName:
			raw.ToolResponse = kv.Value.AsString()
		}
	}

	return existingKeys, raw
}

func (p *translatedSpan) appendToolReconstructedAttributes(kind translatedSpanKind, attrs []attribute.KeyValue, raw toolSpanRawData) []attribute.KeyValue {
	if kind != translatedSpanTool {
		return attrs
	}

	if raw.ToolArgs != "" && raw.ToolName != "" {
		if inputAttrs := p.reconstructToolInput(raw.ToolName, raw.ToolDesc, raw.ToolArgs); inputAttrs != nil {
			attrs = append(attrs, inputAttrs...)
		}
	}

	if raw.ToolResponse != "" && raw.ToolCallID != "" {
		if outputAttrs := p.reconstructToolOutput(raw.ToolName, raw.ToolCallID, raw.ToolResponse); outputAttrs != nil {
			attrs = append(attrs, outputAttrs...)
		}
	}

	return attrs
}

func appendToolSpanKindAttribute(attrs []attribute.KeyValue, raw toolSpanRawData) []attribute.KeyValue {
	if raw.ToolName != "" || raw.ToolCallID != "" {
		return append(attrs, attribute.String(AttrGenAISpanKind, SpanKindTool))
	}
	return attrs
}

func classifyTranslatedSpanKind(name string) translatedSpanKind {
	switch {
	case name == SpanInvocation:
		return translatedSpanInvocation
	case strings.HasPrefix(name, SpanPrefixInvokeAgent):
		return translatedSpanAgent
	case strings.HasPrefix(name, SpanPrefixGenerateContent) || name == OperationNameGenerateContent || name == SpanCallLLM:
		return translatedSpanLLM
	case strings.HasPrefix(name, SpanPrefixExecuteTool):
		return translatedSpanTool
	default:
		return translatedSpanUnknown
	}
}

func (p *translatedSpan) processAttributesByKind(kind translatedSpanKind, attrs []attribute.KeyValue, existingKeys map[string]bool) []attribute.KeyValue {
	newAttrs := make([]attribute.KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		key := string(kv.Key)

		kv = normalizeOperationNameBySpanKind(kind, key, kv)

		// 1. Map ADK internal attributes if not already present in standard form
		if isADKInternalAttribute(key) {
			targetKey, ok := ADKAttributeKeyMap[key]
			if ok {
				if shouldSkipMappedToolAttribute(targetKey) {
					continue
				}

				if !hasExistingKey(existingKeys, targetKey) {
					newAttrs = append(newAttrs, attribute.KeyValue{Key: attribute.Key(targetKey), Value: kv.Value})
				}
			}
			continue
		}

		kv = patchGenAISystem(kv)

		newAttrs = append(newAttrs, kv)
	}
	return newAttrs
}

func isADKInternalAttribute(key string) bool {
	return strings.HasPrefix(key, ADKAttributePrefix)
}

func shouldSkipMappedToolAttribute(targetKey string) bool {
	return targetKey == AttrGenAIToolInput || targetKey == AttrGenAIToolOutput
}

func hasExistingKey(existingKeys map[string]bool, key string) bool {
	return existingKeys[key]
}

func patchGenAISystem(kv attribute.KeyValue) attribute.KeyValue {
	if string(kv.Key) == AttrGenAISystem && kv.Value.AsString() == ADKModelProvider {
		return attribute.String(AttrGenAISystem, ModelProviderVolcengine)
	}
	return kv
}

func normalizeOperationNameBySpanKind(kind translatedSpanKind, key string, kv attribute.KeyValue) attribute.KeyValue {
	if key != AttrGenAIOperationName {
		return kv
	}

	op := kv.Value.AsString()
	if kind == translatedSpanLLM && op == OperationNameGenerateContent {
		return attribute.String(AttrGenAIOperationName, OperationNameChat)
	}
	if (kind == translatedSpanInvocation || kind == translatedSpanAgent) && op == OperationNameInvokeAgent {
		return attribute.String(AttrGenAIOperationName, OperationNameChain)
	}

	return kv
}

func (p *translatedSpan) Name() string {
	name := p.ReadOnlySpan.Name()
	if classifyTranslatedSpanKind(name) == translatedSpanLLM {
		return SpanCallLLM
	}
	return name
}

func (p *translatedSpan) Events() []trace.Event {
	baseEvents := p.ReadOnlySpan.Events()
	if !p.isLLMSpan() {
		return baseEvents
	}
	return appendLLMEventsFromAttributes(p.ReadOnlySpan.Attributes(), baseEvents, p.ReadOnlySpan.StartTime())
}

func (p *translatedSpan) isLLMSpan() bool {
	return classifyTranslatedSpanKind(p.ReadOnlySpan.Name()) == translatedSpanLLM
}

func appendLLMEventsFromAttributes(attrs []attribute.KeyValue, baseEvents []trace.Event, eventTime time.Time) []trace.Event {
	hasEvent := map[string]bool{}
	for _, ev := range baseEvents {
		hasEvent[ev.Name] = true
	}

	inputVal := ""
	outputVal := ""
	for _, kv := range attrs {
		key := string(kv.Key)
		switch key {
		case ADKAttrLLMRequestName, AttrInputValue:
			if inputVal == "" {
				inputVal = kv.Value.AsString()
			}
		case ADKAttrLLMResponseName, AttrOutputValue:
			if outputVal == "" {
				outputVal = kv.Value.AsString()
			}
		}
	}

	newEvents := make([]trace.Event, 0, 4)
	if inputVal != "" {
		if !hasEvent[EventGenAIUserMessage] {
			newEvents = append(newEvents, trace.Event{
				Name:       EventGenAIUserMessage,
				Attributes: []attribute.KeyValue{attribute.String(AttrGenAIMessages, inputVal)},
				Time:       eventTime,
			})
		}
		if !hasEvent[EventGenAIContentPrompt] {
			newEvents = append(newEvents, trace.Event{
				Name:       EventGenAIContentPrompt,
				Attributes: []attribute.KeyValue{attribute.String(AttrInputValue, inputVal)},
				Time:       eventTime,
			})
		}
	}

	if outputVal != "" {
		if !hasEvent[EventGenAIChoice] {
			newEvents = append(newEvents, trace.Event{
				Name:       EventGenAIChoice,
				Attributes: []attribute.KeyValue{attribute.String(AttrGenAIChoice, outputVal)},
				Time:       eventTime,
			})
		}
		if !hasEvent[EventGenAIContentCompletion] {
			newEvents = append(newEvents, trace.Event{
				Name:       EventGenAIContentCompletion,
				Attributes: []attribute.KeyValue{attribute.String(AttrOutputValue, outputVal)},
				Time:       eventTime,
			})
		}
	}

	if len(newEvents) == 0 {
		return baseEvents
	}

	return append(baseEvents, newEvents...)
}

func (p *translatedSpan) reconstructToolInput(toolName, toolDesc, toolArgs string) []attribute.KeyValue {
	var paramsMap map[string]any
	if err := json.Unmarshal([]byte(toolArgs), &paramsMap); err == nil {
		inputData := map[string]any{
			"name":        toolName,
			"description": toolDesc,
			"parameters":  paramsMap,
		}
		if inputJSON, err := json.Marshal(inputData); err == nil {
			val := string(inputJSON)
			return []attribute.KeyValue{
				attribute.String(AttrGenAIToolInput, val),
				attribute.String(AttrCozeloopInput, val),
				attribute.String(AttrGenAIInput, val),
				attribute.String(AttrInputValue, val),
			}
		}
	}
	return nil
}

func (p *translatedSpan) reconstructToolOutput(toolName, toolCallID, toolResponse string) []attribute.KeyValue {
	var responseMap map[string]any
	// ADK serializes response as map, unmarshal it first
	if err := json.Unmarshal([]byte(toolResponse), &responseMap); err == nil {
		outputData := map[string]any{
			"id":       toolCallID,
			"name":     toolName,
			"response": responseMap,
		}
		if outputJSON, err := json.Marshal(outputData); err == nil {
			val := string(outputJSON)
			return []attribute.KeyValue{
				attribute.String(AttrGenAIToolOutput, val),
				attribute.String(AttrCozeloopOutput, val),
				attribute.String(AttrGenAIOutput, val),
				attribute.String(AttrOutputValue, val),
			}
		}
	}
	return nil
}

func (p *translatedSpan) SpanContext() oteltrace.SpanContext {
	sc := p.ReadOnlySpan.SpanContext()
	registry := GetRegistry()

	toolCallID := findToolCallID(p.ReadOnlySpan.Attributes())

	if remapped, ok := p.tryRemapSpanContextByToolCallID(registry, sc, toolCallID); ok {
		return remapped
	}

	if remapped, ok := p.tryRemapSpanContextByTraceID(registry, sc); ok {
		return remapped
	}

	return sc
}

func findToolCallID(attrs []attribute.KeyValue) string {
	for _, kv := range attrs {
		if string(kv.Key) == AttrGenAIToolCallID {
			return kv.Value.AsString()
		}
	}
	return ""
}

func (p *translatedSpan) tryRemapSpanContextByToolCallID(registry *TraceRegistry, sc oteltrace.SpanContext, toolCallID string) (oteltrace.SpanContext, bool) {
	if veadkParentSC, ok := registry.GetVeadkParentContextByToolCallID(toolCallID); ok {
		return newSpanContextWithTraceID(sc, veadkParentSC.TraceID()), true
	}
	return oteltrace.SpanContext{}, false
}

func (p *translatedSpan) tryRemapSpanContextByTraceID(registry *TraceRegistry, sc oteltrace.SpanContext) (oteltrace.SpanContext, bool) {
	if veadkTraceID, ok := registry.GetVeadkTraceID(sc.TraceID()); ok {
		return newSpanContextWithTraceID(sc, veadkTraceID), true
	}
	return oteltrace.SpanContext{}, false
}

func newSpanContextWithTraceID(sc oteltrace.SpanContext, traceID oteltrace.TraceID) oteltrace.SpanContext {
	return oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     sc.SpanID(),
		TraceFlags: sc.TraceFlags(),
		TraceState: sc.TraceState(),
		Remote:     sc.IsRemote(),
	})
}

func (p *translatedSpan) Parent() oteltrace.SpanContext {
	parent := p.ReadOnlySpan.Parent()
	registry := GetRegistry()

	// 1. Check if this is an invoke_agent span - link to our invocation span if available
	if classifyTranslatedSpanKind(p.ReadOnlySpan.Name()) == translatedSpanAgent {
		adkTraceID := p.ReadOnlySpan.SpanContext().TraceID()
		if invocationSC, ok := registry.GetInvocationSpanContext(adkTraceID); ok {
			return invocationSC
		}
	}

	// 2. Check for tool call ID mapping
	toolCallID := findToolCallID(p.ReadOnlySpan.Attributes())
	if remapped, ok := tryParentByToolCallID(registry, toolCallID); ok {
		return remapped
	}

	// 3. Check for trace ID mapping
	if remapped, ok := tryParentByTraceID(registry, parent); ok {
		return remapped
	}

	return parent
}

func tryParentByToolCallID(registry *TraceRegistry, toolCallID string) (oteltrace.SpanContext, bool) {
	if manualParentSC, ok := registry.GetVeadkParentContextByToolCallID(toolCallID); ok {
		return manualParentSC, true
	}
	return oteltrace.SpanContext{}, false
}

func tryParentByTraceID(registry *TraceRegistry, parent oteltrace.SpanContext) (oteltrace.SpanContext, bool) {
	if !parent.IsValid() {
		return oteltrace.SpanContext{}, false
	}
	if veadkTraceID, ok := registry.GetVeadkTraceID(parent.TraceID()); ok {
		return newSpanContextWithTraceID(parent, veadkTraceID), true
	}
	return oteltrace.SpanContext{}, false
}

func (p *translatedSpan) InstrumentationScope() instrumentation.Scope {
	scope := p.ReadOnlySpan.InstrumentationScope()
	// github.com/volcengine/veadk-go is the InstrumentationName defined in observability/constant.go
	if scope.Name == ADKInstrumentationName || scope.Name == ADKLegacyScopeName || scope.Name == InstrumentationName {
		scope.Name = OpenInferenceScopeName
	}
	scope.Version = Version
	return scope
}

func (p *translatedSpan) InstrumentationLibrary() instrumentation.Scope {
	return p.InstrumentationScope()
}

func getStringAttrFromList(attrs []attribute.KeyValue, key, fallback string) string {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			v := kv.Value.AsString()
			if v != "" {
				return v
			}
		}
	}
	return fallback
}
