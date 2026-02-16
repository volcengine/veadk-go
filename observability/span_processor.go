package observability

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/adk/agent"
)

type veadkSpanProcessor struct{}

type semanticSpanKind int

const (
	semanticSpanUnknown semanticSpanKind = iota
	semanticSpanInvocation
	semanticSpanAgent
	semanticSpanLLM
	semanticSpanTool
)

func NewVeADKSpanProcessor() sdktrace.SpanProcessor {
	return &veadkSpanProcessor{}
}

func (p *veadkSpanProcessor) OnStart(ctx context.Context, span sdktrace.ReadWriteSpan) {
	p.setCommonAttributes(ctx, span)
	p.setSemanticAttributes(ctx, span)
}

func (p *veadkSpanProcessor) OnEnd(span sdktrace.ReadOnlySpan) {
	if classifySemanticSpanKind(span.Name()) != semanticSpanTool {
		return
	}

	duration := span.EndTime().Sub(span.StartTime()).Seconds()
	if duration <= 0 {
		return
	}

	toolName := strings.TrimPrefix(span.Name(), SpanPrefixExecuteTool)
	if toolName == "" {
		toolName = "<unknown_tool_name>"
	}

	modelProvider := getStringAttribute(span.Attributes(), AttrGenAISystem, FallbackModelProvider)
	metricAttrs := []attribute.KeyValue{
		attribute.String(MetricAttrGenAIOperationName, toolName),
		attribute.String(MetricAttrGenAIOperationType, OperationTypeTool),
		attribute.String(AttrGenAISystem, modelProvider),
	}

	RecordOperationDuration(context.Background(), duration, metricAttrs...)
	RecordAPMPlusSpanLatency(context.Background(), duration, metricAttrs...)
	p.recordToolTokenUsageFromSpanAttributes(span, metricAttrs)
}

func (p *veadkSpanProcessor) Shutdown(context.Context) error { return nil }

func (p *veadkSpanProcessor) ForceFlush(context.Context) error { return nil }

func (p *veadkSpanProcessor) setCommonAttributes(ctx context.Context, span sdktrace.ReadWriteSpan) {
	sessionID := FallbackSessionID
	userID := FallbackUserID
	appName := FallbackAppName
	invocationID := FallbackInvocationID
	agentName := FallbackAgentName

	if ivc, ok := ctx.(agent.InvocationContext); ok {
		if s := ivc.Session(); s != nil {
			if s.ID() != "" {
				sessionID = s.ID()
			}
			if s.UserID() != "" {
				userID = s.UserID()
			}
			if s.AppName() != "" {
				appName = s.AppName()
			}
		}
		if ivc.InvocationID() != "" {
			invocationID = ivc.InvocationID()
		}
		if ivc.Agent() != nil && ivc.Agent().Name() != "" {
			agentName = ivc.Agent().Name()
		}
	}

	if cctx, ok := ctx.(agent.CallbackContext); ok {
		if cctx.SessionID() != "" {
			sessionID = cctx.SessionID()
		}
		if cctx.UserID() != "" {
			userID = cctx.UserID()
		}
		if cctx.AppName() != "" {
			appName = cctx.AppName()
		}
		if cctx.InvocationID() != "" {
			invocationID = cctx.InvocationID()
		}
		if cctx.AgentName() != "" {
			agentName = cctx.AgentName()
		}
	}

	span.SetAttributes(
		attribute.String(AttrCozeloopReportSource, DefaultCozeLoopReportSource),
		attribute.String(AttrGenAISystem, GetModelProvider(ctx)),
		attribute.String(AttrGenAISystemVersion, Version),
		attribute.String(AttrInstrumentation, Version),
		attribute.String(AttrCozeloopCallType, GetCallType(ctx)),
		attribute.String(AttrGenAISessionID, sessionID),
		attribute.String(AttrSessionID, sessionID),
		attribute.String(AttrGenAIUserID, userID),
		attribute.String(AttrUserID, userID),
		attribute.String(AttrGenAIAppName, appName),
		attribute.String(AttrAppNameUnderline, appName),
		attribute.String(AttrAppNameDot, appName),
		attribute.String(AttrGenAIInvocationID, invocationID),
		attribute.String(AttrInvocationID, invocationID),
		attribute.String(AttrGenAIAgentName, agentName),
		attribute.String(AttrAgentName, agentName),
		attribute.String(AttrAgentNameDot, agentName),
	)
}

func (p *veadkSpanProcessor) setSemanticAttributes(ctx context.Context, span sdktrace.ReadWriteSpan) {
	name := span.Name()
	kind := classifySemanticSpanKind(name)

	switch kind {
	case semanticSpanInvocation:
		p.applyInvocationSemanticAttributes(span)
	case semanticSpanAgent:
		p.applyAgentSemanticAttributes(span, name)
	case semanticSpanLLM:
		p.applyLLMSemanticAttributes(span)
	case semanticSpanTool:
		p.applyToolSemanticAttributes(span, name)
	}

	_ = ctx
}

func classifySemanticSpanKind(name string) semanticSpanKind {
	switch {
	case name == SpanInvocation:
		return semanticSpanInvocation
	case strings.HasPrefix(name, SpanPrefixInvokeAgent):
		return semanticSpanAgent
	case strings.HasPrefix(name, SpanPrefixGenerateContent) || name == SpanCallLLM:
		return semanticSpanLLM
	case strings.HasPrefix(name, SpanPrefixExecuteTool):
		return semanticSpanTool
	default:
		return semanticSpanUnknown
	}
}

func (p *veadkSpanProcessor) applyInvocationSemanticAttributes(span sdktrace.ReadWriteSpan) {
	span.SetAttributes(
		attribute.String(AttrGenAISpanKind, SpanKindWorkflow),
		attribute.String(AttrGenAIOperationName, OperationNameChain),
	)
}

func (p *veadkSpanProcessor) applyAgentSemanticAttributes(span sdktrace.ReadWriteSpan, spanName string) {
	agentName := strings.TrimPrefix(spanName, SpanPrefixInvokeAgent)
	if agentName == "" {
		agentName = FallbackAgentName
	}
	span.SetAttributes(
		attribute.String(AttrGenAISpanKind, SpanKindWorkflow),
		attribute.String(AttrGenAIOperationName, OperationNameChain),
		attribute.String(AttrGenAIAgentName, agentName),
		attribute.String(AttrAgentName, agentName),
		attribute.String(AttrAgentNameDot, agentName),
	)
}

func (p *veadkSpanProcessor) applyLLMSemanticAttributes(span sdktrace.ReadWriteSpan) {
	span.SetAttributes(
		attribute.String(AttrGenAISpanKind, SpanKindLLM),
		attribute.String(AttrGenAIOperationName, OperationNameChat),
		attribute.String(AttrGenAIRequestType, OperationNameChat),
	)
}

func (p *veadkSpanProcessor) applyToolSemanticAttributes(span sdktrace.ReadWriteSpan, spanName string) {
	toolName := strings.TrimPrefix(spanName, SpanPrefixExecuteTool)
	if toolName == "" {
		toolName = "<unknown_tool_name>"
	}
	span.SetAttributes(
		attribute.String(AttrGenAISpanKind, SpanKindTool),
		attribute.String(AttrGenAIOperationName, OperationNameExecuteTool),
		attribute.String(AttrGenAIToolName, toolName),
	)
}

func getStringAttribute(attrs []attribute.KeyValue, key, fallback string) string {
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

func (p *veadkSpanProcessor) recordToolTokenUsageFromSpanAttributes(span sdktrace.ReadOnlySpan, metricAttrs []attribute.KeyValue) {
	inputRaw := getStringAttribute(span.Attributes(), ADKAttrToolCallArgsName, "")
	outputRaw := getStringAttribute(span.Attributes(), ADKAttrToolResponseName, "")

	inputTokens := int64(len(inputRaw)) / 4
	outputTokens := int64(len(outputRaw)) / 4

	if inputTokens > 0 {
		RecordAPMPlusToolTokenUsage(context.Background(), inputTokens, append(metricAttrs, attribute.String(MetricAttrTokenType, TokenTypeInput))...)
	}
	if outputTokens > 0 {
		RecordAPMPlusToolTokenUsage(context.Background(), outputTokens, append(metricAttrs, attribute.String(MetricAttrTokenType, TokenTypeOutput))...)
	}
}
