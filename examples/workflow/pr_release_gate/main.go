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
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"strings"

	verunner "github.com/volcengine/veadk-go/v2/runner"
	veworkflow "github.com/volcengine/veadk-go/v2/workflow"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	adkworkflow "google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

type PRContext struct {
	URL     string
	Kind    string
	Modules []string
}

type ReviewResult struct {
	Name string `json:"name"`
	Risk int    `json:"risk"`
}

type RiskDecision struct {
	Level   string
	Summary string
}

type HumanReply struct {
	Approved bool `json:"approved"`
}

func main() {
	ctx := context.Background()

	newWorkflow := newPRReleaseGate
	if os.Getenv("VEADK_WORKFLOW_EXAMPLE_MODE") == "legacy" {
		newWorkflow = newLegacyPRReleaseGate
	}

	rootAgent, err := newWorkflow()
	if err != nil {
		panic(err)
	}
	r, err := verunner.New(verunner.Config{
		AppName:           "workflow_pr_release_gate",
		Agent:             rootAgent,
		AutoCreateSession: true,
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("== code PR: run until human approval ==")
	for event, err := range r.Run(ctx, "user", "code-pr-session", verunner.UserText("https://example.test/pr/42 code"), agent.RunConfig{}) {
		if err != nil {
			panic(err)
		}
		printEvent(event)
	}

	fmt.Println("== code PR: resume with approval ==")
	approval := workflowInputResponse("approval-42", HumanReply{Approved: true})
	for event, err := range r.Run(ctx, "user", "code-pr-session", approval, agent.RunConfig{}) {
		if err != nil {
			panic(err)
		}
		printEvent(event)
	}

	fmt.Println("== docs PR: route to docs summary ==")
	for event, err := range r.Run(ctx, "user", "docs-pr-session", verunner.UserText("https://example.test/pr/43 docs"), agent.RunConfig{}) {
		if err != nil {
			panic(err)
		}
		printEvent(event)
	}
}

func newPRReleaseGate() (agent.Agent, error) {
	collectPR := veworkflow.FuncNode[string, PRContext](
		"collect_pr_context",
		func(ctx veworkflow.Context, input string) (PRContext, error) {
			// 中文注释：过程事件不会成为下游 node input，只用于 UI 展示进度。
			if err := ctx.EmitProgress("拉取 PR 上下文", veworkflow.String("url", input)); err != nil {
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
	)

	classify := veworkflow.RouteNode[PRContext](
		"classify_change",
		func(ctx veworkflow.Context, pr PRContext) (string, error) {
			if pr.Kind == "docs" {
				return "docs_only", ctx.EmitState(map[string]any{"change_type": "docs_only"})
			}
			return "code_change", ctx.EmitState(map[string]any{"change_type": "code_change"})
		},
	)

	docsSummary := veworkflow.FuncNode[PRContext, string](
		"summarize_docs",
		func(ctx veworkflow.Context, pr PRContext) (string, error) {
			return "docs_summary:" + pr.URL, nil
		},
	)

	securityReview := veworkflow.FuncNode[PRContext, ReviewResult](
		"security_review",
		func(ctx veworkflow.Context, pr PRContext) (ReviewResult, error) {
			return ReviewResult{Name: "security", Risk: 8}, nil
		},
	)
	testImpact := veworkflow.FuncNode[PRContext, ReviewResult](
		"test_impact",
		func(ctx veworkflow.Context, pr PRContext) (ReviewResult, error) {
			return ReviewResult{Name: "test", Risk: 4}, nil
		},
	)
	qualityReview, err := qualityAgentNode()
	if err != nil {
		return nil, err
	}

	moduleReviewer := veworkflow.FuncNode[string, ReviewResult](
		"review_module",
		func(ctx veworkflow.Context, module string) (ReviewResult, error) {
			return ReviewResult{Name: "module:" + module, Risk: 7}, nil
		},
	)
	dynamicModuleReview := veworkflow.DynamicNode[PRContext, []ReviewResult](
		"review_changed_modules",
		func(ctx veworkflow.Context, pr PRContext) ([]ReviewResult, error) {
			results := make([]ReviewResult, 0, len(pr.Modules))
			for _, module := range pr.Modules {
				if err := ctx.EmitProgress("动态评审模块 " + module); err != nil {
					return nil, err
				}
				result, err := veworkflow.RunNode[ReviewResult](
					ctx,
					moduleReviewer,
					module,
					veworkflow.WithUseSubBranch(),
				)
				if err != nil {
					return nil, err
				}
				results = append(results, result)
			}
			return results, nil
		},
	)

	scoreRisk := veworkflow.FuncNode[map[string]any, RiskDecision](
		"score_risk",
		func(ctx veworkflow.Context, merged map[string]any) (RiskDecision, error) {
			if err := ctx.EmitProgress("计算综合风险", veworkflow.Int("inputs", len(merged))); err != nil {
				return RiskDecision{}, err
			}
			return RiskDecision{
				Level:   "high",
				Summary: "安全风险较高，需要人工确认",
			}, nil
		},
	)

	routeRisk := veworkflow.RouteNode[RiskDecision](
		"route_risk",
		func(ctx veworkflow.Context, decision RiskDecision) (string, error) {
			return decision.Level, nil
		},
	)

	askOwner := veworkflow.AskNode[RiskDecision, string](
		"request_owner_approval",
		func(ctx veworkflow.Context, decision RiskDecision) (session.RequestInput, error) {
			return session.RequestInput{
				InterruptID: "approval-42",
				Message:     "高风险 PR 是否允许发布？",
				Payload:     decision,
			}, nil
		},
		func(ctx veworkflow.Context, decision RiskDecision, reply any) (string, error) {
			approved, err := decodeApproval(reply)
			if err != nil {
				return "", err
			}
			if approved {
				return "release_approved", nil
			}
			return "release_blocked", nil
		},
	)

	wf := veworkflow.New(
		"pr_release_gate",
		veworkflow.WithDescription("PR 自动评审与发布门禁"),
	)
	wf.StartWith(collectPR)
	wf.Then(collectPR, classify)
	wf.Route(
		classify,
		veworkflow.Case("docs_only", docsSummary),
		veworkflow.Case("code_change", securityReview),
		veworkflow.Case("code_change", testImpact),
		veworkflow.Case("code_change", qualityReview),
		veworkflow.Case("code_change", dynamicModuleReview),
	)
	mergedReviews := wf.Merge("merge_reviews", securityReview, testImpact, qualityReview, dynamicModuleReview)
	wf.Then(mergedReviews, scoreRisk)
	wf.Then(scoreRisk, routeRisk)
	wf.Route(routeRisk, veworkflow.Case("high", askOwner))

	return wf.Agent()
}

func qualityAgentNode() (veworkflow.Node, error) {
	qualityAgent, err := agent.New(agent.Config{
		Name:        "quality_agent",
		Description: "评估代码质量风险。",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				event := session.NewEvent(ctx, ctx.InvocationID())
				event.Author = "quality_agent"
				event.Output = ReviewResult{Name: "quality", Risk: 6}
				event.LLMResponse = model.LLMResponse{
					Content: genai.NewContentFromText("quality risk checked", genai.RoleModel),
				}
				yield(event, nil)
			}
		},
	})
	if err != nil {
		return nil, err
	}
	return veworkflow.AgentNode(qualityAgent)
}

func workflowInputResponse(interruptID string, reply HumanReply) *genai.Content {
	return &genai.Content{
		Role: genai.RoleUser,
		Parts: []*genai.Part{{
			FunctionResponse: &genai.FunctionResponse{
				ID:   interruptID,
				Name: adkworkflow.WorkflowInputFunctionCallName,
				Response: map[string]any{
					"payload": map[string]any{"approved": reply.Approved},
				},
			},
		}},
	}
}

func decodeApproval(reply any) (bool, error) {
	data, err := json.Marshal(reply)
	if err != nil {
		return false, err
	}
	var out HumanReply
	if err := json.Unmarshal(data, &out); err != nil {
		return false, err
	}
	return out.Approved, nil
}

func printEvent(event *session.Event) {
	switch {
	case event == nil:
		return
	case event.RequestedInput != nil:
		fmt.Printf("ASK      node=%s id=%s message=%s\n",
			nodePath(event),
			event.RequestedInput.InterruptID,
			event.RequestedInput.Message,
		)
	case len(event.Actions.StateDelta) > 0:
		fmt.Printf("STATE    node=%s delta=%v\n", nodePath(event), event.Actions.StateDelta)
	case event.Content != nil && event.Output == nil:
		fmt.Printf("PROGRESS node=%s text=%q\n", nodePath(event), visibleText(event.Content))
	case event.Output != nil:
		fmt.Printf("OUTPUT   node=%s value=%v\n", nodePath(event), event.Output)
	}
}

func nodePath(event *session.Event) string {
	if event.NodeInfo == nil || event.NodeInfo.Path == "" {
		return "-"
	}
	return event.NodeInfo.Path
}

func visibleText(content *genai.Content) string {
	var text strings.Builder
	for _, part := range content.Parts {
		if part != nil && !part.Thought {
			text.WriteString(part.Text)
		}
	}
	return text.String()
}
