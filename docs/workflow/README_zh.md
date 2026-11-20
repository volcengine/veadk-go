# Workflow 场景化使用设计

> 本文档描述 VeADK Go 对 ADK `workflow` 能力的场景化封装。文中的“旧写法”基于 `google.golang.org/adk/v2/workflow` 原生 API；“新写法”基于当前仓库新增的 `github.com/volcengine/veadk-go/v2/workflow` 初版封装。

## 整体思路

ADK v2 已经提供了一套底层 workflow 图编排能力：用户可以创建 `workflow.Node`，用 `workflow.Edge` 连接节点，再通过 `workflowagent.New` 把图包装成标准 `agent.Agent`。

这套能力很完整，但对业务开发者暴露了较多底层细节：

- `workflow.Start`
- `workflow.Edge`
- `workflow.JoinNode`
- `session.Event.Output`
- `session.Event.Routes`
- `workflow.NewRequestInputEvent`
- `workflow.NodeConfig`
- `workflowagent.Config.SubAgents`

VeADK 的封装目标不是重写一套 workflow engine，而是让用户仍然以 `Node` 为中心描述业务流程，同时隐藏这些底层细节。

建议的使用心智是：

```text
Node 接收 input，执行 Go 函数、工具或 Agent，然后输出给后续 Node。

Then            表达顺序
RouteNode/Case  表达条件分支
Parallel/Merge  表达并行和合并
DynamicNode     表达运行时调度、循环、动态 fan-out
AgentNode       表达 Agent 参与流程
AskNode         表达人工介入和恢复
ctx.EmitState   表达 workflow/session 级共享状态
ctx.EmitProgress 表达过程事件
ctx.Trace       表达业务可观测性
```

## 过程事件和可观测性

Workflow 不只有最终输出，长流程还需要把中间状态发出去。例如循环修复测试时，用户需要看到：

```text
第 1 次修复开始
补丁已生成
测试仍然失败
第 2 次修复开始
测试通过
```

当前 ADK 原生 workflow 的事件模型是：**node 在运行过程中 yield `session.Event`，workflow agent 把这些事件继续向 runner 透出**。也就是说，用户真正控制过程事件的位置不是 `workflowagent.New`，而是每个 node 的执行函数。

因此 VeADK 封装建议把事件能力挂到 `workflow.Context` 上：

```go
type Context interface {
	context.Context

	// EmitProgress 发送给调用方可展示的过程信息。
	// 封装层内部会转换为 session.Event，并补齐 InvocationID、Author、Branch、NodeInfo 等字段。
	EmitProgress(message string, fields ...workflow.Field) error

	// EmitState 更新 workflow/session state，同时作为一个过程事件被持久化。
	EmitState(values map[string]any) error

	// EmitOutput 显式把某个事件标记为当前 node 的输出。
	// 普通 FuncNode 通常直接 return；只有流式输出或 AgentNode 适配时才需要它。
	EmitOutput(value any) error

	// Ask 发起人工输入请求。第一次调用会发出 RequestedInput 并暂停；
	// 用户回复后重入该 node 时返回回复 payload。
	Ask(req session.RequestInput) (any, error)

	// Trace 在当前 node span 上记录业务事件或属性。
	Trace(name string, fields ...workflow.Field)

	// Span 返回当前 OpenTelemetry span，作为高级用法。
	Span() trace.Span
}
```

建议统一使用 `Progress` 命名，不使用 `Process`。`Progress` 表达“进度/过程消息”，和 ADK 原生 workflow 里对 output-less intermediate event 的称呼一致；`Process` 更容易被理解成 processor、处理流程或操作系统进程。

### Context 事件能力分类

`workflow.Context` 对外发送信息时建议分成五类。它们都发生在 node 内部，但目标接收方不同：

| 方法 | 用户 SSE 可见 | 进入 OTel span | 影响后继 node input | 是否持久化到 session | 典型用途 |
|---|---:|---:|---:|---:|---|
| `ctx.EmitProgress(...)` | 是 | 可选 | 否 | 是 | 展示“正在做什么” |
| `ctx.EmitState(...)` | 是 | 可选 | 间接影响 | 是 | 更新共享状态 |
| `return output, nil` | 是 | 可选 | 是 | 是 | 当前 node 的正式输出 |
| `ctx.EmitOutput(...)` | 是 | 可选 | 是 | 是 | 高级流式/委托输出 |
| `ctx.Ask(...)` | 是 | 可选 | 恢复后影响 | 是 | 人工介入并暂停 |
| `ctx.Trace(...)` | 否 | 是 | 否 | 否 | 链路追踪和排障 |

这里要特别区分两条通道：

- **SSE/Event 通道**：面向产品体验，用户或前端能看到，底层是 `session.Event`。
- **Trace 通道**：面向可观测系统，默认不展示给用户，底层是 OpenTelemetry span event / attributes。

### 推荐的 SSE 事件约定

ADK REST 当前的 SSE 输出是标准格式：

```text
data: <json encoded session.Event>
```

普通事件没有额外的 `event:` 名称；错误事件才会使用 `event: error`。因此 VeADK workflow 不应该另起一套传输协议，而应该在 `session.Event` 内约定 workflow 事件语义。

`EmitProgress` 有一个非常重要的约束：**过程事件不能写 `session.Event.Output`**。在 ADK workflow 调度器里，`Output` 是 node 的正式输出，会驱动后继 node input；如果 progress event 也写 `Output`，就可能污染数据流，甚至触发“一个 node 多个输出”的调度错误。

因此当前实现遵循这个规则：

- 前端可以直接读 `content.parts[].text` 展示文本。
- 直接消费 `runner.Run` 原始事件的程序，可以读 `customMetadata.type` 和 `customMetadata.fields` 做结构化处理。
- 使用 ADK REST SSE 时，现有 REST event model 不会序列化 `CustomMetadata`，所以前端应以 `content.parts[].text` 作为 progress 展示依据。

推荐形态：

```json
{
  "content": {
    "role": "model",
    "parts": [
      {
        "text": "开始自动修复"
      }
    ]
  },
  "customMetadata": {
    "type": "workflow.progress",
    "message": "开始自动修复",
    "fields": {
      "attempt": 1
    }
  },
  "nodeInfo": {
    "path": "repair_until_pass"
  }
}
```

`EmitState` 推荐使用 `actions.stateDelta` 作为权威状态更新。和 progress 一样，state 过程事件也不写 `Output`：

```json
{
  "actions": {
    "stateDelta": {
      "risk_level": "high"
    },
    "artifactDelta": {}
  },
  "customMetadata": {
    "type": "workflow.state",
    "keys": ["risk_level"]
  },
  "nodeInfo": {
    "path": "score_risk"
  }
}
```

`Ask` 继续复用 ADK 原生 HITL wire format，即 `requestedInput` + `longRunningToolIds` + `adk_request_input` function call，不再自定义一套人工介入事件。

```json
{
  "requestedInput": {
    "interruptId": "human_review_789",
    "message": "检测到高风险 PR，请人工确认是否继续。",
    "payload": {
      "riskLevel": "high"
    }
  },
  "longRunningToolIds": ["human_review_789"]
}
```

这层封装解决两个问题：

- 用户不需要手写 `session.NewEvent(ctx, ctx.InvocationID())`。
- 用户不需要理解 `NodeInfo.Path`、`Branch`、`Routes`、`RequestedInput` 这些底层字段。

### 用户如何控制过程事件

用户在 node 函数里调用 `ctx.EmitProgress` 即可。runner 或上层应用如果本来就消费 agent event stream，就能收到这些过程事件。

```go
repairLoop := workflow.DynamicNode(
	"repair_until_pass",
	func(ctx workflow.Context, result TestResult) (PatchResult, error) {
		for attempt := 1; attempt <= 3; attempt++ {
			// 这条消息会作为过程事件发出去，前端可以展示为进度日志。
			if err := ctx.EmitProgress(
				"开始自动修复",
				workflow.Int("attempt", attempt),
			); err != nil {
				return PatchResult{}, err
			}

			// Trace 不一定展示给用户，它更适合进 OpenTelemetry span。
			ctx.Trace("repair.attempt.started", workflow.Int("attempt", attempt))

			patch, err := workflow.RunNode[PatchResult](
				ctx,
				fixerNode,
				result,
				workflow.WithUseSubBranch(),
			)
			if err != nil {
				return PatchResult{}, err
			}

			if err := ctx.EmitProgress("补丁已生成，开始重新跑测试"); err != nil {
				return PatchResult{}, err
			}

			result, err = workflow.RunNode[TestResult](
				ctx,
				testNode,
				patch,
				workflow.WithUseSubBranch(),
			)
			if err != nil {
				return PatchResult{}, err
			}
			if result.Passed {
				ctx.Trace("repair.test.passed", workflow.Int("attempt", attempt))
				return patch, nil
			}
		}

		return PatchResult{}, fmt.Errorf("修复 3 次后测试仍未通过")
	},
)
```

### 接入 span 的建议

ADK 现状已经有自动 span：

- 普通 agent 运行时会有 `invoke_agent` span。
- workflow node 运行时会有 `invoke_node` span。
- `AgentNode` 包住的 agent 会复用 agent 自己的 span，避免重复套一层 node span。

所以 VeADK 不建议让业务用户自己创建 node span。更合适的封装是：

```go
ctx.Trace("module.review.started",
	workflow.String("module", module),
	workflow.String("reviewer", reviewer.Name()),
)
```

内部实现可以把它转换为当前 OpenTelemetry span 的 event 或 attribute。如果用户需要完全自定义，也可以提供高级入口：

```go
span := ctx.Span()
span.SetAttributes(attribute.String("module", module))
```

默认文档只推荐 `ctx.Trace`，把 `ctx.Span()` 放在高级用法里。

下面用同一个真实场景拆分六个常见能力：**PR 自动评审与发布门禁**。

场景背景：

```text
输入：一个 PR URL

目标：
1. 拉取 PR 文件和模块信息
2. 判断是否只改文档
3. 代码变更时并行执行安全、测试、质量评审
4. 根据风险等级决定自动评论、要求补测或人工审批
5. 对受影响模块执行动态评审
6. 必要时暂停 workflow 等待人工输入
```

示例中用到的业务类型：

```go
type PRContext struct {
	URL     string
	Files   []string
	Modules []string
}

type ModuleReviewInput struct {
	PR     PRContext
	Module string
}

type ReviewResult struct {
	Reviewer string
	Summary  string
	Risk     int
}

type RiskDecision struct {
	Level   string // low, medium, high
	Summary string
	Items   []ReviewResult
}

// PRReviewState 是 workflow 级别的共享状态。
// 节点之间的主要数据流仍然通过 input/output 传递；
// State 用于保存全局上下文，避免每个节点都重复携带相同信息。
type PRReviewState struct {
	PRURL       string
	Files       []string
	Modules     []string
	ChangeType  string
	ReviewItems []ReviewResult
	RiskLevel   string
}
```

## 场景一：顺序流程

### 什么时候用

当节点之间是稳定的线性依赖时使用顺序流程。例如：

```text
PR URL -> 拉取 PR 上下文 -> 生成变更摘要
```

### 旧写法：ADK 原生 workflow

```go
collectPR := workflow.NewFunctionNode(
	"collect_pr_context",
	func(ctx agent.Context, prURL string) (PRContext, error) {
		return loadPRContext(prURL), nil
	},
	workflow.NodeConfig{},
)

summarize := workflow.NewFunctionNode(
	"summarize_change",
	func(ctx agent.Context, pr PRContext) (string, error) {
		return summarizeChange(pr), nil
	},
	workflow.NodeConfig{},
)

edges := workflow.Chain(workflow.Start, collectPR, summarize)

rootAgent, err := workflowagent.New(workflowagent.Config{
	Name:        "pr_summary_workflow",
	Description: "拉取 PR 上下文并生成摘要。",
	Edges:       edges,
})
```

旧写法的问题：

- 用户需要知道 `workflow.Start`。
- 用户需要知道 `workflow.Chain` 最终生成的是 `[]workflow.Edge`。
- `workflowagent.New` 和业务节点定义混在一起。

### 新写法：VeADK workflow 封装

```go
wf := workflow.New(
	"pr_summary_workflow",
	workflow.WithDescription("拉取 PR 上下文并生成摘要。"),
)

collectPR := workflow.FuncNode(
	"collect_pr_context",
	func(ctx workflow.Context, prURL string) (PRContext, error) {
		return loadPRContext(prURL), nil
	},
)

summarize := workflow.FuncNode(
	"summarize_change",
	func(ctx workflow.Context, pr PRContext) (string, error) {
		return summarizeChange(pr), nil
	},
)

wf.StartWith(collectPR)
wf.Then(collectPR, summarize)

rootAgent, err := wf.Agent()
```

新写法隐藏：

- `workflow.Start`
- `workflow.Edge`
- `workflowagent.New`
- 空的 `workflow.NodeConfig{}`

封装层内部映射：

```text
workflow.FuncNode       -> adk workflow.NewEmittingFunctionNode
wf.StartWith(collectPR) -> Edge{From: workflow.Start, To: collectPR}
wf.Then(a, b)           -> Edge{From: a, To: b}
wf.Agent()              -> workflowagent.New(...)
```

## 场景二：条件分支

### 什么时候用

当后续流程取决于某个节点的判断结果时使用条件分支。例如：

```text
docs-only PR -> 只生成文档摘要
code PR      -> 进入深度代码评审
```

### 旧写法：手动发出 Event.Routes

```go
classifyChange := workflow.NewEmittingFunctionNode(
	"classify_change",
	func(ctx agent.Context, pr PRContext, emit func(*session.Event) error) (PRContext, error) {
		event := session.NewEvent(ctx, ctx.InvocationID())

		if docsOnly(pr.Files) {
			event.Routes = []string{"docs_only"}
		} else {
			event.Routes = []string{"code_change"}
		}

		if err := emit(event); err != nil {
			return pr, err
		}
		return pr, nil
	},
	workflow.NodeConfig{},
)

docsSummary := workflow.NewFunctionNode("summarize_docs_change", summarizeDocsOnlyChange, workflow.NodeConfig{})
codeReview := workflow.NewFunctionNode("prepare_code_review", prepareCodeReview, workflow.NodeConfig{})

edges := workflow.Concat(
	workflow.Chain(workflow.Start, collectPR, classifyChange),
	[]workflow.Edge{
		{From: classifyChange, To: docsSummary, Route: workflow.StringRoute("docs_only")},
		{From: classifyChange, To: codeReview, Route: workflow.StringRoute("code_change")},
	},
)
```

旧写法的问题：

- 用户需要创建 `session.Event`。
- 用户需要知道 route label 放在 `Event.Routes`。
- 用户需要手动把 route label 和 `workflow.StringRoute` 对齐。

### 新写法：RouteNode + Case

```go
classifyChange := workflow.RouteNode(
	"classify_change",
	func(ctx workflow.Context, pr PRContext) (string, error) {
		if docsOnly(pr.Files) {
			if err := ctx.EmitState(map[string]any{"change_type": "docs_only"}); err != nil {
				return "", err
			}
			return "docs_only", nil
		}

		if err := ctx.EmitState(map[string]any{"change_type": "code_change"}); err != nil {
			return "", err
		}
		return "code_change", nil
	},
)

docsSummary := workflow.FuncNode("summarize_docs_change", summarizeDocsOnlyChange)
codeReview := workflow.FuncNode("prepare_code_review", prepareCodeReview)

wf.Then(collectPR, classifyChange)
wf.Route(
	classifyChange,
	workflow.Case("docs_only", docsSummary),
	workflow.Case("code_change", codeReview),
)
```

封装层内部映射：

```text
RouteNode              -> 生成带 Event.Routes 的 node output event
wf.Route(node, Case()) -> Edge{From: node, To: to, Route: workflow.StringRoute(k)}
```

注意事项：

- `RouteNode` 应该只产生一个 route。
- route label 建议使用稳定字符串，例如 `docs_only`、`code_change`。
- 默认分支可以用 `workflow.Case(nil, fallbackNode)` 表达，内部映射到 `workflow.Default`。

## 场景三：并行合并

### 什么时候用

当多个节点互不依赖，可以同时运行，并且后续节点需要看到全部结果时使用并行合并。例如：

```text
代码 PR -> 安全评审
       -> 测试影响分析
       -> 代码质量评审
       -> 合并结果 -> 计算风险等级
```

### 旧写法：手动 Fan-out、JoinNode、Fan-in

```go
securityNode, err := workflow.NewAgentNode(securityAgent, workflow.NodeConfig{})
if err != nil {
	return nil, err
}

testNode, err := workflow.NewAgentNode(testAgent, workflow.NodeConfig{})
if err != nil {
	return nil, err
}

qualityNode, err := workflow.NewAgentNode(qualityAgent, workflow.NodeConfig{})
if err != nil {
	return nil, err
}

mergeReviews := workflow.NewJoinNode("merge_reviews")

scoreRiskNode := workflow.NewFunctionNode(
	"score_risk",
	func(ctx agent.Context, input map[string]any) (RiskDecision, error) {
		return scoreParallelReviewResults(input), nil
	},
	workflow.NodeConfig{},
)

eb := workflow.NewEdgeBuilder()
eb.AddFanOut(codeReviewStart, securityNode, testNode, qualityNode)
eb.AddFanIn(mergeReviews, securityNode, testNode, qualityNode)
eb.Add(mergeReviews, scoreRiskNode)

rootAgent, err := workflowagent.New(workflowagent.Config{
	Name:        "parallel_pr_review",
	Description: "并行执行 PR 安全、测试、质量评审。",
	Edges:       eb.Build(),

	// 如果这里漏掉 SubAgents，runner 可能无法根据事件 author 找到对应 agent。
	SubAgents: []agent.Agent{securityAgent, testAgent, qualityAgent},
})
```

旧写法的问题：

- 用户需要知道多路无条件 fan-in 必须使用 `JoinNode`。
- 用户需要手动写 `AddFanOut` 和 `AddFanIn`。
- 用户需要知道 `JoinNode` 输出是 `map[string]any`。
- 用户需要手动维护 `SubAgents`。

### 新写法：Parallel + Merge

```go
securityReview, err := workflow.AgentNode(securityAgent)
if err != nil {
	return nil, err
}
testImpact, err := workflow.AgentNode(testAgent)
if err != nil {
	return nil, err
}
qualityReview, err := workflow.AgentNode(qualityAgent)
if err != nil {
	return nil, err
}

scoreRiskNode := workflow.FuncNode(
	"score_risk",
	func(ctx workflow.Context, results []ReviewResult) (RiskDecision, error) {
		return scoreRisk(results), nil
	},
)

mergeReviews := wf.Parallel(
	codeReviewStart,
	securityReview,
	testImpact,
	qualityReview,
).Merge("merge_reviews")

wf.Then(mergeReviews, scoreRiskNode)
```

封装层内部映射：

```text
wf.Parallel(from, a, b, c) -> 多条 fan-out edge
Merge("x")                 -> 内部 JoinNode("x") + fan-in edge
AgentNode                  -> workflow.NewAgentNode + 自动收集 SubAgents
```

设计建议：

- 入门文档使用 `Merge`，不要让用户先理解 `JoinNode`。
- 高级 API 可以保留 `JoinNode` 或 `RawNode`，用于完全控制 ADK 原生行为。
- `Merge` 的默认输出建议转成有类型的 `[]T` 或结构体，而不是直接暴露 `map[string]any`；如果用户需要原始 map，可以显式选择 `MergeMap`。

## 场景四：循环和动态调度

### 什么时候用

当后续调用数量或调用顺序需要在运行时决定时使用 `DynamicNode`。例如：

- 根据 PR 影响的模块数量，动态调用不同模块 reviewer。
- CI 失败后自动修复并重跑测试，最多三轮。
- 根据 LLM planner 输出决定下一步调用哪个 node。

这里以“CI 失败后自动修复并重跑测试”为例：

```text
test_result failed -> fixer agent -> run tests -> 如果仍失败继续修复，最多 3 轮
```

### 旧写法：ADK DynamicNode + RunNode

```go
repairLoop := workflow.NewDynamicNode[TestResult, PatchResult](
	"repair_until_pass",
	func(ctx agent.Context, result TestResult, emit func(*session.Event) error) (PatchResult, error) {
		var patch PatchResult

		for attempt := 0; attempt < 3; attempt++ {
			if result.Passed {
				return patch, nil
			}

			var err error
			patch, err = workflow.RunNode[PatchResult](
				ctx,
				fixerNode,
				result,
				workflow.WithUseSubBranch(),
			)
			if err != nil {
				return patch, err
			}

			result, err = workflow.RunNode[TestResult](
				ctx,
				testNode,
				patch,
				workflow.WithUseSubBranch(),
			)
			if err != nil {
				return patch, err
			}
		}

		return patch, nil
	},
	workflow.NodeConfig{},
)
```

旧写法的问题：

- 业务代码里出现了 `emit func(*session.Event) error`，但大多数循环并不需要 emit。
- 用户需要知道 `workflow.RunNode` 的上下文要求。
- 常用 option 名称偏底层，例如 `workflow.WithUseSubBranch()`。

### 新写法：保留 DynamicNode，简化 Context

```go
repairLoop := workflow.DynamicNode(
	"repair_until_pass",
	func(ctx workflow.Context, result TestResult) (PatchResult, error) {
		var patch PatchResult

		for attempt := 0; attempt < 3; attempt++ {
			if result.Passed {
				return patch, nil
			}

			// 发送过程事件。调用方可以把它展示成“正在第 N 次修复”。
			if err := ctx.EmitProgress(
				"开始自动修复",
				workflow.Int("attempt", attempt+1),
			); err != nil {
				return patch, err
			}

			// 记录到当前 node span，便于排查每次循环的耗时和结果。
			ctx.Trace("repair.attempt.started", workflow.Int("attempt", attempt+1))

			// RunNode 用于在 DynamicNode 内部调用子节点。
			// SubBranch 表示每次子调用使用独立分支，避免 LLM 历史互相污染。
			nextPatch, err := workflow.RunNode[PatchResult](
				ctx,
				fixerNode,
				result,
				workflow.WithUseSubBranch(),
			)
			if err != nil {
				return patch, err
			}
			patch = nextPatch

			nextResult, err := workflow.RunNode[TestResult](
				ctx,
				testNode,
				patch,
				workflow.WithUseSubBranch(),
			)
			if err != nil {
				return patch, err
			}
			result = nextResult

			if result.Passed {
				ctx.Trace("repair.test.passed", workflow.Int("attempt", attempt+1))
				if err := ctx.EmitProgress("自动修复后测试通过"); err != nil {
					return patch, err
				}
			}
		}

		return patch, nil
	},
)
```

另一个动态 fan-out 例子：根据受影响模块动态调用 reviewer。

```go
moduleReview := workflow.DynamicNode(
	"review_changed_modules",
	func(ctx workflow.Context, pr PRContext) ([]ReviewResult, error) {
		results := make([]ReviewResult, 0, len(pr.Modules))

		for _, module := range pr.Modules {
			reviewer, err := workflow.AgentNode(reviewerForModule(module))
			if err != nil {
				return nil, err
			}

			result, err := workflow.RunNode[ReviewResult](
				ctx,
				reviewer,
				ModuleReviewInput{PR: pr, Module: module},
				workflow.WithUseSubBranch(),
			)
			if err != nil {
				return nil, err
			}

			results = append(results, result)
		}

		return results, nil
	},
)
```

封装层内部映射：

```text
DynamicNode          -> workflow.NewDynamicNode
workflow.RunNode          -> adk workflow.RunNode
workflow.WithUseSubBranch -> adk workflow.WithUseSubBranch
```

注意事项：

- `DynamicNode` 适合运行时编排，不建议替代所有静态图。
- 如果流程结构固定，优先使用 `Then`、`RouteNode`、`Parallel`。
- 对 LLM 子节点并发或重复调用时，建议默认启用 sub-branch 隔离。

## 场景五：Agent 节点

### 什么时候用

当某个节点需要由 Agent 完成推理、总结、审查或调用工具时使用 `AgentNode`。例如：

```text
PR review results -> LLM Agent 生成最终 review comment
```

### 旧写法：先创建 VeADK Agent，再手动包装 AgentNode

```go
writerAgent, err := llmagent.New(&llmagent.Config{
	Config: adkllmagent.Config{
		Name:        "review_writer",
		Description: "生成最终 PR review 评论。",
		Instruction: "根据评审结果生成清晰、简洁、可执行的 PR review 评论。",
	},
	DisableThought: true,
})
if err != nil {
	return nil, err
}

writerNode, err := workflow.NewAgentNode(writerAgent, workflow.NodeConfig{})
if err != nil {
	return nil, err
}

rootAgent, err := workflowagent.New(workflowagent.Config{
	Name:  "review_writer_workflow",
	Edges: workflow.Chain(workflow.Start, writerNode),

	// 这一步容易漏。
	SubAgents: []agent.Agent{writerAgent},
})
```

旧写法的问题：

- `workflow.NewAgentNode` 是 ADK 原生 API，和 VeADK agent 创建方式不在一个层次。
- 用户需要知道 `SubAgents` 对 runner 事件解析有影响。
- 空 `workflow.NodeConfig{}` 噪音较多。

### 新写法：AgentNode 自动收集 Agent

```go
writerAgent, err := llmagent.New(&llmagent.Config{
	Config: adkllmagent.Config{
		Name:        "review_writer",
		Description: "生成最终 PR review 评论。",
		Instruction: "根据评审结果生成清晰、简洁、可执行的 PR review 评论。",
	},
	DisableThought: true,
})
if err != nil {
	return nil, err
}

writerNode, err := workflow.AgentNode(writerAgent)
if err != nil {
	return nil, err
}

wf := workflow.New("review_writer_workflow")
wf.StartWith(writerNode)

rootAgent, err := wf.Agent()
```

如果希望创建 `AgentNode` 的体验更接近创建 agent，可以提供 config 版本，把“创建 agent”和“包装成 node”合并为一步：

```go
writerAgent, err := llmagent.New(&llmagent.Config{
	Name:        "review_writer",
	Description: "生成最终 PR review 评论。",
	Instruction: "根据评审结果生成清晰、简洁、可执行的 PR review 评论。",
	Model:       modelName,
	Tools:       []tool.Tool{commentTool},
})
if err != nil {
	return nil, err
}
writerNode, err := workflow.AgentNode(writerAgent)
if err != nil {
	return nil, err
}

wf := workflow.New("review_writer_workflow")
wf.StartWith(writerNode)

rootAgent, err := wf.Agent()
```

也可以保留更贴近 ADK 的入口，适合用户已经创建好了 agent、远程 agent 或复杂自定义 agent 的情况：

```go
writerAgent, err := llmagent.New(writerAgentConfig)
if err != nil {
	return nil, err
}

// AgentNode 表达“把已有 agent 包成 workflow node”。
writerNode, err := workflow.AgentNode(writerAgent)
```

封装层内部映射：

```text
workflow.AgentNode(a) -> workflow.NewAgentNode(a, defaultNodeConfig)
wf.Agent()            -> 自动把 a 加入 workflowagent.Config.SubAgents
```

设计建议：

- `AgentNode` 的名称默认取 agent 的 `Name()`。
- 如果希望“一步创建 LLM agent node”，可以后续补充 `NewAgentNode(AgentNodeConfig)` 便利入口。
- 当前 `AgentNode(agent.Agent)` 适合复用已有 agent、远程 agent 或复杂自定义 agent。
- 对 mid-graph 的 LLM agent，封装层应尽量给出清晰错误提示，说明 chat-mode / single-turn 限制。
- 当一个 Agent 被多个 node 复用时，封装层需要检查命名冲突。

## 场景六：人工介入

### 什么时候用

当 workflow 需要暂停并等待人的输入、审批或补充信息时使用人工介入。这里称为 HITP / HITL。例如：

```text
高风险 PR -> 暂停 workflow -> 请求 owner 批准或要求修改 -> 收到回复后恢复
```

### 旧写法：RequestInputEvent + ErrNodeInterrupted

```go
requestHumanReview := workflow.NewEmittingFunctionNode(
	"request_human_review",
	func(ctx agent.Context, decision RiskDecision, emit func(*session.Event) error) (RiskDecision, error) {
		err := emit(workflow.NewRequestInputEvent(ctx, session.RequestInput{
			Message: "检测到高风险 PR，请人工确认：批准、拒绝或要求修改。",
			Payload: decision,
		}))
		if err != nil {
			return decision, err
		}

		// 节点返回 ErrNodeInterrupted 后，workflow 会暂停并等待后续恢复。
		return decision, workflow.ErrNodeInterrupted
	},
	workflow.NodeConfig{},
)
```

旧写法的问题：

- 用户需要知道 `NewRequestInputEvent`。
- 用户需要知道 `ErrNodeInterrupted`。
- 用户需要理解 interrupt id 和后续 function response 如何对应。
- 简单“问人一句话”的场景写法太重。

### 新写法：AskNode

```go
requestHumanReview := workflow.AskNode(
	"request_human_review",
	func(ctx workflow.Context, decision RiskDecision) (session.RequestInput, error) {
		return session.RequestInput{
			Message: "检测到高风险 PR，请人工确认：批准、拒绝或要求修改。",
			Payload: decision,
		}, nil
	},
	func(ctx workflow.Context, decision RiskDecision, reply any) (HumanReviewReply, error) {
		typed, ok := reply.(HumanReviewReply)
		if !ok {
			return HumanReviewReply{}, fmt.Errorf("invalid human review reply: %T", reply)
		}
		return typed, nil
	},
)

wf.Route(scoreRisk, workflow.Case("high", requestHumanReview))
```

如果人工回复后还需要继续走后续节点，可以把 `AskNode` 当作普通 node 连接：

```go
applyHumanDecision := workflow.FuncNode(
	"apply_human_decision",
	func(ctx workflow.Context, reply HumanReviewReply) (string, error) {
		if reply.Approved {
			return "approved", nil
		}
		return "changes_requested", nil
	},
)

wf.Then(requestHumanReview, applyHumanDecision)
```

封装层内部映射：

```text
AskNode -> NewEmittingFunctionNode
        -> workflow.ResumeOrRequestInput
        -> workflow.NewRequestInputEvent
        -> workflow.ErrNodeInterrupted
        -> resume 时把人工回复作为 node output 传给后继节点
```

注意事项：

- `AskNode` 的 payload 必须可序列化，因为 workflow 暂停和恢复依赖 session 事件。
- 如果需要校验人工回复，可以在 `session.RequestInput.ResponseSchema` 中提供 response schema。
- 如果一个节点需要多轮人工输入，建议使用 `DynamicNode` 组合 `ctx.Ask(...)`。

## API 分层建议

这份设计不是要做三套互相重复的封装，而是建议提供一个内部编译层和多个入口深度：

```text
FuncNode / AgentNode / RouteNode / Parallel / DynamicNode / AskNode
        -> 内部 FlowSpec
        -> ADK workflow.Node / workflow.Edge
        -> workflowagent.New
```

不同入口只负责生成同一个内部 `FlowSpec`：

- 非链式 Builder：推荐主文档使用，适合复杂 workflow。
- 链式 Builder：适合短 demo 和简单线性流程。
- Raw escape hatch：允许高级用户直接传入 ADK 原生 `workflow.Node` / `workflow.Edge`。

推荐主写法：

```go
wf := workflow.New("pr_review_workflow")

collect := workflow.FuncNode("collect_pr_context", collectPR)
classify := workflow.RouteNode("classify_change", classifyChange)
review := workflow.DynamicNode("review_changed_modules", reviewChangedModules)

wf.StartWith(collect)
wf.Then(collect, classify)
wf.Route(classify, workflow.Case("code_change", review))

rootAgent, err := wf.Agent()
```

简单场景后续可以再补链式糖，但第一版先推荐上面的非链式 Builder。原因是复杂 workflow 更常见的是先定义一批 node，再集中声明边和分支：

- node 定义和图连接分开，便于阅读和复用。
- 并行、条件、动态调度混用时，不会出现很长的链式调用。
- 和 ADK 原生 `Edge` 图模型更接近，排查问题时容易对齐到底层行为。
