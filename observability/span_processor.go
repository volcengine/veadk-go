package observability

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/adk/agent"
)

type veadkSpanProcessor struct{}

func NewVeADKSpanProcessor() sdktrace.SpanProcessor {
	return &veadkSpanProcessor{}
}

func (p *veadkSpanProcessor) OnStart(ctx context.Context, span sdktrace.ReadWriteSpan) {
	p.setCommonAttributes(ctx, span)
	p.setSemanticAttributes(ctx, span)
}

func (p *veadkSpanProcessor) OnEnd(sdktrace.ReadOnlySpan) {}

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

	switch {
	case name == SpanInvocation:
		span.SetAttributes(
			attribute.String(AttrGenAISpanKind, SpanKindWorkflow),
			attribute.String(AttrGenAIOperationName, "chain"),
		)
	case strings.HasPrefix(name, "invoke_agent "):
		agentName := strings.TrimPrefix(name, "invoke_agent ")
		if agentName == "" {
			agentName = FallbackAgentName
		}
		span.SetAttributes(
			attribute.String(AttrGenAISpanKind, SpanKindWorkflow),
			attribute.String(AttrGenAIOperationName, "chain"),
			attribute.String(AttrGenAIAgentName, agentName),
			attribute.String(AttrAgentName, agentName),
			attribute.String(AttrAgentNameDot, agentName),
		)
	case strings.HasPrefix(name, "generate_content ") || name == SpanCallLLM:
		span.SetAttributes(
			attribute.String(AttrGenAISpanKind, SpanKindLLM),
			attribute.String(AttrGenAIOperationName, "chat"),
			attribute.String(AttrGenAIRequestType, "chat"),
		)
	case strings.HasPrefix(name, "execute_tool "):
		toolName := strings.TrimPrefix(name, "execute_tool ")
		if toolName == "" {
			toolName = "<unknown_tool_name>"
		}
		span.SetAttributes(
			attribute.String(AttrGenAISpanKind, SpanKindTool),
			attribute.String(AttrGenAIOperationName, "execute_tool"),
			attribute.String(AttrGenAIToolName, toolName),
		)
	}

	_ = ctx
}
