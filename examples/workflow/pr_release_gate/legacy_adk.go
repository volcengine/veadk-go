// Copyright (c) 2025 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"iter"
	"strings"

	"google.golang.org/adk/v2/agent"
	workflowagent "google.golang.org/adk/v2/agent/workflowagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	adkworkflow "google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

// newLegacyPRReleaseGate 展示同一个 PR 发布门禁场景在 ADK 原生 workflow 里的写法。
//
// 中文注释：这个函数不会被 main 默认调用，它用于和 main.go 里的 VeADK workflow
// 封装写法做对照。保留在同一个 package 中可以让它参与编译检查，避免示例代码漂移。
func newLegacyPRReleaseGate() (agent.Agent, error) {
	collectPR := adkworkflow.NewEmittingFunctionNode[string, PRContext](
		"collect_pr_context",
		func(ctx agent.Context, input string, emit func(*session.Event) error) (PRContext, error) {
			// 中文注释：旧写法需要开发者自己构造过程事件，并保证不要写 Event.Output。
			if err := legacyEmitProgress(ctx, emit, "拉取 PR 上下文", map[string]any{"url": input}); err != nil {
				return PRContext{}, err
			}

			kind := "code"
			if strings.Contains(input, "docs") {
				kind = "docs"
			}
			return PRContext{
				URL:     input,
				Kind:    kind,
				Modules: []string{"payment", "checkout"},
			}, nil
		},
		adkworkflow.NodeConfig{},
	)

	classify := adkworkflow.NewFunctionNode[PRContext, *session.Event](
		"classify_change",
		func(ctx agent.Context, pr PRContext) (*session.Event, error) {
			route := "code_change"
			if pr.Kind == "docs" {
				route = "docs_only"
			}

			// 中文注释：旧写法里，条件分支需要手动在 Event.Routes 上写路由值。
			// 同时 Output 必须继续携带业务输入，否则下游节点拿不到 PRContext。
			ev := session.NewEvent(ctx, ctx.InvocationID())
			ev.Author = ctx.AgentName()
			ev.Branch = ctx.Branch()
			ev.IsolationScope = ctx.IsolationScope()
			ev.NodeInfo = &session.NodeInfo{Path: ctx.Path()}
			ev.Output = pr
			ev.Routes = []string{route}
			ev.Actions.StateDelta = map[string]any{"change_type": route}
			if err := ctx.State().Set("change_type", route); err != nil {
				return nil, err
			}
			return ev, nil
		},
		adkworkflow.NodeConfig{},
	)

	docsSummary := adkworkflow.NewFunctionNode[PRContext, string](
		"summarize_docs",
		func(ctx agent.Context, pr PRContext) (string, error) {
			return "docs_summary:" + pr.URL, nil
		},
		adkworkflow.NodeConfig{},
	)

	securityReview := adkworkflow.NewFunctionNode[PRContext, ReviewResult](
		"security_review",
		func(ctx agent.Context, pr PRContext) (ReviewResult, error) {
			return ReviewResult{Name: "security", Risk: 8}, nil
		},
		adkworkflow.NodeConfig{},
	)
	testImpact := adkworkflow.NewFunctionNode[PRContext, ReviewResult](
		"test_impact",
		func(ctx agent.Context, pr PRContext) (ReviewResult, error) {
			return ReviewResult{Name: "test", Risk: 4}, nil
		},
		adkworkflow.NodeConfig{},
	)

	qualityAgent, err := legacyQualityAgent()
	if err != nil {
		return nil, err
	}
	// 中文注释：ADK 原生 AgentNode 的 schema 校验不接受直接校验 Go struct，
	// 因此旧写法里需要让 Agent 输出 JSON-like map。VeADK 封装会自动处理这层转换。
	qualityReview, err := adkworkflow.NewAgentNodeTyped[PRContext, map[string]any](
		qualityAgent,
		adkworkflow.NodeConfig{},
	)
	if err != nil {
		return nil, err
	}

	moduleReviewer := adkworkflow.NewFunctionNode[string, ReviewResult](
		"review_module",
		func(ctx agent.Context, module string) (ReviewResult, error) {
			return ReviewResult{Name: "module:" + module, Risk: 7}, nil
		},
		adkworkflow.NodeConfig{},
	)
	dynamicModuleReview := adkworkflow.NewDynamicNode[PRContext, []ReviewResult](
		"review_changed_modules",
		func(ctx agent.Context, pr PRContext, emit func(*session.Event) error) ([]ReviewResult, error) {
			results := make([]ReviewResult, 0, len(pr.Modules))
			for _, module := range pr.Modules {
				// 中文注释：DynamicNode 中也需要手动传入 emit，自己维护过程事件。
				if err := legacyEmitProgress(ctx, emit, "动态评审模块 "+module, nil); err != nil {
					return nil, err
				}
				result, err := adkworkflow.RunNode[ReviewResult](
					ctx,
					moduleReviewer,
					module,
					adkworkflow.WithUseSubBranch(),
				)
				if err != nil {
					return nil, err
				}
				results = append(results, result)
			}
			return results, nil
		},
		adkworkflow.NodeConfig{},
	)

	scoreRisk := adkworkflow.NewEmittingFunctionNode[map[string]any, RiskDecision](
		"score_risk",
		func(ctx agent.Context, merged map[string]any, emit func(*session.Event) error) (RiskDecision, error) {
			if err := legacyEmitProgress(ctx, emit, "计算综合风险", map[string]any{"inputs": len(merged)}); err != nil {
				return RiskDecision{}, err
			}
			return RiskDecision{
				Level:   "high",
				Summary: "安全风险较高，需要人工确认",
			}, nil
		},
		adkworkflow.NodeConfig{},
	)

	routeRisk := adkworkflow.NewFunctionNode[RiskDecision, *session.Event](
		"route_risk",
		func(ctx agent.Context, decision RiskDecision) (*session.Event, error) {
			ev := session.NewEvent(ctx, ctx.InvocationID())
			ev.Author = ctx.AgentName()
			ev.Branch = ctx.Branch()
			ev.IsolationScope = ctx.IsolationScope()
			ev.NodeInfo = &session.NodeInfo{Path: ctx.Path()}
			ev.Output = decision
			ev.Routes = []string{decision.Level}
			return ev, nil
		},
		adkworkflow.NodeConfig{},
	)

	askOwner := adkworkflow.NewEmittingFunctionNode[RiskDecision, string](
		"request_owner_approval",
		func(ctx agent.Context, decision RiskDecision, emit func(*session.Event) error) (string, error) {
			// 中文注释：旧写法需要直接使用 ResumeOrRequestInput，并理解
			// ErrNodeInterrupted 会让 workflow 暂停等待人工输入。
			reply, err := adkworkflow.ResumeOrRequestInput(ctx, emit, session.RequestInput{
				InterruptID: "approval-42",
				Message:     "高风险 PR 是否允许发布？",
				Payload:     decision,
			})
			if err != nil {
				return "", err
			}

			approved, err := decodeApproval(reply)
			if err != nil {
				return "", err
			}
			if approved {
				return "release_approved", nil
			}
			return "release_blocked", nil
		},
		adkworkflow.NodeConfig{RerunOnResume: boolPtr(true)},
	)

	mergeReviews := adkworkflow.NewJoinNode("merge_reviews")

	// 中文注释：旧写法的 workflow 结构主要体现为手动维护 Edge 列表。
	// 顺序、分支、并行、合并都要显式写成 From/To/Route。
	return workflowagent.New(workflowagent.Config{
		Name:        "pr_release_gate_legacy",
		Description: "PR 自动评审与发布门禁，ADK 原生 workflow 写法。",
		Edges: []adkworkflow.Edge{
			{From: adkworkflow.Start, To: collectPR},
			{From: collectPR, To: classify},

			{From: classify, To: docsSummary, Route: adkworkflow.StringRoute("docs_only")},
			{From: classify, To: securityReview, Route: adkworkflow.StringRoute("code_change")},
			{From: classify, To: testImpact, Route: adkworkflow.StringRoute("code_change")},
			{From: classify, To: qualityReview, Route: adkworkflow.StringRoute("code_change")},
			{From: classify, To: dynamicModuleReview, Route: adkworkflow.StringRoute("code_change")},

			{From: securityReview, To: mergeReviews},
			{From: testImpact, To: mergeReviews},
			{From: qualityReview, To: mergeReviews},
			{From: dynamicModuleReview, To: mergeReviews},

			{From: mergeReviews, To: scoreRisk},
			{From: scoreRisk, To: routeRisk},
			{From: routeRisk, To: askOwner, Route: adkworkflow.StringRoute("high")},
		},
		SubAgents: []agent.Agent{
			qualityAgent,
		},
	})
}

func legacyQualityAgent() (agent.Agent, error) {
	return agent.New(agent.Config{
		Name:        "legacy_quality_agent",
		Description: "评估代码质量风险。",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				event := session.NewEvent(ctx, ctx.InvocationID())
				event.Author = "legacy_quality_agent"
				event.Output = map[string]any{"name": "quality", "risk": 6}
				event.LLMResponse = model.LLMResponse{
					Content: genai.NewContentFromText("quality risk checked", genai.RoleModel),
				}
				yield(event, nil)
			}
		},
	})
}

func legacyEmitProgress(
	ctx agent.Context,
	emit func(*session.Event) error,
	message string,
	fields map[string]any,
) error {
	if emit == nil {
		return fmt.Errorf("legacy workflow progress events are unavailable")
	}
	ev := session.NewEvent(ctx, ctx.InvocationID())
	ev.Author = ctx.AgentName()
	ev.Branch = ctx.Branch()
	ev.IsolationScope = ctx.IsolationScope()
	ev.NodeInfo = &session.NodeInfo{Path: ctx.Path()}
	ev.LLMResponse = model.LLMResponse{
		Content: genai.NewContentFromText(message, genai.RoleModel),
		CustomMetadata: map[string]any{
			"type":    "workflow.progress",
			"message": message,
			"fields":  fields,
		},
	}
	return emit(ev)
}

func boolPtr(value bool) *bool {
	return &value
}
