# Review Summary

- **本次需求**：为 Go 现有 Ark Chat Completions 流式请求补齐 SDK 接收侧 TPOT 采集与上报。
- **需求来源**：GitHub MR 同步，Python PR #1223；Meego 7399067096，P3。
- **变更面**：Model 原始流式接收与清理、Observability Span/Histogram、文档、隔离验证；复杂度 medium。
- **判断依据**：源 MR 增量、Go main 8034d8e、官方 ADK/Ark SDK 源码、两语言 Agent E2E 结果。
- **差异判断**：Python LiteLLM 内部结构不复刻；Go 使用 Ark 官方 SDK。站点不新增差异；支持版本矩阵由维护者确认。
- **本次变更关键信息**：复用现有 Histogram；新增请求级计时与真实终态校验；维持业务输出和异常；尾部 usage/清理延迟不计入。
- **Review 重点**：SDK 增量与 ADK 输出粒度不同；EOF/兜底 STOP 不等于正常终态；隐藏输出计数风险；关闭错误与取消；未采样指标独立性。

## Scope Note

- Implemented sections：来源与目标、现状验证、行为契约、Metrics、验收、SDK 调研、兼容发布、Open Questions、附录。
- Not involved：前端 UX、CLI、业务持久化 schema、权限模型、Dashboard；现有公开 SDK 签名/输出格式不变。
- Needs confirmation：SDK 未表示字段的可靠识别路径、维护者支持版本矩阵；这些问题沿用保守省略规则，不放宽验收。
- Reason：只补齐本次 MR 引入的语言无关核心 Model/Observability 能力。

# Spec: VeADK-Go 流式每输出 Token 耗时（TPOT）采集与上报

## 1. 需求来源与目标

| 项目 | 内容 |
|---|---|
| 来源 | GitHub MR 同步；[Python PR #1223](https://github.com/volcengine/veadk-python/pull/1223)，合并于 2026-11-18 09:25:19 UTC |
| 源提交 | 1763e5a548a952449d87e0ccfc11c2ec8d78c8cc；7 文件，+909/-25；author wujiaming-ai |
| 目标仓库 | volcengine/veadk-go，Model / Observability；只覆盖 Ark Chat Completions 流 |
| 基线与分支 | main：8034d8e9680e6ce87500bc1370aadf80deb3645f；交付分支 feat/687dfb82-mj7o |
| 共享上下文 | 执行时 CLI context_version=5，repository_resolution=resolved，目标 Go@main；输入版本 4 已由实时上下文更新 |
| 人员 | 创建人、负责人、RD：单春华；PMO：丁彦俊；Meego 查询回读核对，优先级 P3 |
| 产品定位 | 封装原生 ADK 能力并适配火山引擎；模型流式性能观测属于核心能力 |

**用户问题**：开发者与运维人员跨 Python/Go 排查模型流式生成时，当前 Go 有指标定义却无请求样本，无法使用统一 TPOT 比较性能。

**目标**：符合条件的每次 SDK 请求输出一次相同含义的 Span 属性与 Histogram；无可靠证据时省略。

**非目标**：不新建 LiteLLM、Ark Responses 或 Provider 接入，不统计客户端渲染，不重做 TTFT、建设 Dashboard、补写历史指标或改动业务默认流程。Java 不在本需求仓库与验证范围，其同步另行处理。

## 2. 调研现状与验证结论

**验证类型为同步缺口验证**，并非把已上线 Go 需求重新归类为开发者 Bug。2026-11-20 fetch 最新 main 后，从 VeADK Agent → Runner → 官方 Ark SDK → 本地 SSE → 事件文件与遥测文件完整执行，再用独立测试程序读取文件断言。

| 证据 | 当前行为与影响 |
|---|---|
| observability/constant.go:70、metrics.go:157、253 | 同名 Histogram 已有，单位 s；全仓无 RecordStreamingTimePerOutputToken 调用，复用而不重复注册 |
| model/ark.go:437 generateStream | IncludeUsage 已开启；收取 Usage、choices[0]、文本、ReasoningContent、工具参数；缺少首末有效输出计时 |
| model/ark.go 流式 cleanup 与 final response | defer stream.Close() 丢弃关闭错误；未获 finishReason 时补 stop；最终 yield 结果未用于判断完整消费 |
| SDK → ADK 转换 | 同一原始增量的推理、文本可能分两次 yield；工具参数积累后才输出，不能在下游事件处计 TPOT |
| buildArkUsageMetadata | completion_tokens 转 int32 CandidatesTokenCount，未传 reasoning 细分；TPOT 应用原始 SDK int 计数，避免转型溢出和推理重复叠加 |
| ADK Go v2.1.0 | 有 generate_content Span，Go 翻译导出成 call_llm；当前无 model_request Span、无 TPOT 实现 |

**实测对比**（相同本地 SSE，首末发送间隔 0.99 s，尾部 usage 延迟 0.15 s，completion_tokens=100）：

| 场景 | Python 源合并提交 | Go 最新 main |
|---|---|---|
| stop + usage + DONE 正常流 | Histogram count=1，sum=0.009847472656565655 s；model_request 属性完全相同 | 两个 partial，最终 STOP / output=100；TPOT 样本与属性均为 0 |
| 无 finish_reason，仅 usage + DONE | Histogram count=0，属性省略 | 仍返回最终 STOP / output=100；TPOT 无样本 |
| 已有遥测对照 | model_request、generate_content、call_llm 等 Span | call_llm、invoke_agent、invocation；Token usage、operation duration、chat count 正常导出 |

Python 72 项源契约测试通过，37 条依赖弃用警告。Go 缺口已在用户路径复现；无终态场景说明不能从转换后的 STOP 判断成功。实测值是 SDK 接收间隔，受连接及调度影响，不把服务器发送 sleep 当成精确时钟；精确 0.01 s 由确定性计时测试验收。

**验证限制**：本轮使用真实语言 SDK 连接本地 HTTP SSE，未调用真实云模型；为控制外部费用并确定性注入 usage/终态，没有使用云凭据。本轮无 AgentKit 控制台交互，无控制台 agentid；本地 app=tpot-prd、user=local-user、session=local-session。仅 Go 正常与无终态两场景是本轮 Go E2E 已执行，其余边界为开发验收要求，未声称已通过。

## 3. 需求范围与行为

**REQ-001（P3）：单请求一次计量。** 对单候选、完整消费且具备上游真实正常结束原因、清理成功、至少两个有效输出增量、可信最终整数输出 Token > 1、计数范围一致的流，在该 SDK 调用的 model_request Span 写一次 TPOT 浮点秒属性，向同名 Histogram 记录一个样本。不向父 Span 复制；Histogram 不依赖 Trace 采样。真实正常原因限定 stop、tool_calls、兼容的 function_call；不得使用 EOF、DONE 或下游补出的 STOP 代替正常原因。工具结束须继续满足完整消费及清理条件。

**REQ-002（P3）：SDK 接收侧计量。** TPOT = (末个有效输出增量接收时间 - 首个有效输出增量接收时间) / (最终累计 completion_tokens - 1)。使用单调时间。非空文本、完整推理增量、非空工具参数属于有效输出；仅角色、空字符串、usage、结束包或只有工具标识/函数名不构成 Go 本需求有效输出。同一 SDK 增量多个字段只算一个有效增量，但保留全部观测输出类别以校验计数范围。首输出前等待、尾部 usage、清理、后续 Agent 工具执行不计；首末之间的网络、SDK 调度、消费等待计入。真实测得 0 s 有效；负值、非有限值省略。

分母使用上游最终累计 completion_tokens 原值，不累加多个 usage 包，不使用字符/chunk/本地估算，不用响应总耗时替代。completion_tokens 已含 reasoning 时不再次叠加；reasoning_tokens 非法、超过总输出或有隐藏推理而未观测完整推理时省略；推理摘要、音频等造成计数范围不匹配时省略。

**REQ-003（P3）：无法可信计量即省略。** 非流式、无有效输出、只有一个有效增量、usage 缺失/非法/<=1、多候选（含多个 choice 或非零候选 index）、缓存回放、未知 SDK 包装、计数范围不匹配、异常、超时、取消、主动终止、length 截断、过滤、缺少真实正常终态、清理失败，均无 TPOT 属性或样本；不补 0。终态后再有有效生成或多次完成且无法确认契约时保守省略。取消包括收到终态后、完成清理和采集之前被取消。已有且已观测的 TTFT 保留。

**REQ-004（P3）：并发、重试与业务隔离。** 并发调用状态隔离；VeADK 可观测重试分别计量，失败尝试无 TPOT、成功尝试一次；官方 SDK 隐藏重试属于同一观测调用。采集/导出失败不改变生成内容、顺序、工具参数、usage、结束状态、原有错误或取消传播；关闭失败只阻止新增 TPOT，业务侧继续沿用既有关闭错误处理语义，不借本需求新增业务错误。沿用现有鉴权、采样和遥测启用配置；指标关闭时不记录 Histogram，不新增调用者必填计时参数或高基数维度；新增埋点不存提示、内容、工具参数或凭据。可用内存观测或等价隔离路径完成验证。

**Metrics 契约**：

| 字段 | 值 |
|---|---|
| Span 属性 / Histogram 名称 | gen_ai.chat_completions.streaming_time_per_output_token |
| Span 归属 | 本次 SDK 调用的 model_request；现有 call_llm / ADK Span 保留 |
| 类型、单位 | 浮点、Histogram、s |
| Histogram 维度 | gen_ai.request.model（沿用请求模型）、gen_ai.operation.name=chat |
| 触发 | 上述可信条件全部满足，清理成功后记录一次 |
| 聚合语义 | sum/count 为按请求平均 TPOT；并非按 Token 加权平均 |

**Backend / SDK 集成边界**：复用官方 Recv / Close 与现有 OTel Histogram。如何传递调用级状态、处理尾部清理和请求 Span 生命周期由开发设计；验收以原始终态、原始 usage、原始增量接收时间为准，不要求跨语言复制类结构。SDK 会丢弃未声明 JSON 字段，不能据此声称音频或未知字段已被排除；开发设计需说明当前支持范围与可靠识别方式，无法证实的范围省略。

## 4. 验收标准与验证要求

**EARS**：当满足 REQ-001、可控单调时钟首末间隔 0.99 s、completion_tokens=100 时，VeADK-Go 应产生 TPOT=0.01 s，model_request 属性与 Histogram 样本相同、count=1。增加尾部 usage 或清理 5 s 时应仍为 0.01 s。取消或缺少真实正常终态时，应无该属性和样本，保留原有业务与已观测 TTFT。Trace 不采样但指标开启时仍应有一个 Histogram 样本；指标关闭时无样本。

```gherkin
Scenario: 正常流式请求排除尾部延迟
  Given 单候选真实正常结束且清理成功的完整流，最终 completion_tokens 为 100
  And 至少两个有效增量且 SDK 首末接收间隔为 0.99 秒
  When 尾部 usage 和连接清理额外等待 5 秒
  Then model_request 的 TPOT 为 0.01 秒且 Histogram 仅记录一次
Scenario: 请求取消
  Given 已收到两个有效增量
  When 流在完成并清理成功前被取消
  Then 不产生 TPOT 属性或样本，且保留已有 TTFT 与原有取消行为
Scenario: 缺少真实终态
  Given 收到有效输出和最终 usage，但未收到 finish_reason
  When SDK 返回 EOF 且兼容层返回 STOP
  Then 不产生 TPOT 属性或样本
```

| 测试组 | 必须分别验证的输入 | 通过标准 / REQ |
|---|---|---|
| 精确公式 | 0.99/99；实测 0；尾部 usage/Close 延迟；中间消费等待 | 0.01、有效 0；排除尾部，包含中间等待；REQ-002 |
| 输出粒度 | 文本、完整推理、工具参数、同包多字段、仅角色/名字/空字段 | 原始 SDK 接收计时；同包一次，多类别校验；REQ-002 |
| Token 证据 | 多个累计 usage；缺失、0/1/负数、非法值、超 int32；推理已含/隐藏/非法；音频/摘要 | 使用最终原始 int，不累加/重复推理；范围不一致省略；REQ-002/003 |
| 终态与消费 | stop/tool_calls/function_call；length/filter；无真实终态；提前 break；异常/超时/取消；Close 失败 | 成功一次、失败省略，内容和错误与 main 对照一致；REQ-001/003/004 |
| 多候选及包装 | 多 choice、非零 index、缓存、未知包装/未知输出 | 不误计，多候选及不可靠范围省略；REQ-003 |
| 遥测配置 | Trace 采样关闭/开启、指标关闭/开启、采集/导出故障 | 指标与采样独立，不重复上报，不改变业务；REQ-001/004 |
| 并发重试 | 不同计时/usage 并发；VeADK 可见失败后成功；SDK 隐藏重试 | 无串值；每观测调用至多一次，失败无样本；REQ-004 |
| E2E 回归 | VeADK Agent/Runner + 本地 SSE + 独立事件/遥测断言 | 正常输出/工具参数/usage/finish/error 不变；符合条件有 TPOT，父 Span 无属性；全部 REQ |

开发自测应运行适用 model / observability 测试及仓库 CI 要求；上述基线验证代码留在附录，不把临时 app 或验证脚本作为生产代码提交。新增正式契约测试由开发节点维护。Go 本地 E2E 应使用实际 SDK，文件/内存读取或本地 OTLP receiver 均可；不直接调用 RecordStreamingTimePerOutputToken 代替业务链路验收。精确数值用确定性时钟；真实网络 E2E 按实际 SDK 首末接收时间计算且说明误差，不能按 fixture 发送 sleep 作精确断言。

真实云服务集成作为发布补充验证：由维护者在已有授权与隔离资源上执行，记录模型/Endpoint、SDK 版本和正常流实际 usage/终态；若经 AgentKit 控制台部署必须记录 agentid、部署版本及 trace 标识。跳过需明确原因与影响，不把本地 E2E 宣称为云端通过。失败记录场景、原始非敏感元数据、命令、版本、影响 REQ，修复后重测；无法可靠识别的服务范围按 REQ-003 省略。

## 5. 官方 SDK 与同类框架调研

| 来源与基线 | 发现 | Go 差异化要求 |
|---|---|---|
| [ADK Go v2.1.0 telemetry.go](https://github.com/google/adk-go/blob/v2.1.0/internal/telemetry/telemetry.go) | StartGenerateContentSpan 已有模型 Span，usage 采用 candidates+thoughts；本地源码无 TPOT | 保留原生 ADK 调用/Span；SDK 请求 model_request 单独计量，不通过 ADK 最终候选推算 |
| [Ark Go SDK v1.1.53 stream_reader.go](https://github.com/volcengine/volcengine-go-sdk/blob/v1.1.53/service/arkruntime/utils/stream_reader.go) | Recv 返回原始 chunk；Close 返回 Body.Close 错误；EOF 不含成功终态保证 | 复用 Recv/Close，记录真实 finishReason 和清理结果，不造另一套流解析器或降级依赖 |
| [Ark SDK common.go](https://github.com/volcengine/volcengine-go-sdk/blob/v1.1.53/service/arkruntime/model/common.go) / chat_completion.go | Usage.CompletionTokens 为 int、details.ReasoningTokens 为 int；choice 包含 Index、FinishReason、Delta | 原始 Token 作分母；单候选校验及推理范围检查；字段存在性与未知 JSON 丢弃风险需设计验证 |
| Python MR timing.py / timed_lite_llm.py | 单调计时、可信终态/usage、清理后一次记录；unknown wrapper 省略 | 共享产品契约；不复制 CustomStreamWrapper.chunks 等 LiteLLM 内部检查 |
| [LangChain ChatOpenAI](https://docs.langchain.com/oss/python/integrations/chat/openai) | stream_usage 提供额外 usage chunk；usage_metadata 用于 Token 统计 | 参考尾部 usage 与内容流分离，不以 usage 到达时间作末输出，不引入 LangChain 依赖；未验证其 TPOT 与本契约相同 |
| [OTel GenAI metrics](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-metrics.md) | gen_ai.server.time_per_output_token 描述服务器解码阶段秒值 | 本需求是 SDK 接收侧、已有 chat_completions 命名；明确服务端 TPOT 与此值的观测位置差异，不改共享指标名 |

**调研结论**：当前依赖已有所需主体能力，优先复用；不降级 Go、ADK 或 SDK，不为 TPOT 重建 Provider。已核对源提交内容与官方源码，已运行两语言用户链路；LangChain/OTel 为文档参考，未运行其框架或服务端实现。官方 SDK 原始字段比 ADK 转换后的 metadata 更适合作为可信证据。

## 6. Open Questions、兼容与交付状态

| ID | 问题与负责人 | 影响与关闭时点 |
|---|---|---|
| OQ-001 | Go 维护者 / RD 单春华：确认 supported finishReason、完整推理证据、异常清理与取消路径 | 字段与 Close error 已存在；开发设计阶段给可靠识别方案及测试，无法证实即省略 |
| OQ-002 | Go 维护者 / RD 单春华：SDK 未声明 audio/refusal/summary 等字段可能被 JSON 解码丢弃；如何界定与识别不匹配范围 | 不允许仅凭字段为零声称安全；开发设计前补验证，不改共享省略契约、不扩展 Provider |
| OQ-003 | Go 维护者：发布版本和 SDK/ADK 支持验证矩阵 | 当前已验证 Go 1.26.5、ADK v2.1.0、Ark SDK v1.1.53、OTel v1.43.0；发布前补支持边界，升级依赖必须复跑流契约 |

**兼容性**：新增遥测契约，API 签名、模型返回格式、工具执行与错误语义不变；沿用启用/鉴权配置。无存储迁移、历史补写或默认 Provider 变更。文档需明确单位、计量区间、分母、省略原因与聚合口径。

**发布与回退**：通过既有仓库发布流程，维护者确定版本。回退新增采集应恢复原有业务与其他遥测，无历史数据迁移。未完成 OQ 识别方案或未覆盖验收边界，不得宣称全量支持。

**当前状态**：Spec 与同步判断可供方案设计/开发评审；Go 功能尚未实现或发布。本轮不因 Meego 回读当前节点为“开发&自测”而扩展到编码；执行上下文指定节点为“需求澄清 & Spec设计”。本地通过只证明现状差异，开发节点需补齐未来验收测试。

## 7. 附录：可执行调研与验证代码

### 7.1 环境、命令与结果

验证日：2026-11-20 UTC；Linux amd64。Go 1.26.5（按 go.mod 自动下载）、ADK Go v2.1.0、Ark Go SDK v1.1.53、OTel Go v1.43.0。Python 3.11、google-adk 2.11.0、LiteLLM 1.83.14、volcengine-python-sdk 5.0.47、opentelemetry-sdk 1.42.1；Python 使用源合并提交，而非混入最新 main 增量。

```bash
# 只读核对；在 workspace root 执行
agentnative workflow repositories status --available --json
git -C repos/volcengine_veadk-go fetch origin main
git -C repos/volcengine_veadk-go rev-parse origin/main
rg -n 'RecordStreamingTimePerOutputToken|streaming_time_per_output_token' repos/volcengine_veadk-go
# main 输出：8034d8e9680e6ce87500bc1370aadf80deb3645f
# 官方 SDK 与 ADK 本地源码
cd repos/volcengine_veadk-go
go mod download google.golang.org/adk/v2 github.com/volcengine/volcengine-go-sdk
rg -n 'Close|Recv' "$(go env GOMODCACHE)/github.com/volcengine/volcengine-go-sdk@v1.1.53/service/arkruntime/utils/stream_reader.go"
rg -n 'CompletionTokens|ReasoningTokens' "$(go env GOMODCACHE)/github.com/volcengine/volcengine-go-sdk@v1.1.53/service/arkruntime/model/common.go"
rg -n 'StartGenerateContentSpan|TraceGenerateContentResult' "$(go env GOMODCACHE)/google.golang.org/adk/v2@v2.1.0/internal/telemetry/telemetry.go"
```

将以下 main.go 与 verify.py 保存到仓库之外的新临时目录。本轮使用 /tmp/tpot-go-validation。复制目标仓库 go.mod/go.sum，变更临时 module 名并添加本地 replace；不修改仓库依赖、不提交临时文件。

```bash
# 在临时目录执行，TPOT_GO_REPO 指向 fetch 后的目标 checkout
cp "$TPOT_GO_REPO/go.mod" "$TPOT_GO_REPO/go.sum" .
python - <<'PYSETUP'
import os,pathlib
p=pathlib.Path('go.mod')
s=p.read_text().replace('module github.com/volcengine/veadk-go/v2','module tpot-validation')
s+='\nrequire github.com/volcengine/veadk-go/v2 v2.0.0\n'
s+='replace github.com/volcengine/veadk-go/v2 => '+os.environ['TPOT_GO_REPO']+'\n'
p.write_text(s)
PYSETUP
go build -mod=readonly -o agent-app .
python verify.py
# 本轮两个场景：partial_events=2，FinishReason=STOP，candidatesTokenCount=100
# metric_names 包含 gen_ai.client.token.usage / operation.duration / chat.count
# TPOT samples=0、attributes=0：正常场景确认缺口；无终态场景确认兜底 STOP 风险
```

注意 ADK Go session.Event 的事件 JSON 字段为 Partial、FinishReason、UsageMetadata（首字母大写），其内嵌 genai usage 字段为 candidatesTokenCount；测试只读输出，不把字段大小写误判为链路失败。File exporter 与 Plugin 的 metrics 开关均需启用，verify.py 要求已有 Token usage 存在，以排除遥测整体关闭造成的假阴性。Go sonic 对 Go 1.26 提示 fallback 到 encoding/json；两场景正常完成。

### 7.2 用户代码：Go Agent 程序 main.go

```go
package main
import (
 "context"
 "encoding/json"
 "os"
 veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
 "github.com/volcengine/veadk-go/v2/configs"
 "github.com/volcengine/veadk-go/v2/observability"
 verunner "github.com/volcengine/veadk-go/v2/runner"
 "google.golang.org/adk/v2/agent"
 "google.golang.org/adk/v2/plugin"
 adkrunner "google.golang.org/adk/v2/runner"
)
func main() {
 ctx:=context.Background(); enabled:=true
 must(observability.Init(ctx,&configs.ObservabilityConfig{OpenTelemetry:&configs.OpenTelemetryConfig{EnableMetrics:&enabled,File:&configs.FileConfig{Path:"telemetry.json"}}}))
 defer func(){must(observability.Shutdown(ctx))}()
 a,err:=veagent.New(&veagent.Config{ModelName:"fixture-model",ModelProvider:"ark",ModelAPIKey:"local-fixture",ModelAPIBase:os.Getenv("TPOT_BASE_URL")});must(err)
 r,err:=verunner.New(verunner.Config{AppName:"tpot-prd",Agent:a,AutoCreateSession:true,PluginConfig:adkrunner.PluginConfig{Plugins:[]*plugin.Plugin{observability.NewPlugin(observability.WithEnableMetrics(true))}}});must(err)
 f,err:=os.Create("events.jsonl");must(err);defer f.Close();enc:=json.NewEncoder(f)
 for event,err:=range r.Run(ctx,"local-user","local-session",verunner.UserText("fixture request"),agent.RunConfig{StreamingMode:agent.StreamingModeSSE}) {
  if err!=nil {must(enc.Encode(map[string]any{"error":err.Error()})); break};must(enc.Encode(event))
 }
}
func must(err error){if err!=nil{panic(err)}}
```

### 7.3 独立测试：SSE fixture 与文件断言 verify.py

```python
import http.server,json,subprocess,time,pathlib,threading
ROOT=pathlib.Path(__file__).parent
TPOT='gen_ai.chat_completions.streaming_time_per_output_token'
class Fixture(http.server.BaseHTTPRequestHandler):
 scenario='normal'; requests=[]
 def log_message(self,*a): pass
 def do_POST(self):
  request=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
  self.requests.append(request)
  self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers()
  def send(delta,finish=None,usage=None):
   chunk={'id':'local-completion','object':'chat.completion.chunk','created':1,'model':'fixture-model','choices':[] if usage else [{'index':0,'delta':delta,'finish_reason':finish}]}
   if usage: chunk['usage']=usage
   self.wfile.write(('data: '+json.dumps(chunk)+'\n\n').encode());self.wfile.flush()
  send({'content':'hello '});time.sleep(.99);send({'content':'world'})
  if self.scenario!='no-terminal': send({},'stop')
  time.sleep(.15);send({},usage={'prompt_tokens':5,'completion_tokens':100,'total_tokens':105})
  self.wfile.write(b'data: [DONE]\n\n');self.wfile.flush()
def objects(s):
 decoder=json.JSONDecoder();i=0;out=[]
 while i<len(s):
  while i<len(s) and s[i].isspace():i+=1
  if i==len(s):break
  obj,j=decoder.raw_decode(s,i);out.append(obj);i=j
 return out
def walk(x):
 if isinstance(x,dict):
  yield x
  for v in x.values():yield from walk(v)
 elif isinstance(x,list):
  for v in x:yield from walk(v)
server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Fixture)
threading.Thread(target=server.serve_forever,daemon=True).start()
results=[]
try:
 for scenario in ['normal','no-terminal']:
  Fixture.scenario=scenario
  for p in ['telemetry.json','events.jsonl']:(ROOT/p).unlink(missing_ok=True)
  import os
  env={**os.environ,'TPOT_BASE_URL':f'http://127.0.0.1:{server.server_port}/api/v3'}
  for k in list(env):
   if k.startswith('OTEL_'):del env[k]
  subprocess.run([str(ROOT/'agent-app')],cwd=ROOT,env=env,check=True,stdout=subprocess.DEVNULL)
  events=[json.loads(x) for x in (ROOT/'events.jsonl').read_text().splitlines()]
  telemetry=objects((ROOT/'telemetry.json').read_text());nodes=list(walk(telemetry))
  names=[n['Name'] for n in telemetry if 'Name' in n]
  metrics=[n for n in nodes if 'Name' in n and 'Data' in n]
  tpot=[n for n in metrics if n['Name']==TPOT]
  attrs=[n for n in nodes if n.get('Key')==TPOT]
  partial=[e for e in events if e.get('Partial')];final=[e for e in events if not e.get('Partial')]
  assert len(partial)==2 and final,events
  assert not tpot and not attrs,(tpot,attrs)
  assert any(n['Name']=='gen_ai.client.token.usage' for n in metrics),[n['Name'] for n in metrics]
  assert Fixture.requests[-1].get('stream') is True
  result={'scenario':scenario,'partial_events':len(partial),'final_finish_reason':final[-1].get('FinishReason'),'final_usage':final[-1].get('UsageMetadata'),'span_names':names,'metric_names':sorted(set(n['Name'] for n in metrics)),'tpot_samples':0,'tpot_attributes':0}
  results.append(result)
  (ROOT/f'{scenario}-events.jsonl').write_text((ROOT/'events.jsonl').read_text());(ROOT/f'{scenario}-telemetry.json').write_text((ROOT/'telemetry.json').read_text())
finally:server.shutdown()
(ROOT/'results.json').write_text(json.dumps(results,indent=2));print(json.dumps(results,indent=2))
```

### 7.4 Python 对比：源契约测试与独立 Agent E2E

```bash
git clone --depth 1 https://github.com/volcengine/veadk-python.git /tmp/veadk-python-tpot-research
cd /tmp/veadk-python-tpot-research
git fetch --depth 1 origin 1763e5a548a952449d87e0ccfc11c2ec8d78c8cc
git checkout --detach FETCH_HEAD
uv sync --extra dev
uv run --extra dev pytest tests/test_model_tpot.py -q
# 72 passed, 37 warnings in 7.45s
# 将下方用户 app 保存为 tpot_user_app.py；临时代码不提交
# verify_python.py 保存到 Go 临时目录；该目录已保存 verify.py 的 fixture
cd /tmp/tpot-go-validation
python verify_python.py
# normal: count=1, sum=0.009847472656565655 s, Span 属性同值
# no-terminal: count=0, 无 TPOT 属性
```

用户 Agent 代码 tpot_user_app.py：

```python
import asyncio,json,os
from veadk import Agent
from google.adk.runners import Runner
from google.adk.agents.run_config import RunConfig,StreamingMode
from google.adk.sessions import InMemorySessionService
from google.genai import types
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import InMemoryMetricReader
from veadk.tracing.telemetry import portal_metrics
async def main():
 exporter=InMemorySpanExporter();provider=TracerProvider();provider.add_span_processor(SimpleSpanProcessor(exporter));trace.set_tracer_provider(provider)
 reader=InMemoryMetricReader();metrics=MeterProvider(metric_readers=[reader]);portal_metrics.metrics_api.get_meter=metrics.get_meter
 portal_metrics.portal_metric_recorder=portal_metrics.PortalMetricRecorder('tpot-e2e')
 a=Agent(name='tpot_agent',model_name='fixture-model',model_provider='openai',model_api_base=os.environ['TPOT_BASE_URL'],model_api_key='local-fixture',instruction='Return the fixture.')
 sessions=InMemorySessionService();await sessions.create_session(app_name='tpot-prd',user_id='local-user',session_id='local-session')
 runner=Runner(agent=a,app_name='tpot-prd',session_service=sessions)
 events=[]
 async for event in runner.run_async(user_id='local-user',session_id='local-session',new_message=types.Content(role='user',parts=[types.Part(text='fixture request')]),run_config=RunConfig(streaming_mode=StreamingMode.SSE)):
  events.append(event.model_dump(mode='json',exclude_none=True))
 data=reader.get_metrics_data()
 name='gen_ai.chat_completions.streaming_time_per_output_token'
 points=[p for r in data.resource_metrics for s in r.scope_metrics for m in s.metrics if m.name==name for p in m.data.data_points] if data else []
 spans=exporter.get_finished_spans()
 result={'events':events,'tpot_samples':sum(p.count for p in points),'tpot_sum':sum(p.sum for p in points),'tpot_span_values':[s.attributes[name] for s in spans if name in s.attributes],'span_names':[s.name for s in spans]}
 open('python-e2e.json','w').write(json.dumps(result,indent=2));provider.shutdown();metrics.shutdown()
asyncio.run(main())
```

独立测试 verify_python.py（读取共享 fixture，不执行 Go 断言）：

```python
import ast,pathlib,threading,http.server,subprocess,os,json
ROOT=pathlib.Path(__file__).parent
# Reuse only the independent HTTP fixture definition; do not execute Go checks.
tree=ast.parse((ROOT/'verify.py').read_text());definitions=[n for n in tree.body if isinstance(n,(ast.Import,ast.ImportFrom,ast.ClassDef))]
exec(compile(ast.Module(body=definitions,type_ignores=[]),'fixture','exec'))
server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Fixture);threading.Thread(target=server.serve_forever,daemon=True).start()
results=[]
try:
 for scenario in ['normal','no-terminal']:
  Fixture.scenario=scenario
  env={**os.environ,'TPOT_BASE_URL':f'http://127.0.0.1:{server.server_port}/api/v3','OTEL_SDK_DISABLED':'false'}
  subprocess.run(['uv','run','python','tpot_user_app.py'],cwd='/tmp/veadk-python-tpot-research',env=env,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  raw=json.load(open('/tmp/veadk-python-tpot-research/python-e2e.json'))
  assert raw['tpot_samples']==(1 if scenario=='normal' else 0),raw
  if scenario=='normal':assert abs(raw['tpot_sum']-.01)<.001 and raw['tpot_span_values']==[raw['tpot_sum']],raw
  result={k:v for k,v in raw.items() if k!='events'};result['scenario']=scenario;results.append(result)
finally:server.shutdown()
(ROOT/'python-results.json').write_text(json.dumps(results,indent=2));print(json.dumps(results,indent=2))
```

Python 内存 recorder 替换仅隔离导出，Agent/Runner、LiteLLM SDK、HTTP SSE 都实际执行，不替换 ModelTiming 或注入计时结果。两语言同一 fixture 验证真实终态与 usage 口径。Python 源 72 项测试另外使用可控时钟，覆盖精确公式与 SDK 生命周期；不能与云端性能测试混同。
