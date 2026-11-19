# Review Summary

**结论：veadk-go 已有可用的底层 Session 更新路径，缺少独立的 VeADK `UpdateSession` 便捷 API。本需求补齐 SDK 入口，复用运行时的 SessionService 和 ADK StateDelta 事件。**

- 需求类型：**开发者功能特性**；关联 Meego `7397916600`。
- 变更面：Go SDK、Session/Runner、短期记忆持久化契约、兼容性与示例说明。
- 判断依据：用户需求，`runner/runner.go`，`memory/short_term_memory.go`，三个数据库 backend，官方 ADK Go v2.1.0 源码及官方 State 文档。
- 差异判断：四种后端的持久化与并发行为存在差异；本轮限定 V2/main。Python、Java 与 V1 不要求跟进；依据 `CONTRIBUTING.md` 的新特性版本策略。
- 本次变更关键信息：新增不触发 Agent 执行的状态更新入口；复用 SessionService；以事件增量合并状态；保留原有 ADK 调用方式。
- Review 重点：服务实例一致性、app/user/session 定位、事件与状态一致性、错误传播、temp 前缀过滤、四后端验证证据。

## Scope Note

- Implemented sections：以下六节覆盖来源、现状验证、行为/API、验收、SDK/框架调研及交付状态；附录记录临时 e2e 过程与代码。
- Not involved：前端 UX、控制台接口、HTTP/gRPC 新路由、长期记忆写入、埋点/Dashboard、新存储模型、跨语言同步。
- Needs confirmation：开发完成后的新 API e2e 结果；本节点不宣称新接口已实现。
- 原因：用户要求的是运行时 SessionService 上的 SDK 能力，现有 ADK 事件和数据库服务已承担更新及持久化职责。

# PRD：Session Update 能力调研与接口补齐

## 1. 需求来源与目标

| 项目 | 内容 |
| --- | --- |
| 来源 | 开发者功能特性；用户输入与 Meego 最新描述一致 |
| 目标仓库 | `volcengine/veadk-go`，模块 `github.com/volcengine/veadk-go/v2` |
| 交付分支 | `feat/4f204cd7-z82c`，基线 `main` |
| 调研基线 | 2026-11-18 fetch 后 HEAD 与 origin/main 均为 `8034d8e9680e6ce87500bc1370aadf80deb3645f` |
| 共享上下文 | 实时 Context version 4，状态 resolved；仅绑定 veadk-go/main |
| 影响能力 | Session、Runner、Memory；属于 VeADK 封装原生 ADK 核心能力 |

**开发者问题**：业务侧可能在下一次 Agent 执行前写入审批结果、会话阶段、偏好等状态。目前需要自行获取 Session、构造 ADK Event 并追加；若误用读取对象上的 `State().Set`，新读取与后续 Agent 执行不会看到该变更。

**目标**：开发者通过 Runner 的明确公开方法更新已存在会话，并在同一存储中读回，下一轮 Agent 使用更新后的状态。

- 覆盖多个 key 的基础增量更新；变更作为标准 ADK Event 追加。
- 使用已有四种短期记忆后端；不重复建设数据库服务，不降级依赖。
- 不扩展为会话整对象替换、历史事件编辑/删除、JSON Patch、原子计数、CAS、跨请求幂等或 checkpoint 分叉能力。

## 2. 调研现状与 bug 验证

**验证思路**：从独立开发者程序创建真实 Agent 和 VeADK Runner，通过 Runner 实际配置的服务创建/读取会话；比较直接 Set 与追加 StateDelta 的新读取结果；执行 Agent 读取更新后的状态并回写事件；再用独立进程检验数据库持久化。此需求按功能缺口处理，直接 Set 的结果用于解释 ADK 使用契约，不将它误报为 VeADK 数据库故障。

### 2.1 现有调用流程

| 环节 | 已有能力与证据 | 开发者影响 |
| --- | --- | --- |
| 服务选择 | `runner.NormalizeConfig`：SessionService 优先，其次 ShortTermMemory，均未提供时创建 InMemoryService | 必须使用最终选中的同一实例，不能重新按 backend 创建服务 |
| 服务暴露 | `Runner.SessionService()`、`AppName()` 已公开 | 已能由 Runner 定位真实会话存储 |
| 创建/读取 | 官方 `session.Service.Create/Get/List/Delete`；Run 的 AutoCreateSession 也可创建 | 更新 API 的目标应是已有会话，不继承运行时自动创建语义 |
| 事件追加 | `SessionService.AppendEvent(ctx, session, event)` | 已有对外底层更新路径，不是“完全无法更新” |
| 外部状态变更 | Get 最新对象 → Event.Actions.StateDelta → AppendEvent → Get | 符合 ADK 官方状态更新方式 |
| 执行时状态变更 | VeADK Run/RunText 透传 ADK RunOption；`adkrunner.WithStateDelta` 随用户事件追加 | 已支持伴随执行的更新，不能替代不执行 Agent 的新入口 |
| Agent 状态写入 | Runner 将 Agent 事件交给同一 SessionService.AppendEvent | 下一次执行读取该服务中的持久状态 |
| 本地内存 | 原生 InMemoryService；map 增量合并及事件追加 | 进程内服务实例有效；新实例不共享状态，无跨进程持久化 |
| SQLite/MySQL/PostgreSQL | VeADK backend 仅封装 GORM dialector、ADK database.NewSessionService 和 AutoMigrate | 新 API 不应直接写 SQL 或新建 schema |
| 数据库存储 | ADK applyEvent 在事务中写事件、session state/update time、app/user state；检查 stale snapshot | 返回成功需以 AppendEvent 成功为准，不可把本地对象的变化当作落库成功 |
| 长期记忆 | AutoSaveSession 实际配置长期记忆插件，另有 SaveSessionToLongTermMemory | 不是短期 Session 的保存开关；新 API 不触发长期记忆写入 |

已检索 `runner/`、`memory/`、`apps/` 的公开定义：未找到 `UpdateSession`/`UpdateState` 独立入口。ADK Go `session.Service` 也没有 Update 方法。ADK REST Sessions controller 提供 Create/Get/List/Delete；本需求不新增 HTTP 接口。

### 2.2 实际验证结果

| 场景 | local | SQLite 文件 | MySQL 8.0.46（默认严格模式） | PostgreSQL 14.24 |
| --- | --- | --- | --- | --- |
| Create/Get，直接 Set 后新读取 | Set 未写回，通过 | Set 未写回，通过 | Set 未写回，通过 | Set 未写回，通过 |
| 普通 state delta、保留旧 key、null | 通过 | 通过 | 通过（独立基础场景） | 通过 |
| 下一轮 Agent 读取外部更新、回写事件 | 通过，最终 5 个事件 | 通过，最终 5 个事件 | 通过，最终 5 个事件 | 通过，最终 5 个事件 |
| RunText + WithStateDelta | 通过 | 通过 | 通过 | 通过 |
| app:/user: 范围共享 | 通过 | 通过 | 混合前缀事件在 app state 写入阶段失败，见下文；user: 独立场景未验证 | 通过 |
| temp: 新 Get 状态 | 不存在，通过 | 不存在，通过 | 基础场景不存在，通过 | 不存在，通过 |
| temp: 存储历史事件 | **仍保留 temp:scratch** | 已过滤 | 基础场景已过滤 | 已过滤 |
| 数据库 stale snapshot | 不作数据库语义承诺 | 拒绝，存储未改变 | 拒绝，存储未改变 | 拒绝，存储未改变 |
| 新进程重建服务读取 | 不适用，进程内存 | 通过，状态/5 个事件 | 基础场景通过，状态/5 个事件 | 通过，状态/5 个事件 |

**MySQL 事实限制**：带 app:/user: 的混合事件返回 `failed to save app state: Error 1292 (22007): Incorrect datetime value: '0000-00-00' for column 'update_time' at row 1`。新 Get 确认 phase 仍为 created、事件数为 0、app/user key 均未写入，事务回滚成立。未调整 sql_mode 掩盖问题。

源码定位：ADK database.fetchStorageAppState 在不存在记录时构造未设置 UpdateTime 的 storageAppState；applyEvent 随后 Save；字段仅标注 precision，MySQL 严格模式拒绝零日期。user state 的相似源码也没有初始化 UpdateTime，但**本轮未单独复现 user-only，不宣称其结果**。

**本轮兼容性边界**：新便捷 API 保证四后端的普通 session state；app:/user: 复用底层语义与错误行为，不能承诺修复上述已存在的 MySQL 共享态问题。开发文档须明确限制，错误必须透传；修复上游共享态兼容性作为独立后续问题评估，不作为基础 Session Update 的隐性范围扩大。

**内存后端差异**：原生 InMemoryService 的新 Get state 没有 temp key，但存储事件仍带 temp key；SQLite/PostgreSQL/MySQL 基础场景过滤正常。新便捷 API 须在提交前过滤增量副本，避免该原生路径差异影响新增入口。

所有成功场景进程退出 0；MySQL 混合共享态场景 go run 退出 1（程序 panic），结果如实记录。未运行尚不存在的 UpdateSession API。


### 2.3 ADK 语义与边界

- 普通 key 属于当前 session；`user:` 在同 app、同 user 的多会话间共享；`app:` 在同 app 下共享。
- `temp:` 为临时态，不应落库；不能把 app/user 前缀误解释为只影响当前 session。
- StateDelta 是顶层 key 增量合并；同 key 覆盖整个值，不执行嵌套对象深度合并。
- nil 是可序列化的空值，不等价于删除 key；跨数据库读取数值的 Go 具体类型可能变化，验收使用值语义。
- Partial 事件被原生服务忽略；便捷更新 API 必须构造非 Partial 事件。
- ADK 数据库会拒绝过期 Session 对象；其本地对象可能在持久化失败前已变更。失败后必须以新 Get 校验真实结果。

## 3. 需求范围与行为

### 3.1 Functional Requirements（均为本需求必需）

| REQ | EARS 行为与契约 |
| --- | --- |
| REQ-001 公开入口 | When 开发者对 Runner 调用 UpdateSession，VeADK shall 使用 Runner 已归一化的 appName 和 SessionService，以 userID/sessionID 定位目标；不创建第二份存储服务，不触发 Agent/Model/Tool。 |
| REQ-002 已有会话定位 | When 标识合法，VeADK shall 先 Get 最新会话；If userID/sessionID 或显式 appName 为空、service 为 nil，shall 返回可识别错误；If 目标不存在，shall 返回错误，不创建 session，即使 AutoCreateSession=true。 |
| REQ-003 基础状态更新 | When 提供 StateDelta，VeADK shall 按顶层 key 新增/覆盖并保留未提及 key；支持字符串、布尔、有限数值、null、JSON 可序列化列表/对象；If 值无法 JSON 序列化，shall 在追加事件前失败，避免内存成功而数据库失败。 |
| REQ-004 标准事件追加 | When 有可持久化变更，VeADK shall 生成一个标准 ADK 非 Partial Event，具有有效 ID、InvocationID、Timestamp、明确的外部更新 Author 和 Actions.StateDelta，无伪造用户/模型消息；通过同一服务 AppendEvent 更新状态和追加变更记录。 |
| REQ-005 前缀与输入保护 | When 使用 user:/app:，VeADK shall 复用 ADK 共享范围和底层错误语义（MySQL 已知限制见 §2.2）；When 使用 temp:，shall 从待持久化增量副本中移除，且不修改调用方 map；持久化事件和新 Get 均不含 temp:。 |
| REQ-006 空增量 | When nil/空 map 或过滤后无持久 key，VeADK shall 在验证目标存在后返回成功，不追加空事件、不改变更新时间；空增量不得隐式创建会话。 |
| REQ-007 成败传播 | When Get/AppendEvent 失败或 context 被取消，shall 返回带操作上下文并保留底层 error 链的错误；不得吞掉 stale/存储失败，不自动重试非幂等追加；失败不得声称更新已完成。 |
| REQ-008 后端兼容 | When 使用 local/SQLite/MySQL/PostgreSQL，shall 通过 session.Service 公共契约工作；成功后新 Get 可见，下一次 Runner.Run 可见；数据库重建服务/重启进程仍可读取。 |
| REQ-009 文档与兼容 | VeADK shall 记录新 SDK 签名、现有 Get+AppendEvent 路径、前缀/空值/错误/并发边界及四后端使用方式；保持现有 Run/Config/SessionService API 和数据库格式兼容。 |

### 3.2 API Design：建议公开 Go 契约

**新增 SDK API 的建议签名（当前 main 尚不存在，开发阶段须落实导出名称与 GoDoc）**：

| 入口 | 建议签名 | 定位与用途 |
| --- | --- | --- |
| Runner 方法 | `func (r *Runner) UpdateSession(ctx context.Context, userID, sessionID string, stateDelta map[string]any) error` | app 来自 `r.AppName()`；使用 `r.SessionService()`，覆盖主要开发者场景 |
| 显式服务 helper | `func UpdateSession(ctx context.Context, sessionService session.Service, appName, userID, sessionID string, stateDelta map[string]any) error` | 遵循现有 SaveSessionToLongTermMemory helper 形态；允许 apps/自定义运行时传入其最终服务 |

- 不接收 backend/config/DSN；不能在更新时重新选择存储。
- 返回 error；开发者需要状态/历史时继续通过同一服务 Get，无需返回持久化前的快照。
- 错误类别：参数错误、会话不存在、不可序列化、底层 Get/AppendEvent 失败、context 取消/超时。保留底层错误链；不新增 HTTP 错误码体系。
- Author 使用明确外部更新来源（例如 `system`），不得使用真实 agent 名称冒充执行结果；ID/InvocationID/Timestamp 采用 ADK 标准生成能力。实际名称由 GoDoc 固定。
- 追加任意完整 ADK Event 仍使用已公开的 SessionService.AppendEvent；本轮便捷 API 聚焦 state delta，不复制 ADK Event 全部字段到新 DTO。

**开发者使用方式**：创建 Runner 并配置短期记忆 → 创建目标会话或先运行一次 → 调用 Runner.UpdateSession 写入 `phase=approved` 等增量 → 通过 Runner.SessionService().Get 读回 → 下一轮 Run 使用该状态。显式 helper 必须接收运行时归一化后的服务，不能再次创建内存实例。

### 3.3 Backend/Data 与 NFR/DFX

- 不修改 SessionService interface、不对现有实现增加强制方法；不修改数据库表或迁移已有数据。
- 新增能力仅限 V2；原生 Get/AppendEvent 与 WithStateDelta 持续可用。
- 只保证已完成的顺序更新在后续读取可见；不承诺与同会话正在执行的 Run 强一致，也不承诺跨实例线性一致/CAS。业务需串行化同会话变更。
- 数据库 stale 冲突/锁冲突按底层错误返回；已提交但响应丢失时不得自动重发。调用方必须读回判断，历史事件重试去重不在本轮范围。
- SDK 不提供额外身份认证；调用方负责保证 app/user 的访问权限。隔离验证需同时覆盖普通 key 与 app/user 共享 key。
- temp 过滤与序列化检查在输入副本上进行；不改调用方 map。数据库时间戳按 ADK 微秒精度，不能假定纳秒完全相等。
- 发布为向后兼容的新 SDK 能力；回滚采用正常回退新增入口，使用它的客户端先移回原生调用路径；无数据清理或 schema 回滚。

## 4. 验收标准与验证要求

**开发交付门禁：必须在新 API 实现后执行独立 e2e，而不是仅凭本节点的现有 API 验证判定通过。**

| 验收 | 对应 REQ | 必须观测的结果 |
| --- | --- | --- |
| 四后端基础路径 | 001/003/004/008 | 每种后端由开发者程序创建 Agent/Runner/session → 新 API 更新 → 新 Get 校验 key、保留值、恰好一个更新事件 → 下一轮 Agent 输出/状态证明读到新值 |
| 服务选择 | 001/008 | 默认内存、仅 ShortTermMemory、自定义 SessionService、二者同时提供；更新与 Run 走同一实例，SessionService 优先；helper 使用同实例 |
| 目标维度 | 002/005 | 普通 key 在四后端按 app/user/session 隔离；local/SQLite/PostgreSQL 校验 user:/app: 共享；MySQL 严格模式的已知混合前缀失败必须返回错误且事务回滚，user-only 需开发节点补测并记录支持边界 |
| 输入语义 | 003/005/006 | 多 key、新 key、同 key 覆盖、嵌套对象整体替换、null、空 map、仅 temp、混合 temp；调用方 map 保持原样，temp 不进入历史/持久状态 |
| 参数与不存在 | 002/007 | 空标识、nil service、missing session；AutoCreateSession=true 仍不创建；未产生状态更新事件 |
| 事件与失败 | 004/007 | 更新事件元数据有效、无消息内容、非 Partial；不可序列化、取消、底层失败有错误；用新 Get 校验真实持久状态，不检查失败对象的本地 map 来判断成功 |
| 数据库持久化 | 008 | SQLite 使用文件 DB；MySQL/PostgreSQL 使用真实服务；新进程/新服务可读回状态、事件和更新时间 |
| 并发边界 | 007/NFR | 顺序多次更新保留未触及 key；数据库 stale 会话失败后存储无该次事件/状态；无自动追加重试，不把 Get+AppendEvent 视为 CAS |
| 既有兼容 | 009 | 原生 AppendEvent、RunText/WithStateDelta、现有短期后端保持有效；无新 schema 或依赖降级 |

```gherkin
Scenario: 外部状态更新后 Agent 读取同一存储
  Given 一个使用 SQLite 文件后端的 VeADK Runner 和已有 app/user/session
  When 开发者调用该 Runner 的 UpdateSession 写入 phase=approved
  Then 新 Get 的 phase 为 approved 且新增一个标准 StateDelta 事件
  When 开发者在同一会话执行下一轮 Agent
  Then Agent 读取到 approved
  And 新进程仍可读取状态和追加的事件
```

标准检查按 `CONTRIBUTING.md` 和 `.github/workflows/pre_check.yml` 执行：changed Go 文件 gofmt；`go mod tidy -diff`；`go build -mod=readonly -v ./...`；`go test -mod=readonly -v -race -count=1 -shuffle=on ./...`，覆盖率输出放仓库外。PRD 节点只有 Markdown 变更，采用文档检查与基线 e2e；上述实现门禁由开发节点执行。

**本节点未调用真实模型、火山云记忆或控制台**：需求路径只依赖 SessionService；使用真实 ADK custom Agent 和真实 Runner 读取/写回状态，避免引入模型随机输出与无关费用。四种短期后端按实际运行记录验收，无控制台 agentID；测试 Agent Name 为 `state_reader`，AppName 为 `session_update_survey`，主要 UserID/SessionID 为 `u1/s1`。

## 5. SDK 与同类框架调研要求

| 来源 | 已调研能力与结论 | 本需求要求 |
| --- | --- | --- |
| 官方 ADK Go v2.1.0 session.Service | Create/Get/List/Delete/AppendEvent，无 Update；State.Set 仅修改读出对象；EventActions.StateDelta 驱动存储 | 封装公共契约，不 fork ADK，不重复实现 session 数据库 |
| 官方 ADK Runner | WithStateDelta 已支持运行时增量；VeADK 已透传 opts | 文档区分随 Run 更新与独立 UpdateSession；不使用空消息执行来替代更新 |
| 官方 ADK database / GORM | 事务存储事件/状态，stale 检查，JSON 序列化；VeADK 已接入三种 dialector | 复用现有 SQL 后端和当前依赖，不新增数据库 SDK、不直接改表 |
| 官方 State 文档 | 推荐手工 StateDelta Event 或受管理的 Callback/Tool Context；强调前缀、序列化与直接 Set 的限制 | 以官方事件行为为基础，校验当前依赖版本的实际实现差异 |
| LangGraph 官方 checkpointers 文档 | update_state 创建新 checkpoint，经 reducer 更新；以 thread_id 定位 | 参考“明确外部更新入口和追踪变更”的产品体验；不移植 checkpoint、reducer、fork 语义或引入非 Go 依赖 |
| 火山 SDK | 本功能无火山 OpenAPI 调用；现有 volcengine SDK 与 TOS SDK 不承担这四种 Session 后端更新 | 无新增火山 SDK 适配；保持当前依赖，不为状态更新新增外部服务 |

官方来源（访问日期 2026-11-18）：

- [ADK Go session.Service v2.1.0](https://github.com/google/adk-go/blob/v2.1.0/session/service.go)
- [ADK Go InMemoryService](https://github.com/google/adk-go/blob/v2.1.0/session/inmemory.go)
- [ADK Go database Service](https://github.com/google/adk-go/blob/v2.1.0/session/database/service.go)
- [ADK Go Runner / WithStateDelta](https://github.com/google/adk-go/blob/v2.1.0/runner/runner.go)
- [ADK 官方 State 文档](https://adk.dev/sessions/state/)
- [LangGraph 官方 checkpointers / update_state](https://docs.langchain.com/oss/python/langgraph/checkpointers)

本轮直接读取 Go module cache 中锁定版本的官方源码；跨语言在线文档示例仅辅助说明，实际行为以 v2.1.0 源码及本节点运行结果为准。

## 6. 待确认问题与交付状态

| 项目 | 状态与后续责任 |
| --- | --- |
| 产品范围/目标语言 | 已明确：Go V2 SDK 基础状态增量更新；无阻塞产品问题 |
| 公开名称与 GoDoc | 建议签名见 §3.2；开发节点落实编译、示例与导出说明，不扩展事件 DTO |
| MySQL 共享态限制 | 已通过 main 真实服务复现；本轮保证基础 session state 与错误透传。RD 后续评估官方 ADK 修复或不降级的兼容方案；不得放宽严格模式来宣称兼容。user-only 的实测结果待开发节点补充 |
| 原生内存 temp 历史行为 | 见实测结果；便捷 API 在持久化前过滤副本以满足 ADK 临时态契约；直接原生路径的上游修复不作为本轮交付内容 |
| 新 API 验证 | 尚未实现，尚未运行新 API e2e；由开发、自测及 e2e 节点完成 §4 |
| 当前交付 | 本节点交付 PRD、主分支能力调研与现有调用路径验证证据；运行时代码无变更 |

**完成定义**：本节点完成需求规格交付，不代表 Session Update 功能上线。开发节点完成新增入口后，必须通过四后端新接口 e2e、兼容检查与标准 CI 才能进入代码合并。

## 附录：现有能力 e2e 验证过程

### A. 项目结构与隔离

```text
repos/volcengine_veadk-go/       # 最新 main 同 commit 的工作分支；只新增本 PRD
  runner/runner.go              # 真正被验证的运行时代码
  memory/                      # 真正被验证的后端 factory
  docs/prd/session-update.md    # 唯一 Git 变更
/tmp/veadk-session-survey/      # 仓库外临时验证目录，不提交、不上传数据库/日志缓存
  main.go                      # 独立开发者调用程序
  session.db                   # SQLite 文件数据库
  mysql/                       # 独立 MySQL datadir
  pg/                          # 独立 PostgreSQL datadir
  pgsocket/                    # PostgreSQL Unix socket
  *.log                        # 原始运行日志
```

环境：Linux amd64，Go 1.26.5，MySQL 8.0.46，PostgreSQL 14.24，ADK v2.1.0，GORM v1.31.0，SQLite/MySQL/PostgreSQL driver v1.6.0。主分支通过 `git fetch origin main` 确认；未降低依赖版本。未发现 checkout 下嵌套 AGENTS.md；初始工作区干净。

### B. 服务准备与运行

- PostgreSQL 使用安装好的 14 版本二进制；独立 initdb 目录、postgres 操作系统用户、Unix socket，关闭 TCP 监听。
- MySQL 使用安装好的 mysqld；独立 initialize-insecure 数据目录、Unix socket、skip-networking；仅在该隔离实例创建 `session_survey` 数据库。
- 初始化时 PostgreSQL 日志路径不具备 postgres 写权限，第一次启动失败；改为已归属 postgres 的 datadir 内日志路径后启动成功。这是临时环境权限问题，无产品代码修改。
- MySQL 基础场景第一次复用验证元组导致 other_app 的重复创建错误；改用独立 session_basic 数据库后完成基础验证和新进程读回。失败只涉及临时程序与测试库准备；未删除数据或改产品代码。
- 不复用既有数据库，不输出凭据或完整连接串；MySQL/PostgreSQL DSN 通过仅供该临时程序读取的环境变量传入。

```bash
# 在工作 checkout 内运行；临时代码位于仓库外，使用 checkout 的真实依赖
# 每轮写入使用全新隔离数据库，避免重复 Create。
go run -mod=readonly /tmp/veadk-session-survey/main.go local
go run -mod=readonly /tmp/veadk-session-survey/main.go sqlite
go run -mod=readonly /tmp/veadk-session-survey/main.go mysql  # 混合共享态，预期复现零日期失败
# 切换 SURVEY_MYSQL_DSN 到单独的 session_basic 测试库后：
SURVEY_BASIC_ONLY=1 go run -mod=readonly /tmp/veadk-session-survey/main.go mysql
go run -mod=readonly /tmp/veadk-session-survey/main.go postgresql

# 环境变量 SURVEY_MYSQL_DSN / SURVEY_PG_DSN 在本地准备，不打印内容。
# 独立进程只读验证（不再次创建同 ID）
go run -mod=readonly /tmp/veadk-session-survey/main.go sqlite read
SURVEY_BASIC_ONLY=1 go run -mod=readonly /tmp/veadk-session-survey/main.go mysql read
go run -mod=readonly /tmp/veadk-session-survey/main.go postgresql read
# 切回 session_survey 测试库，读回失败事务：
go run -mod=readonly /tmp/veadk-session-survey/main.go mysql failed
```

产物是进程 stdout PASS 记录及三种数据库内容。校验以程序断言为准：失败 panic 且退出非零；成功需检查退出码及完整关键输出。MySQL/PostgreSQL 使用真实本地服务，SQLite 使用真实文件库；未用 mock SessionService。

### C. 关键验证代码（已实际运行的完整临时程序）

该程序验证**现有** Get/AppendEvent/Runner 行为，不是拟新增 UpdateSession 的实现，不应保存为代码分支测试文件。

```go
package main

import (
	"context"
	"fmt"
	"iter"
	"os"
	"time"

	"github.com/volcengine/veadk-go/v2/configs"
	vememory "github.com/volcengine/veadk-go/v2/memory"
	backends "github.com/volcengine/veadk-go/v2/memory/short_term_memory_backends"
	verunner "github.com/volcengine/veadk-go/v2/runner"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	adkrunner "google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

var app = "session_update_survey"

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func check(ok bool, label string) {
	if !ok {
		panic(label)
	}
}
func get(ctx context.Context, svc session.Service, a, u, s string) session.Session {
	res, err := svc.Get(ctx, &session.GetRequest{AppName: a, UserID: u, SessionID: s})
	must(err)
	return res.Session
}
func val(s session.Session, key string) any { v, e := s.State().Get(key); must(e); return v }
func create(ctx context.Context, svc session.Service, a, u, s string) {
	_, err := svc.Create(ctx, &session.CreateRequest{AppName: a, UserID: u, SessionID: s, State: map[string]any{"phase": "created", "keep": "yes"}})
	must(err)
}
func main() {
	ctx := context.Background()
	basic := os.Getenv("SURVEY_BASIC_ONLY") == "1"
	if basic {
		app = "session_update_basic"
	}
	backend := os.Args[1]
	var cfg any
	switch backend {
	case "sqlite":
		cfg = &backends.SqliteBackendConfig{CommonDatabaseConfig: &configs.CommonDatabaseConfig{DBUrl: "/tmp/veadk-session-survey/session.db"}}
	case "mysql":
		cfg = &backends.MysqlBackendConfig{CommonDatabaseConfig: &configs.CommonDatabaseConfig{DBUrl: os.Getenv("SURVEY_MYSQL_DSN")}}
	case "postgresql":
		cfg = &backends.PostgresqlBackendConfig{CommonDatabaseConfig: &configs.CommonDatabaseConfig{DBUrl: os.Getenv("SURVEY_PG_DSN")}}
	}
	svc, err := vememory.NewShortTermMemoryService(vememory.ShortTermBackendType(backend), cfg)
	must(err)
	a, err := agent.New(agent.Config{Name: "state_reader", Run: func(ic agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			phase, e := ic.Session().State().Get("phase")
			if e != nil {
				yield(nil, e)
				return
			}
			event := session.NewEvent(ic, ic.InvocationID())
			event.Author = "state_reader"
			event.LLMResponse = model.LLMResponse{Content: genai.NewContentFromText(fmt.Sprint(phase), genai.RoleModel)}
			event.Actions.StateDelta["agent_seen"] = phase
			yield(event, nil)
		}
	}})
	must(err)
	r, err := verunner.New(verunner.Config{AppName: app, Agent: a, ShortTermMemory: svc})
	must(err)
	check(r.SessionService() == svc, "service identity")
	if len(os.Args) > 2 && os.Args[2] == "failed" {
		s := get(ctx, svc, app, "u1", "s1")
		check(val(s, "phase") == "created" && s.Events().Len() == 0, "failed transaction mutated storage")
		_, e := s.State().Get("app:brand")
		check(e != nil, "failed app state persisted")
		_, e = s.State().Get("user:locale")
		check(e != nil, "failed user state persisted")
		fmt.Printf("%s failed_append rollback PASS events=%d\n", backend, s.Events().Len())
		return
	}
	if len(os.Args) > 2 && os.Args[2] == "read" {
		s := get(ctx, r.SessionService(), app, "u1", "s1")
		check(val(s, "phase") == "run_option" && val(s, "agent_seen") == "run_option", "process persistence")
		check(s.Events().Len() == 5, "persisted event count")
		fmt.Printf("%s restart_read PASS events=%d\n", backend, s.Events().Len())
		return
	}
	create(ctx, svc, app, "u1", "s1")
	create(ctx, svc, app, "u1", "s2")
	create(ctx, svc, app, "u2", "s1")
	create(ctx, svc, "other_app", "u1", "s1")
	snap := get(ctx, svc, app, "u1", "s1")
	must(snap.State().Set("phase", "direct_set"))
	check(val(get(ctx, svc, app, "u1", "s1"), "phase") == "created", "direct Set persisted unexpectedly")
	fmt.Printf("%s direct_set fresh_read=created PASS\n", backend)
	fresh := get(ctx, svc, app, "u1", "s1")
	event := session.NewEvent(ctx, "external-update")
	event.Author = "system"
	event.Actions.StateDelta = map[string]any{"phase": "updated", "user:locale": "zh", "app:brand": "veadk", "temp:scratch": "transient", "nullable": nil, "nested": map[string]any{"new": true}}
	if basic {
		delete(event.Actions.StateDelta, "app:brand")
		delete(event.Actions.StateDelta, "user:locale")
	}
	must(r.SessionService().AppendEvent(ctx, fresh, event))
	updated := get(ctx, svc, app, "u1", "s1")
	check(val(updated, "phase") == "updated" && val(updated, "keep") == "yes", "delta merge")
	check(val(updated, "nullable") == nil, "nil value")
	_, e := updated.State().Get("temp:scratch")
	check(e != nil, "temp persisted in state")
	_, tempInEvent := updated.Events().At(0).Actions.StateDelta["temp:scratch"]
	fmt.Printf("%s append PASS events=%d temp_in_stored_event=%v\n", backend, updated.Events().Len(), tempInEvent)
	if !basic {
		sameUser := get(ctx, svc, app, "u1", "s2")
		otherUser := get(ctx, svc, app, "u2", "s1")
		otherApp := get(ctx, svc, "other_app", "u1", "s1")
		check(val(sameUser, "phase") == "created" && val(sameUser, "user:locale") == "zh", "same user scope")
		check(val(otherUser, "phase") == "created" && val(otherUser, "app:brand") == "veadk", "app scope")
		_, e = otherUser.State().Get("user:locale")
		check(e != nil, "cross user leak")
		_, e = otherApp.State().Get("app:brand")
		check(e != nil, "cross app leak")
		fmt.Printf("%s isolation PASS\n", backend)
	}
	partial := session.NewEvent(ctx, "partial")
	partial.Author = "system"
	partial.Partial = true
	partial.Actions.StateDelta["phase"] = "partial"
	must(svc.AppendEvent(ctx, updated, partial))
	check(val(get(ctx, svc, app, "u1", "s1"), "phase") == "updated", "partial persisted")
	text, e := r.RunText(ctx, "u1", "s1", "read state", agent.RunConfig{})
	must(e)
	check(text == "updated", "agent did not see external update")
	text, e = r.RunText(ctx, "u1", "s1", "read state with option", agent.RunConfig{}, adkrunner.WithStateDelta(map[string]any{"phase": "run_option"}))
	must(e)
	check(text == "run_option", "run option not applied")
	end := get(ctx, svc, app, "u1", "s1")
	check(val(end, "agent_seen") == "run_option" && end.Events().Len() == 5, "runner writes")
	_, e = svc.Get(ctx, &session.GetRequest{AppName: app, UserID: "u1", SessionID: "missing"})
	check(e != nil, "missing Get")
	if backend != "local" {
		staleEvent := session.NewEvent(ctx, "stale")
		staleEvent.Author = "system"
		staleEvent.Actions.StateDelta["phase"] = "stale"
		e = svc.AppendEvent(ctx, snap, staleEvent)
		check(e != nil, "stale snapshot accepted")
		check(val(get(ctx, svc, app, "u1", "s1"), "phase") == "run_option", "stale storage changed")
		fmt.Printf("%s stale_rejected PASS\n", backend)
	}
	check(!end.LastUpdateTime().Before(event.Timestamp.Truncate(time.Microsecond)), "timestamp")
	fmt.Printf("%s runner PASS text=%s events=%d\n", backend, text, end.Events().Len())
}
```

### D. 实际运行关键输出

```text
local direct_set fresh_read=created PASS
local append PASS events=1 temp_in_stored_event=true
local isolation PASS
local runner PASS text=run_option events=5
sqlite direct_set fresh_read=created PASS
sqlite append PASS events=1 temp_in_stored_event=false
sqlite isolation PASS
sqlite stale_rejected PASS
sqlite runner PASS text=run_option events=5
mysql direct_set fresh_read=created PASS
mysql append PASS events=1 temp_in_stored_event=false
mysql stale_rejected PASS
mysql runner PASS text=run_option events=5
postgresql direct_set fresh_read=created PASS
postgresql append PASS events=1 temp_in_stored_event=false
postgresql isolation PASS
postgresql stale_rejected PASS
postgresql runner PASS text=run_option events=5
sqlite restart_read PASS events=5
mysql restart_read PASS events=5
postgresql restart_read PASS events=5
mysql failed_append rollback PASS events=0
```

MySQL 混合共享态的错误全文见 §2.2；成功输出中的 temp_in_stored_event=true 代表已复现的原生内存历史事件差异，不代表新增 API 可以接受该行为。

验证结束后关闭本次独立 MySQL/PostgreSQL 实例，临时数据库、日志及验证程序不提交、不登记为跨节点产物；代码与关键事实已完整保存在本附录。

## 当前交付基线对齐

负责人已选择以当前 `feat/4f204cd7-z82c` / `c87e0f6ae782d57bee126429bf333ccb7e521a93` 整理交付。本文保留已确认的需求契约与调研历史；新增 API 的当前实现、独立四后端 E2E 结论及合并门禁见 [交付记录](../session-update/task.md)。完整 E2E 程序与执行证据登记为“Session Update E2E 验证报告”，不新增产品分支测试程序。
