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
	"strings"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// SpanEnrichmentProcessor implements sdktrace.SpanProcessor.
// It enriches Google ADK internal spans with standard GenAI semantic conventions and
// platform-specific attributes for CozeLoop, APMPlus, and TLS platforms.
type SpanEnrichmentProcessor struct{}

func (p *SpanEnrichmentProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	name := s.Name()

	// Capture common attributes
	SetCommonAttributes(parent, s)

	// Enrich based on span type
	switch {
	case name == SpanCallLLM:
		SetLLMAttributes(s)
	case strings.HasPrefix(name, SpanExecuteTool):
		toolName := ""
		if parts := strings.SplitN(name, " ", 2); len(parts) == 2 {
			toolName = parts[1]
		}
		SetToolAttributes(s, toolName)
	case name == SpanInvokeAgent || strings.HasPrefix(name, SpanInvokeAgent+" "):
		agentName := FallbackAgentName
		if parts := strings.SplitN(name, " ", 2); len(parts) == 2 {
			agentName = parts[1]
		}
		SetAgentAttributes(s, agentName)
	case name == "Run" || name == SpanInvocation:
		SetWorkflowAttributes(s)
	}
}

func (p *SpanEnrichmentProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	spanName := s.Name()
	elapsed := s.EndTime().Sub(s.StartTime()).Seconds()
	attrs := s.Attributes()

	// Convert trace attributes to metric attributes
	var metricAttrs []attribute.KeyValue
	for _, kv := range attrs {
		// Map specific trace attributes to metric dimensions
		if kv.Key == GenAIRequestModelKey {
			metricAttrs = append(metricAttrs, attribute.String(GenAIRequestModelKey, kv.Value.AsString()))
		}
	}

	if spanName == SpanCallLLM {
		RecordOperationDuration(context.Background(), elapsed, metricAttrs...)

		// Record token usage if available in attributes
		var input, output int64
		for _, kv := range attrs {
			switch kv.Key {
			case GenAIUsageInputTokensKey, GenAIResponsePromptTokenCountKey:
				input = kv.Value.AsInt64()
			case GenAIUsageOutputTokensKey, GenAIResponseCandidatesTokenCountKey:
				output = kv.Value.AsInt64()
			}
		}

		if input > 0 || output > 0 {
			RecordTokenUsage(context.Background(), input, output, metricAttrs...)
		}

	} else if strings.HasPrefix(spanName, SpanExecuteTool) {
		RecordOperationDuration(context.Background(), elapsed, metricAttrs...)
	}
}

func (p *SpanEnrichmentProcessor) Shutdown(ctx context.Context) error {
	return nil
}

func (p *SpanEnrichmentProcessor) ForceFlush(ctx context.Context) error {
	return nil
}
