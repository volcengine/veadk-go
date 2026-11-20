# VeADK TPOT 跨语言需求同步判断

**结论：需要 Go 跟进；本任务仅同步 Python PR #1223 的 SDK 接收侧 TPOT 增量。** 这是 Model / Observability 核心能力，现有 Go 同名 Histogram 未连接到业务流，已通过用户 Agent 路径复现。Go 使用官方 Ark SDK 差异化接入，无需复制 Python LiteLLM 实现。

## 1. 来源、范围与责任

来源：GitHub MR 同步，[volcengine/veadk-python#1223](https://github.com/volcengine/veadk-python/pull/1223)，feat(tracing): record streaming time per output token，合并于 2026-11-18 09:25:19 UTC，commit 1763e5a548a952449d87e0ccfc11c2ec8d78c8cc。GitHub API 核实 7 个文件 +909/-25，含 656 行 TPOT 测试及双语 tracing 文档。MR author wujiaming-ai；commit authors 信息来自需求输入。

目标：volcengine/veadk-go main 8034d8e9680e6ce87500bc1370aadf80deb3645f，工作分支 feat/687dfb82-mj7o；实时共享 context_version=5，resolved。Meego 7399067096，P3；创建人/负责人/RD 单春华、PMO 丁彦俊，已回读确认。已存在的本工作项不重复创建；历史查重信息沿用输入，不将其当成本轮重新执行的全项目查重。

**能力归属**：统一模型流式性能指标属于“封装原生 ADK + 适配火山引擎”的核心观测能力。此次不涉及 Python Studio、Harness、CLI、MPA、Codex Runtime、packaging 或生态专属实现。Java 是否跟进应在独立 Java 任务中调研，不在此 Go 任务内新增仓库或生成未经核查的 Java 结论。

## 2. 增量与语言差异判断

| 本次 Python 增量 | Go 现状与证据 | 同步决策 |
|---|---|---|
| 首末有效增量的 SDK 接收时间 | model/ark.go Recv 已有文本、推理、工具参数，但无计时 | 补齐；Go 使用单调 time，按原始 SDK 包计一次，避免多个 ADK yield 重复 |
| 可信终态 + 最终累计 Token | Go 有 FinishReason、Usage；缺失原因时补 stop | 补齐可信证据，拒绝 EOF/DONE/兜底 STOP；原始 completion_tokens 作分母 |
| model_request 属性与同名 Histogram 一次计量 | Go Histogram 定义存在，无调用；原生 ADK Span 翻译为 call_llm | 复用 Histogram，新建 SDK 请求 model_request；不将属性复制父 Span |
| 清理后提交、错误/取消省略 | Go defer Close 丢弃错误；yield 可提前停止 | 跟进生命周期保证；不改变既有业务返回或错误语义 |
| 计数范围检查、并发/重试、未采样指标 | SDK 有 reasoning_tokens，Go 转换未传细分；存在鉴权重试 | 独立请求状态与原始 usage 检查；复用 OTel，采样与指标解耦 |
| CustomStreamWrapper.chunks / LiteLLM SDK 检查 | Go 使用 Ark 官方 SDK v1.1.53 | 不复制；调研官方字段/Recv/Close，未知计数范围保守省略 |
| Ark Responses 与 Python 双语站点实现 | Go 本需求限定已有 Chat Completions | 不新增 Responses；Go 既有可观测文档更新指标解释即可 |

共享契约为名称、秒单位、(末输出-首输出)/(completion_tokens-1)、请求级一次计量及省略条件。Go 工具有效输出以非空参数为准；源 Python 的 classifier 包括函数名，这一适配粒度在 Go PRD 明确，不把名字/角色包计成 Go 有效生成。完整推理已含在 completion_tokens 时不重复叠加，不能把 SDK 未表示的字段默认为已排除。

## 3. 端到端对比与验证结果

2026-11-20 在 fetch 后 Go 最新 main 与 Python 源合并提交执行。用户代码独立于测试：用户创建 VeADK Agent + Runner，真实语言 SDK 请求本地 SSE；用户程序写业务事件与遥测文件/内存快照；测试程序从文件读取并断言，不直接调用 TPOT recorder 模拟成功。

| fixture | Python 源提交实际结果 | Go 最新 main 实际结果 | 判断 |
|---|---|---|---|
| 文本 hello / world，间隔发送 0.99 s，真实 stop，尾部 usage 延迟 0.15 s，Token=100，DONE | model_request 属性 0.009847472656565655 s；Histogram count=1 同值 | partial=2、最终 STOP、Token=100；已有 Token usage/耗时可导出；TPOT 样本数=0 | 正常业务链路确认 Go 缺口 |
| 同样输出/usage，缺少 finish_reason，仅 DONE | TPOT count=0、无属性 | 最终仍为 STOP、Token=100；TPOT 样本数=0 | Go 下游 STOP 不能证明上游正常终态 |
| 源 72 项契约测试 | 72 passed / 37 warnings | 本轮未实现 Go 契约测试 | 源契约有证据；目标完整边界仍需开发验收 |

**范围限制**：此为本地 SDK HTTP E2E，未调用真实云模型，未验证云平台导出或 AgentKit Console；为确定性注入终态/usage并避免外部费用跳过云端。本轮无控制台 agentid；本地 app=tpot-prd、user=local-user、session=local-session。源契约测试包括精确 0.01 s，网络实测允许 SDK 接收与发送时间差，不能按 sleep 宣称精确值。

## 4. 同步范围、验收与待确认

目标语言完整行为以同目录 [Go PRD](prd.md) 的 REQ-001～004 为唯一规格：可信成功流一次计量；无可信证据省略；Trace 不采样时指标仍记录；并发重试隔离；原有业务与异常不变。0.99 s / (100-1) 精确结果为 0.01 s；增加尾部 usage/Close 延迟不改变结果。非流式、单增量、非法/缺失 usage、多候选、缓存、未知 SDK 范围、隐藏推理/音频、异常/取消/超时、无正常终态、Close 失败等必须分别验收。

Go 官方 ADK v2.1.0 已有模型 Span、Ark SDK v1.1.53 已有 Recv/FinishReason/CompletionTokens/ReasoningTokens/Close error，应复用，不重复造流解析器，不降级依赖。参考 [LangChain streaming usage](https://docs.langchain.com/oss/python/integrations/chat/openai) 的尾部 usage chunk，以及 [OTel GenAI server TPOT](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-metrics.md) 的秒制 Histogram；后者为服务端观测，不能等同 SDK 接收侧指标。未运行这两个参考实现。

**开发设计阶段需确认**：Go 维护者与 RD 单春华说明不匹配输出在 SDK 未声明字段情况下如何可靠识别、完整推理范围证据、清理失败与取消路径；发布版本/支持矩阵由维护者在发布前确认。保守省略已是确定产品规则，不等待这些映射问题来放宽契约。

**交付状态**：需求与同步判断可供设计/开发评审；本轮不实现 Go 埋点，不宣称开发/发布完成。现有 Histogram 注册不等于采集能力已上线。

## 5. 附录：调研及可执行验证

完整用户 main.go / Python Agent、独立 SSE fixture 与文件断言、依赖与复现步骤见 [PRD 附录 7](prd.md#7-附录可执行调研与验证代码)，临时代码在仓库外运行，仅文档附录保留。下列命令及结果足以定位本轮证据；重新运行时先按 PRD 附录保存代码并设置临时 Go module 的本地 replace。

```bash
agentnative workflow repositories status --available --json
git -C repos/volcengine_veadk-go fetch origin main
git -C repos/volcengine_veadk-go rev-parse origin/main
# 8034d8e9680e6ce87500bc1370aadf80deb3645f
rg -n 'RecordStreamingTimePerOutputToken|streaming_time_per_output_token' repos/volcengine_veadk-go
# 只有函数/Histogram 定义与文档，无业务调用
cd /tmp/tpot-go-validation
go build -mod=readonly -o agent-app .
python verify.py
python verify_python.py
cd /tmp/veadk-python-tpot-research
uv run --extra dev pytest tests/test_model_tpot.py -q
# 72 passed, 37 warnings in 7.45s
```

独立结果核查代码（结果文件由 PRD 附录的 E2E 测试生成）：

```python
import json
from pathlib import Path
base=Path('/tmp/tpot-go-validation')
go=json.loads((base/'results.json').read_text())
py=json.loads((base/'python-results.json').read_text())
assert all(x['partial_events']==2 and x['final_finish_reason']=='STOP' for x in go)
assert all(x['tpot_samples']==0 and x['tpot_attributes']==0 for x in go)
assert all('gen_ai.client.token.usage' in x['metric_names'] for x in go)
assert py[0]['scenario']=='normal' and py[0]['tpot_samples']==1
assert py[0]['tpot_span_values']==[py[0]['tpot_sum']]
assert py[1]['scenario']=='no-terminal' and py[1]['tpot_samples']==0
print('PASS: Python/Go 用户流式链路差异已确认，遥测启用对照正常')
```

实际：上述断言通过；Go 正常与无终态均 TPOT 样本数=0，Python 分别一次与省略。预期修复后：Go 正常流一次，缺少真实终态省略；工具/完整推理、并发、重试、Close error、采样开关等按 PRD 继续验证。云端跳过影响仅限真实服务元数据和云平台集成证据，不能由本地测试替代。
