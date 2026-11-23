# PR 发布门禁 Workflow 示例

这个目录用同一个真实场景对比两种 workflow 写法：

- `main.go`：VeADK workflow 封装后的推荐写法，默认运行。
- `legacy_adk.go`：ADK 原生 workflow 写法，对比用，可通过环境变量运行。
- `VEADK_WORKFLOW_EXAMPLE_MODE=llm`：复用新封装写法，但质量评审节点会请求真实模型。

运行示例：

```bash
go run ./examples/workflow/pr_release_gate
```

运行 ADK 原生 workflow 对照版本：

```bash
VEADK_WORKFLOW_EXAMPLE_MODE=legacy go run ./examples/workflow/pr_release_gate
```

运行真实模型版本：

```bash
VEADK_WORKFLOW_EXAMPLE_MODE=llm go run ./examples/workflow/pr_release_gate
```

真实模型版本沿用 VeADK Go 的模型配置。可以使用 `config.yaml`，也可以设置环境变量：

```bash
export MODEL_AGENT_PROVIDER=openai
export MODEL_AGENT_NAME="<your_model_name>"
export MODEL_AGENT_API_BASE="<your_model_base_url>"
export MODEL_AGENT_API_KEY="<your_model_api_key>"
```

如果使用 Ark 默认鉴权，也可以通过火山 AK/SK 生成模型 token；不要把密钥写入示例代码。

## 场景

这个 workflow 模拟一次 PR 发布门禁：

1. 拉取 PR 上下文。
2. 判断 PR 是文档变更还是代码变更。
3. 文档变更直接生成摘要。
4. 代码变更并行执行安全检查、测试影响分析、质量 Agent 评审和动态模块评审。
5. 汇总检查结果并计算风险。
6. 高风险 PR 暂停 workflow，等待负责人审批。
7. 收到审批回复后继续输出发布结果。

## 新写法

`main.go` 里开发者主要面对业务 node。默认版本中的质量评审 Agent 是本地确定性 agent，便于无凭证运行；`llm` 模式会把质量评审替换为真实 LLM agent。

```go
collectPR := workflow.FuncNode("collect_pr_context", func(ctx workflow.Context, input string) (PRContext, error) {
    ctx.EmitProgress("拉取 PR 上下文", workflow.String("url", input))
    return PRContext{URL: input}, nil
})
```

workflow 结构用 `StartWith`、`Then`、`Route`、`Merge` 描述：

```go
wf.StartWith(collectPR)
wf.Then(collectPR, classify)
wf.Route(classify,
    workflow.Case("docs_only", docsSummary),
    workflow.Case("code_change", securityReview),
)
mergedReviews := wf.Merge("merge_reviews", securityReview, testImpact, qualityReview, dynamicModuleReview)
```

## 旧写法

`legacy_adk.go` 里开发者要直接使用 ADK workflow 原生概念：

- `workflow.NewFunctionNode`
- `workflow.NewEmittingFunctionNode`
- `workflow.NewDynamicNode`
- `workflow.RunNode`
- `workflow.NewJoinNode`
- `workflow.Edge`
- `workflow.StringRoute`
- `workflow.NewRequestInputEvent` / `workflow.ResumeOrRequestInput`
- `workflowagent.Config.SubAgents`

例如条件分支需要手动构造带 `Routes` 的事件：

```go
ev := session.NewEvent(ctx, ctx.InvocationID())
ev.Output = pr
ev.Routes = []string{"code_change"}
return ev, nil
```

并行和合并也要手动写成边：

```go
Edges: []workflow.Edge{
    {From: classify, To: securityReview, Route: workflow.StringRoute("code_change")},
    {From: classify, To: testImpact, Route: workflow.StringRoute("code_change")},
    {From: securityReview, To: mergeReviews},
    {From: testImpact, To: mergeReviews},
}
```

## 主要差别

新写法把底层 `Event`、`Route`、`Edge`、`SubAgents` 和 HITP 暂停恢复细节收敛到封装里，开发者主要写“节点做什么”和“节点怎么连”。

旧写法更贴近 ADK 的执行模型，适合需要完全控制底层事件和图结构的高级场景，但普通业务 workflow 会暴露更多机制细节。

真实模型版本进一步验证 `AgentNode` 可以包装普通 VeADK LLM Agent：

```go
qualityAgent, err := veagent.New(&veagent.Config{
    Config: llmagent.Config{
        Name:        "llm_quality_agent",
        Description: "使用真实模型评估代码质量风险。",
        Instruction: "请只输出一行中文质量评审结论。",
    },
})
qualityNode, err := workflow.AgentNodeTyped[PRContext, string](qualityAgent)
```

这样 workflow 仍然使用同一套 `StartWith` / `Route` / `Merge` 编排方式，只是其中一个 node 的输出来自真实模型。
