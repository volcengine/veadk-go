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

## 更新 Session 状态

在下一轮 Agent 执行前，使用 `r.UpdateSession` 更新已有会话：

```go
// r 为已初始化的 VeADK runner。会话只需创建一次，也可使用此前
// Run/RunText 已创建的会话。
_, err := r.SessionService().Create(ctx, &session.CreateRequest{
    AppName: r.AppName(), UserID: "user", SessionID: "session",
    State: map[string]any{"phase": "created", "keep": true},
})
if err != nil {
    return err
}
if err := r.UpdateSession(ctx, "user", "session", map[string]any{
    "phase": "approved", "approval": true,
}); err != nil {
    return err
}
// 从同一服务重新读取已提交的状态及事件历史。
updated, err := r.SessionService().Get(ctx, &session.GetRequest{
    AppName: r.AppName(), UserID: "user", SessionID: "session",
})
if err != nil {
    return err
}
phase, err := updated.Session.State().Get("phase") // "approved"
```

示例使用 `google.golang.org/adk/v2/session`。两个 SDK 入口均返回 error：

```go
func (r *Runner) UpdateSession(ctx context.Context, userID, sessionID string, stateDelta map[string]any) error
func UpdateSession(ctx context.Context, sessionService session.Service, appName, userID, sessionID string, stateDelta map[string]any) error
```

自定义运行时或 app 集成可调用 `runner.UpdateSession(ctx, svc, appName, userID, sessionID, delta)`，传入最终选定的服务。Runner 方法使用归一化后的 appName 和服务：`SessionService` 优先于 `ShortTermMemory`，默认使用内存服务。更新本身不执行 Agent、不写长期记忆；即使开启 `AutoCreateSession`，目标不存在也返回错误。

构建运行时时，通过现有 memory factory 选择一次短期后端：

```go
svc, err := memory.NewShortTermMemoryService(memory.BackendShortTermSQLite,
    &short_term_memory_backends.SqliteBackendConfig{
        CommonDatabaseConfig: &configs.CommonDatabaseConfig{DBUrl: "session.db"},
    })
if err != nil {
    return err
}
r, err := runner.New(runner.Config{AppName: "app", Agent: rootAgent, ShortTermMemory: svc})
```

对应包为 `github.com/volcengine/veadk-go/v2/{memory,configs,runner}` 与 `github.com/volcengine/veadk-go/v2/memory/short_term_memory_backends`。

| 后端 | Factory 类型 | 配置 |
| --- | --- | --- |
| 本地内存 | `memory.BackendShortTermLocal` | `nil`；仅当前服务实例、当前进程有效 |
| SQLite | `memory.BackendShortTermSQLite` | `*short_term_memory_backends.SqliteBackendConfig` |
| MySQL | `memory.BackendShortTermMySQL` | `*short_term_memory_backends.MysqlBackendConfig` |
| PostgreSQL | `memory.BackendShortTermPostgreSQL` | `*short_term_memory_backends.PostgresqlBackendConfig` |

数据库配置嵌入 `*configs.CommonDatabaseConfig`，提供 `DBUrl` 或已有数据库配置字段；凭据不要写入源码或日志。更新沿用服务自身的持久化流程，不要为了更新另建服务。数据库服务重连同一数据库可读取已持久化状态；新建内存服务则从空状态开始。

增量按顶层 key 合并，保留未提及 key；传入对象会整体替换该 key 的旧值，不做嵌套深度合并。`nil` 写入 JSON null，不表示删除。持久值须可 JSON 序列化，数值须有限；无法序列化时在追加前失败。不修改调用方 map，调用期间应保持输入稳定。

普通 key 属于指定 session；ADK `user:` 在同 app/user 的会话间共享，`app:` 在同 app 下共享。`temp:` 在校验前过滤，不进入新接口的持久状态或历史事件。nil、空 map 或仅 temp 的增量仍检查目标存在，成功后不新增事件、不改变更新时间。

每次非空持久增量追加一个标准 ADK 非 Partial 事件，Author 为 `system`，ID、InvocationID、Timestamp 使用标准生成能力，且不带消息内容。Get、序列化、取消及追加错误使用 Go error wrapping 保留底层原因。MySQL 严格模式存在 ADK v2.1.0 共享态初始化限制：PRD 调研已复现混合 `app:`/`user:` 更新因零更新时间失败。本能力保证基础 session key 的兼容路径，共享态失败如实透传；MySQL 独立 `user:` 场景仍需下一节点 e2e 验证。

同会话更新与 Run 应由调用方串行化；Get+AppendEvent 不提供 CAS 或跨请求去重，数据库 stale/锁冲突直接返回，不自动重试。响应丢失时先读回再决定是否重试。调用方负责 app/user 访问授权。

已有底层路径继续有效：Get 最新会话，构造 `session.NewEvent` 并设置 `Actions.StateDelta`，通过同服务 AppendEvent。仅对读取快照调用 `State().Set` 不会完成持久化。伴随 Agent 执行的更新继续使用 `adkrunner.WithStateDelta` 与 Run/RunText；任意完整 ADK Event 仍通过 `SessionService.AppendEvent` 追加。

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
