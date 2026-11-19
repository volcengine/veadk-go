---
title: Session Update SDK 技术设计
status: approved
schema_version: 1
linked_spec: ../../docs/prd/session-update.md
created_at: 2026-11-19
updated_at: 2026-11-19
---

# Design: Session Update SDK

# 1. 总体方案

在 V2 `runner` 包增加 Runner 方法与显式服务 helper，封装官方 ADK 的 Get → AppendEvent(StateDelta)；更新使用运行时最终选中的同一 SessionService。目标是已存在的会话，更新本身不执行 Agent，不写长期记忆。

输入来源：工作区 `artifacts/需求澄清 & Spec设计/Session Update PRD/session-update.md`（manifest version 3，产物 version 1），仓库 `docs/prd/session-update.md`，用户需求与当前节点约束。关联 Meego story 7397916600。Current Repo 为 `volcengine/veadk-go`，工作分支 `feat/4f204cd7-z82c`，当前 HEAD `1eae74479b86bcfa449ad3ce55633e9a5f013bfd`。采用 backend 域的 Go SDK 设计；当前节点开发与单元测试，独立 e2e 由下一节点执行，最终合并门禁仍需满足 PRD §4。

已阅读 `CONTRIBUTING.md`、`.github/workflows/pre_check.yml`、`runner/runner.go`、相关 runner 单测、`memory/short_term_memory.go` 和三种数据库 backend。仓库未发现嵌套 AGENTS.md。`.agentnative/templates/default/` 未物化，采用同步的 `knowledge/template/技术设计模板.md` 与 `任务拆分规范.md`，保留设计模板的十二节；服务端模板的 Proto/TOP 要求不适用于本 SDK。

现状：NormalizeConfig 的优先级为 SessionService → ShortTermMemory → 新建 InMemoryService；New 将归一化结果同时传给 ADK Runner 并保存在 VeADK Runner 字段。公开 SessionService/AppName 可定位最终存储；SaveSessionToLongTermMemory 已有方法+helper 模式。创建/读取与事件追加由官方 session.Service 提供；Run 透传 WithStateDelta，Agent 输出经该服务追加。直接对 Get 对象 State.Set 不承担持久化。数据库 backend 仅提供 GORM dialector 和 ADK database 服务/迁移封装。

官方 ADK v2.1.0 `session/service.go`、`session/session.go`、`session/inmemory.go`、`session/database/service.go` 已从官方 tag 只读核对：Service 无 Update；NewEvent 使用 platform UUID/time；数据库 AppendEvent 事务落库、检查 stale、过滤 temp。官方内存服务存储历史复制原始增量，所以便捷入口须主动过滤 temp。当前工作区 module cache 尚无该版本，实现时按 go.mod 锁定版本获取，不降级。

| 层 | 目录 | 改动范围 | 关联 REQ |
|---|---|---|---|
| Service / Validator | runner/ | 新增 SDK 方法、helper、参数与增量校验 | 001–007 |
| Model | 官方 ADK session.Event | 复用，无模型定义变更 | 004/008 |
| Proto / Handler / Client | 无 | 无 RPC、HTTP 或 TOP 注册 | 009 |
| Workflow / DB Schema / 配置 | 无 | 复用运行时与现有存储 | 001/008/009 |

# 2. 文件清单

| 文件路径 | 新增/修改 | 职责 | 关联 REQ |
|---|---|---|---|
| runner/session_update.go | 新增 | Runner.UpdateSession、runner.UpdateSession 与 GoDoc，保持 runner.go 聚焦现有运行逻辑 | 001–007 |
| runner/session_update_test.go | 新增 | 内存/SQLite 行为测试，服务选择与错误注入单测 | 001–008 |
| examples/runner/README.md | 修改 | 英文签名、示例、持久化与错误边界说明 | 009 |
| examples/runner/README_zh.md | 修改 | 对应中文说明与四后端配置方式 | 009 |
| docs/session-update/design.md | 新增 | 本技术设计及实施边界 | 001–009 |
| docs/session-update/task.md | 新增 | 任务追溯及下游交接 | 001–009 |
| nfr/compat-impact.yaml | 新增 | NFR integration-test skill 要求的兼容性影响声明；仅新增 SDK 签名，无破坏性变更 | 009 |

# 3. 数据模型

无 DB 变更。使用现有 Event.Actions.StateDelta 和 ADK 存储表，无迁移、字段映射或 SQL。顶层增量覆盖同 key 整个值，保留未提及 key；嵌套对象不深度合并，nil 表示 null 而非删除。数值数据库读回按值语义验证。

# 4. API 实现

## 4.1 Proto

无，公开 Go SDK API。

## 4.2 TOP 网关注册

不适用；PRD 明确不新增 HTTP/gRPC 路由或云 OpenAPI。

## 4.3 接口实现详情

落实 PRD §3.2 建议签名，不新增 spec 未定义的 API 编号：

```go
func (r *Runner) UpdateSession(ctx context.Context, userID, sessionID string, stateDelta map[string]any) error
func UpdateSession(ctx context.Context, sessionService session.Service, appName, userID, sessionID string, stateDelta map[string]any) error
```

方法直接委托 helper，使用 r.sessionService / r.appName；helper 不接收后端配置，也不创建服务。参数错误包含具体缺失字段；遵循现有 fmt.Errorf 风格，不引入错误码体系。拒绝 nil service、空 appName/userID/sessionID，ctx 使用标准非 nil Context；对取消/超时返回包装的 ctx.Err，使 errors.Is 可识别。nil Runner/零值 Runner 可防御性返回错误，不发生 panic。字符串标识不进行 trim 或重写。

流程：参数与取消校验 → 同服务 Get 最新目标 → 检查返回会话有效 → 创建增量副本并过滤 strings.HasPrefix(key, session.KeyPrefixTemp) → 无持久 key 则成功 → json.Marshal 校验持久增量 → 再次检查取消 → session.NewEvent(ctx, platform.NewUUID(ctx)) → Author="system"、Actions.StateDelta=副本 → 同服务 AppendEvent → 返回结果。Get 失败优先于增量校验；空增量仍验证目标存在。JSON 校验只针对过滤后的增量，不通过 JSON 往返改变输入值类型，不修改调用方 map。新增接口不提供输入嵌套对象的并发修改支持，调用方应保持输入在调用期间稳定。

Event ID、InvocationID、Timestamp 使用官方 platform/NewEvent；无 Content，Partial=false，不冒充模型消息。空增量不生成事件、不改变更新时间。Get/AppendEvent/序列化/取消错误加入操作上下文并用 %w 保留原因，避免输出增量值或连接串。AppendEvent 只调用一次；失败不尝试补偿或自动重试，真实存储结果由新 Get 判断。

# 5. 服务编排

REQ-001/008：创建服务 → Runner.New 归一化 → 通过同实例 Create 或既有 Run 创建会话 → UpdateSession → 新 Get / 后续 Run。更新路径不使用 AutoCreateSession、不执行 Agent、Model、Tool 或长期记忆插件。显式 helper 的调用方必须传最终选中的服务和匹配 appName。

数据库事务与 stale 检查由 ADK 负责。Get+AppendEvent 不构成 CAS，不添加应用锁，不承诺并发强一致；同会话 Run 与更新由调用方串行化。保留底层锁/stale 错误，响应丢失后调用方先读回，不盲目重发。

# 6. 状态机

没有新增业务状态机；允许任意基础 session key。

| ACTION | 前置状态 | 目标状态 | 实现位置 |
|---|---|---|---|
| UpdateSession 非空持久增量 | 目标存在 | state 顶层合并，新增一个 ADK Event | runner/session_update.go → session.Service.AppendEvent |
| UpdateSession 空/仅 temp 增量 | 目标存在 | 状态、历史、更新时间不变 | helper 在 Get 后返回 |
| 参数错误 / 目标不存在 / 无法序列化 / 追加失败 | 任意 | 返回错误；前置失败不追加事件 | helper 校验与错误包装 |

# 7. 异步任务

无。

# 8. 外部依赖

无新增依赖或跨仓实现。复用锁定的 google.golang.org/adk/v2 v2.1.0、现有 GORM 和三种 dialector；本地内存为官方 InMemoryService。Context 传给底层 Get/AppendEvent，调用方设置超时；新增入口不加重试、不调用模型或云 API。

# 9. 配置与 Feature Gate

无新增配置或 Gate。SessionService 优先级、AutoCreateSession、AutoSaveSession 行为均按现有运行时。SDK 无额外鉴权，调用方负责 app/user 访问权限；app: 作用于同 app，user: 作用于同 app/user，普通 key 仅目标 session，temp: 不进入新接口的历史和持久状态。

# 10. 上线策略

| 步骤 | 动作 | 备注 |
|---|---|---|
| 1 | 用户确认本设计，实施并完成单测、构建、标准检查和 pre-commit hook | 当前节点 |
| 2 | 开发分支提交推送并核验远端，交接四后端新 API e2e | 下一节点验证 |
| 3 | e2e 通过后通过 PR 合入 main，随 V2 SDK 发布 | 合并节点；不直接推 main |

回滚预案：客户端改回原生 Get+AppendEvent 路径，再正常回退新增 SDK 入口；无 schema 回滚或历史数据清理。

# 11. 风险

| 风险 | 影响 | 缓解 |
|---|---|---|
| MySQL 严格模式 app:/user: 共享态零日期问题 | PRD 已复现混合前缀失败；user-only 尚未实测 | 透传错误并明确支持边界；下一节点补 user-only 及事务回滚，不放宽 sql_mode |
| 官方内存历史未过滤 temp | 临时信息可能进入存储历史 | helper 在副本中过滤，不扩展修复原生服务 |
| 同会话并发 / 响应丢失 | stale 或重复事件；不保证 CAS | 串行化调用、透传失败、不自动重试，读回判断 |
| 内存后端忽略 ctx 取消 | 已取消请求仍可能写入 | helper 在 Get 前、空返回前与 AppendEvent 前检查 ctx.Err；不宣称检查与写入原子化 |
| 大增量 / 长会话 Get | JSON 校验开销和历史读取开销 | 不引入无依据的额度/分页语义；沿用标准 Get，复杂度与输入/已有历史相称 |
| 日志含敏感 state | 输入内容泄漏 | 不记录增量值，不新增持久化日志；返回操作上下文和原错误链 |
| 默认技术设计模板未物化 | 无法按原目录引用 | 使用同步产品模板，已明确不适用项；无需新增平台能力 |

设计自检：REQ-001–009 均映射接口、测试或文档；文件清单覆盖入口、验证与说明；无依赖升级/降级、数据迁移或其他仓库修改；错误、取消、前缀、并发、权限、持久化及回滚已明确。用户已通过工作流交互确认方案并要求继续开发与单元测试。编码后的 NFR 自检按 skill 补充兼容性声明，未改变确认的 API/业务范围。

# 12. 测试要点

| 场景 | 方法 | 关联 REQ |
|---|---|---|
| 合并、覆盖、保留、多 key、null、嵌套整体替换 | 内存与临时 SQLite 文件服务单测，新 Get 断言状态/一个事件 | 003/004/008 |
| 四种配置组合 | 默认内存、ShortTermMemory、SessionService、二者同时提供；断言更新/Run 使用所选实例 | 001/008 |
| 不执行 Agent / 长期记忆 | 记录调用次数，更新本身不触发 Agent 或保存长期记忆 | 001 |
| app/user/session 隔离与前缀共享 | 内存/SQLite 多会话新 Get；事件无 temp | 002/005 |
| 空 map、nil、仅 temp、混合 temp、输入保护 | 状态/事件数/更新时间不变；原 map 不变 | 005/006 |
| 空标识、nil service、missing、AutoCreate=true | 可识别错误；目标仍不存在，无事件追加 | 002/007 |
| 不可序列化与有限数值 | 函数、channel、循环对象、NaN/Inf 拒绝；持久化无变化，temp-only 不校验被过滤值 | 003/007 |
| 底层 Get/AppendEvent 失败与取消 | 沿用包内 fake/recording 风格，errors.Is/As 检查链；AppendEvent 最多一次 | 007 |
| 标准 Event | 非空 ID/InvocationID、有效时间、Author system、Content nil、Partial false、连续更新保留 key | 004/007 |
| 既有运行语义 | 后续无模型 custom Agent 读取更新态，原生 AppendEvent/WithStateDelta 不回归 | 008/009 |
| 四后端持久化与 MySQL user-only/stale | 下一节点独立 e2e，按 PRD §4，真实数据库及新进程 | 007/008 |

编码阶段标准命令：changed Go 文件 gofmt；go mod tidy -diff；go build -mod=readonly -v ./...；go test -mod=readonly -v -race -count=1 -shuffle=on ./...（覆盖率文件置仓库外）；必要时先定向 ./runner ./memory/...。保留现有 hook，直接执行当前 core.hooksPath 下的 pre-commit，提交时正常运行 hook；当前 Git 无 hook 子命令，无仓库 pre-commit YAML，不虚构新检查入口。执行前核对工具/依赖可用性，失败如实区分本次变更与环境/存量问题。提交前 fetch main 并使用非破坏的 merge-tree/祖先关系核验冲突；当前 Git 无 merge-tree --write-tree，main 为 HEAD 祖先时通过祖先关系确认，否则使用旧版三参数 merge-tree 检查。推送工作分支后对比远端 SHA。每个修改代码文件在最终交付给出可点击链接及职责说明。

## 当前分支交付记录

本设计源自历史共享提交 `c9ed9f51eb495c144ff91b30b4b6264501155aca`，上文的开发节点/历史 HEAD 说明保留作溯源。负责人已明确选择当前 `feat/4f204cd7-z82c` / `c87e0f6ae782d57bee126429bf333ccb7e521a93` 作为实际实现基线。本次只补齐设计、PRD、交付记录和兼容声明，不合并另一侧历史。公开 API、示例及锁定依赖与已 E2E 验证的 c9ed9f5 无差异；真实四后端结果和未完成 CI 门禁见 [task.md](task.md)。MySQL 严格模式首次 user-only 更新已独立复现失败并确认回滚，补足上文历史未验证项；不改变既定支持边界。
