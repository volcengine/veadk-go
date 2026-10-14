# Runner 示例

这个目录展示如何通过 VeADK runner 在代码中直接调用 Agent。

## Basic

运行基础示例：

```shell
go run ./examples/runner/basic
```

它展示三种常见用法：

- `RunText`：纯文本输入，并直接获取最终文本响应。
- `UserParts` 配合 `CollectFinalText`：处理多 part 或多模态输入。
- `Run`：消费原始 ADK 事件流。

多轮对话时，复用相同的 `userID` 和 `sessionID` 即可延续会话上下文。

## 运行时概念

`runner.Config` 用于配置 VeADK runner 实例。它承载稳定的运行时依赖，例如 root agent、session service、artifact service、memory service、plugin config 和 auto-save 行为。`ShortTermMemory` 和 `LongTermMemory` 是 VeADK 对 ADK Go `SessionService` 和 `MemoryService` 字段的别名。

`apps.RunConfig` 用于配置 VeADK app launcher，例如 simple app、A2A app 和 server app。它和 runner 使用相同的 memory/session 归一化规则，但作用在 app server 生命周期上，而不是单个 runner 实例上。

`agent.RunConfig` 是 ADK Go 的单次运行配置，会传给 `Run` 和 `RunText`。它适合配置某次调用的 ADK 行为，例如 streaming mode 或是否把输入 blob 保存为 artifact。

`context.Context` 是 Go 标准的取消、超时和请求作用域上下文。它不是对话上下文。对话历史来自 `userID`、`sessionID` 和配置的 `SessionService`。

## App 集成

`apps` 包和直接使用 runner 的路径共享同一套 session、memory、plugin 和 auto-save 概念：

- `apps.RunConfig.ShortTermMemory` 是 `SessionService` 的别名。
- `apps.RunConfig.LongTermMemory` 是 `MemoryService` 的别名。
- 启用 `AutoSaveSession` 时，app 会安装和 runner 相同的 VeADK auto-save memory plugin。
- simple app 的 `/invoke` 路由通过 VeADK runner helper 路径返回最终文本响应。
- A2A 和 ADK REST 路由保留原始 ADK 事件流，因为这些协议需要 streaming event、artifact、tool call 和 metadata。

如果是在自己的 Go 流程中嵌入 Agent，可以直接使用 `runner.New`。如果要启动 VeADK 提供的 HTTP app，则使用 `apps.RunConfig`。

## Python Runner 对照

VeADK Python 的 `Runner.run` 接收比较宽泛的 `messages` 参数。VeADK Go 保留 ADK Go 的强类型 content 模型，并通过聚焦的 helper 覆盖常见场景。

| Python 输入 | VeADK Go 对应写法 |
| --- | --- |
| `runner.run(messages="hello")` | `r.RunText(ctx, userID, sessionID, "hello", agent.RunConfig{})` |
| `runner.run(messages=MediaMessage(...))` | 构造 `genai.Part`，调用 `runner.UserParts(...)`，再调用 `r.Run(...)` |
| `runner.run(messages=[...])` | 使用相同 `userID` 和 `sessionID` 多次调用 `RunText` 或 `Run` |
| 返回最终文本 | `RunText(...)` 或 `CollectFinalText(r.Run(...))` |
| 消费事件流 | 遍历 `r.Run(...)` |

这样可以保持 Go 调用点的类型清晰，同时覆盖纯文本、多模态输入、多轮会话、最终文本响应和原始事件流这些常见工作流。

## 输入 Helper

构造用户消息时可以使用这些 helper：

```go
msg := runner.UserText("hello")
msg := runner.UserBytes(imageBytes, "image/png")
msg := runner.UserURI("gs://bucket/image.png", "image/png")
msg := runner.UserParts(
	genai.NewPartFromText("describe this image"),
	genai.NewPartFromBytes(imageBytes, "image/png"),
)
```

如果要构造 model 消息、tool call、tool response 或自定义 role，可以直接使用底层 `google.golang.org/genai` 构造函数。
