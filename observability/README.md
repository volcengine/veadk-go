# VeADK Go Observability Package

This package provides comprehensive observability features for the VeADK Go SDK, fully aligned with the [VeADK Python SDK](https://volcengine.github.io/veadk-python/observation/span-attributes/) and [OpenTelemetry GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/).

## Features

- **Python ADK Alignment**: Implements the same span attributes, event structures, and naming conventions as the Python ADK
- **Multi-Platform Support**: Simultaneously export traces to APMPlus, CozeLoop, Volcano TLS, or local files/stdout
- **Automatic Attribute Enrichment**: Automatically captures and propagates `SessionID`, `UserID`, `AppName`, `InvocationID` from context, config, or environment
- **Span Hierarchy Support**: Properly tracks invocation → agent → LLM/tool execution hierarchies
- **Metrics Support**: Automated recording of token usage, operation latencies, and first token latency

## Span Attribute Specification

VeADK Go implements the following span attribute categories as documented in [Python ADK Span Attributes](https://volcengine.github.io/veadk-python/observation/span-attributes/):

### Common Attributes (All Spans)
- `gen_ai.system` - Model provider (e.g., "openai", "ark")
- `gen_ai.system.version` - VeADK version
- `gen_ai.agent.name` - Agent name
- `gen_ai.app.name` / `app_name` / `app.name` - Application name
- `gen_ai.user.id` / `user.id` - User identifier
- `gen_ai.session.id` / `session.id` - Session identifier
- `gen_ai.invocation.id` / `invocation.id` - Invocation identifier
- `cozeloop.report.source` - Fixed value "veadk"
- `cozeloop.call_type` - Call type for CozeLoop
- `openinference.instrumentation.veadk` - Instrumentation version

### LLM Span Attributes
- `gen_ai.span.kind` - "llm"
- `gen_ai.operation.name` - "chat"
- `gen_ai.request.model` - Model name
- `gen_ai.request.type` - Request type
- `gen_ai.request.max_tokens` - Max output tokens
- `gen_ai.request.temperature` - Sampling temperature
- `gen_ai.request.top_p` - Top-p parameter
- `gen_ai.usage.input_tokens` - Input token count
- `gen_ai.usage.output_tokens` - Output token count
- `gen_ai.usage.total_tokens` - Total token count
- `gen_ai.prompt` - Input messages
- `gen_ai.completion` - Output messages
- `gen_ai.messages` - Complete message events
- `gen_ai.choice` - Model choices

### Tool Span Attributes
- `gen_ai.span.kind` - "tool"
- `gen_ai.operation.name` - "execute_tool"
- `gen_ai.tool.name` - Tool name
- `gen_ai.tool.input` / `cozeloop.input` / `gen_ai.input` - Tool input
- `gen_ai.tool.output` / `cozeloop.output` / `gen_ai.output` - Tool output

### Workflow Span Attributes
- `gen_ai.span.kind` - "workflow"
- `gen_ai.operation.name` - "invocation"

## Configuration

### YAML Configuration

Add an `observability` section to your `config.yaml`:

```yaml
observability:
  opentelemetry:
        enable_global_tracer: true
    apmplus:
      endpoint: "https://apmplus-cn-beijing.volces.com:4318"
      api_key: "YOUR_APMPLUS_API_KEY"
      service_name: "YOUR_SERVICE_NAME"
```

### Environment Variables

All settings can be overridden via environment variables:

- `OBSERVABILITY_OPENTELEMETRY_COZELOOP_API_KEY`
- `OBSERVABILITY_OPENTELEMETRY_APMPLUS_API_KEY`
- `OBSERVABILITY_OPENTELEMETRY_ENABLE_GLOBAL_PROVIDER` (default: true)
- `VEADK_MODEL_PROVIDER` - Set model provider


## Usage

### Observability Plugin

To enable automatic trace capture (including the root `invocation` span), register the observability plugin:

```go
import (
    "github.com/volcengine/veadk-go/observability"
    "google.golang.org/adk/cmd/launcher/full"
    "google.golang.org/adk/runner"
    "google.golang.org/adk/plugin"
)

func main() {
    ctx := context.Background()

    config := &launcher.Config{
        AgentLoader:    agent.NewSingleLoader(a),
        PluginConfig: runner.PluginConfig{
            Plugins: []*plugin.Plugin{observability.NewPlugin()},
        },
    }
    
    l := full.NewLauncher()
    if err := l.Execute(ctx, config, os.Args[1:]); err != nil {
        log.Fatal(err)
    }
}
```

## Metrics (Aligned with Python ADK)

This package automatically records standard GenAI metrics when **APMPlus** is configured. The metrics are fully aligned with the Python ADK implementation.

### Standard GenAI Metrics
- `gen_ai.chat.count`: Counter for number of LLM invocations.
- `gen_ai.client.token.usage`: Histogram for input/output token usage.
- `gen_ai.client.operation.duration`: Histogram for LLM operation latency.
- `gen_ai.chat_completions.exceptions`: Counter for exceptions during chat completions.

### Streaming Metrics
- `gen_ai.chat_completions.streaming_time_to_first_token`: Time to first token.
- `gen_ai.chat_completions.streaming_time_to_generate`: Total generation time.
- `gen_ai.chat_completions.streaming_time_per_output_token`: Average time per output token.

### APMPlus Custom Metrics
- `apmplus_span_latency`: Latency for both LLM and Tool spans.
- `apmplus_tool_token_usage`: Estimated token usage for tool inputs (type=input) and outputs (type=output), calculated as `char_len / 4`.

> **Note**: Metrics collection is automatically enabled when APMPlus configuration involves an API Key.
