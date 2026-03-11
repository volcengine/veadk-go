# VeADK Go 可观测性包

本包为 VeADK Go SDK 提供全面的可观测性功能插件，与 [VeADK Python SDK](https://volcengine.github.io/veadk-python/observation/span-attributes/) 和 [OpenTelemetry GenAI 语义约定](https://opentelemetry.io/docs/specs/semconv/gen-ai/) 对齐。

## 功能特性

- **对齐 Python ADK**：实现了与 Python ADK 相同的 Span 属性、事件结构和命名约定
- **多平台支持**：支持同时将 Trace 导出到APMPlus、以及本地文件或标准输出
- **自动属性丰富**：自动从 Context、配置或环境变量中捕获并传播 `SessionID`、`UserID`、`AppName`、`InvocationID`
- **Span 层级支持**：正确跟踪 Invocation → Agent → LLM/Tool 执行的层级关系
- **指标支持**：自动记录 Token 使用量、操作延迟和首 Token 延迟等指标

## Span 属性规范

VeADK Go 实现了以下 Span 属性类别，详见 [Python ADK Span 属性文档](https://volcengine.github.io/veadk-python/observation/span-attributes/)：

### 通用属性 (所有 Span)
- `gen_ai.system` - 模型提供商 (例如 "openai", "ark")
- `gen_ai.system.version` - VeADK 版本
- `gen_ai.agent.name` - Agent 名称
- `gen_ai.app.name` / `app_name` / `app.name` - 应用名称
- `gen_ai.user.id` / `user.id` - 用户 ID
- `gen_ai.session.id` / `session.id` - 会话 ID
- `gen_ai.invocation.id` / `invocation.id` - 调用 ID
- `cozeloop.report.source` - 固定值 "veadk"
- `cozeloop.call_type` - CozeLoop 调用类型
- `openinference.instrumentation.veadk` - 插桩版本

### LLM Span 属性
- `gen_ai.span.kind` - "llm"
- `gen_ai.operation.name` - "chat"
- `gen_ai.request.model` - 模型名称
- `gen_ai.request.type` - 请求类型
- `gen_ai.request.max_tokens` - 最大输出 Token 数
- `gen_ai.request.temperature` - 采样温度
- `gen_ai.request.top_p` - Top-p 参数
- `gen_ai.usage.input_tokens` - 输入 Token 数
- `gen_ai.usage.output_tokens` - 输出 Token 数
- `gen_ai.usage.total_tokens` - 总 Token 数
- `gen_ai.prompt` - 输入消息
- `gen_ai.completion` - 输出消息
- `gen_ai.messages` - 完整消息事件
- `gen_ai.choice` - 模型选择

### Tool Span 属性
- `gen_ai.span.kind` - "tool"
- `gen_ai.operation.name` - "execute_tool"
- `gen_ai.tool.name` - 工具名称
- `gen_ai.tool.input` / `cozeloop.input` / `gen_ai.input` - 工具输入
- `gen_ai.tool.output` / `cozeloop.output` / `gen_ai.output` - 工具输出

### Workflow Span 属性
- `gen_ai.span.kind` - "workflow"
- `gen_ai.operation.name` - "invocation"

## 配置

### YAML 配置

在你的 `config.yaml` 中添加 `observability` 部分：

```yaml
observability:
  opentelemetry:
    apmplus:
      endpoint: "https://apmplus-cn-beijing.volces.com:4318"
      api_key: "YOUR_APMPLUS_API_KEY"
      service_name: "YOUR_SERVICE_NAME"
```

### 环境变量

所有设置均可通过环境变量覆盖：

- `OBSERVABILITY_OPENTELEMETRY_COZELOOP_API_KEY`
- `OBSERVABILITY_OPENTELEMETRY_APMPLUS_API_KEY`
- `VEADK_MODEL_PROVIDER` - 设置模型提供商

只要配置了至少一个 trace exporter，就会自动启用 trace 导出。

## 使用方法


### 可观测性插件

要启用自动 Trace 捕获（包括根 `invocation` Span），请注册可观测性插件：

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
