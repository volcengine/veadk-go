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

package workflow_test

import (
	"context"
	"iter"
	"testing"

	"github.com/volcengine/veadk-go/v2/runner"
	vewf "github.com/volcengine/veadk-go/v2/workflow"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func runWorkflow(t *testing.T, root agent.Agent, text string) []*session.Event {
	t.Helper()
	r, err := runner.New(runner.Config{
		AppName:           "workflow_test",
		Agent:             root,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
	})
	if err != nil {
		t.Fatalf("runner.New() error = %v", err)
	}

	var events []*session.Event
	for ev, err := range r.Run(context.Background(), "user", "session", runner.UserText(text), agent.RunConfig{}) {
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		events = append(events, ev)
	}
	return events
}

func TestFuncNodeEmitsProgressWithoutUsingOutput(t *testing.T) {
	classify := vewf.FuncNode[string, string]("classify", func(ctx vewf.Context, input string) (string, error) {
		if err := ctx.EmitProgress("开始识别变更类型", vewf.String("pr", input)); err != nil {
			return "", err
		}
		return "code_change", nil
	})
	comment := vewf.FuncNode[string, string]("comment", func(ctx vewf.Context, input string) (string, error) {
		return "result:" + input, nil
	})

	graph := vewf.New("progress_case")
	graph.StartWith(classify)
	graph.Then(classify, comment)
	root, err := graph.Agent()
	if err != nil {
		t.Fatalf("Agent() error = %v", err)
	}

	events := runWorkflow(t, root, "https://example.test/pr/1")
	if len(events) < 2 {
		t.Fatalf("events = %d, want at least 2", len(events))
	}
	progress := events[0]
	if progress.Output != nil {
		t.Fatalf("progress Output = %#v, want nil", progress.Output)
	}
	if progress.Content == nil || progress.Content.Parts[0].Text != "开始识别变更类型" {
		t.Fatalf("progress content = %#v", progress.Content)
	}
	if progress.CustomMetadata["type"] != vewf.EventTypeProgress {
		t.Fatalf("progress metadata = %#v", progress.CustomMetadata)
	}
	if got := events[len(events)-1].Output; got != "result:code_change" {
		t.Fatalf("final output = %#v, want result:code_change", got)
	}
}

func TestRouteNodeSelectsCase(t *testing.T) {
	classify := vewf.RouteNode[string]("classify", func(ctx vewf.Context, input string) (string, error) {
		if input == "docs" {
			return "docs_only", nil
		}
		return "code_change", nil
	})
	docs := vewf.FuncNode[string, string]("docs_comment", func(ctx vewf.Context, input string) (string, error) {
		return "docs:" + input, nil
	})
	code := vewf.FuncNode[string, string]("code_review", func(ctx vewf.Context, input string) (string, error) {
		return "code:" + input, nil
	})

	graph := vewf.New("route_case")
	graph.StartWith(classify)
	graph.Route(classify, vewf.Case("docs_only", docs), vewf.Case("code_change", code))
	root, err := graph.Agent()
	if err != nil {
		t.Fatalf("Agent() error = %v", err)
	}

	events := runWorkflow(t, root, "docs")
	if got := events[len(events)-1].Output; got != "docs:docs" {
		t.Fatalf("final output = %#v, want docs:docs", got)
	}
}

func TestDynamicNodeRunsChildNode(t *testing.T) {
	review := vewf.FuncNode[string, string]("review_module", func(ctx vewf.Context, input string) (string, error) {
		return "reviewed:" + input, nil
	})
	dynamic := vewf.DynamicNode[string, string]("review_changed_modules", func(ctx vewf.Context, input string) (string, error) {
		if err := ctx.EmitProgress("开始动态评审模块", vewf.String("module", input)); err != nil {
			return "", err
		}
		return vewf.RunNode[string](ctx, review, input, vewf.WithUseSubBranch())
	})

	graph := vewf.New("dynamic_case")
	graph.StartWith(dynamic)
	root, err := graph.Agent()
	if err != nil {
		t.Fatalf("Agent() error = %v", err)
	}

	events := runWorkflow(t, root, "payment")
	if got := events[len(events)-1].Output; got != "reviewed:payment" {
		t.Fatalf("final output = %#v, want reviewed:payment", got)
	}
}

func TestAskNodeEmitsRequestedInput(t *testing.T) {
	approval := vewf.AskNode[string, string](
		"human_approval",
		func(ctx vewf.Context, input string) (session.RequestInput, error) {
			return session.RequestInput{
				InterruptID: "approve-1",
				Message:     "是否允许发布？",
				Payload:     map[string]any{"risk": input},
			}, nil
		},
		func(ctx vewf.Context, input string, reply any) (string, error) {
			return "approved", nil
		},
	)

	graph := vewf.New("hitl_case")
	graph.StartWith(approval)
	root, err := graph.Agent()
	if err != nil {
		t.Fatalf("Agent() error = %v", err)
	}

	events := runWorkflow(t, root, "high")
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].RequestedInput == nil {
		t.Fatal("RequestedInput is nil")
	}
	if events[0].RequestedInput.Message != "是否允许发布？" {
		t.Fatalf("RequestedInput.Message = %q", events[0].RequestedInput.Message)
	}
}

func TestBuilderCollectsAgentNodeSubAgents(t *testing.T) {
	sub, err := agent.New(agent.Config{
		Name: "review_agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Content = genai.NewContentFromText("reviewed", genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}
	node, err := vewf.AgentNode(sub)
	if err != nil {
		t.Fatalf("AgentNode() error = %v", err)
	}

	graph := vewf.New("agent_case")
	graph.StartWith(node)
	root, err := graph.Agent()
	if err != nil {
		t.Fatalf("Agent() error = %v", err)
	}
	if got := len(root.SubAgents()); got != 1 {
		t.Fatalf("SubAgents len = %d, want 1", got)
	}
}

func TestAgentNodeNormalizesStructOutput(t *testing.T) {
	type ReviewResult struct {
		Name string `json:"name"`
		Risk int    `json:"risk"`
	}
	sub, err := agent.New(agent.Config{
		Name: "struct_output_agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ctx, ctx.InvocationID())
				ev.Author = "struct_output_agent"
				ev.Output = ReviewResult{Name: "quality", Risk: 6}
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}
	node, err := vewf.AgentNode(sub)
	if err != nil {
		t.Fatalf("AgentNode() error = %v", err)
	}
	consume := vewf.FuncNode[map[string]any, string]("consume", func(ctx vewf.Context, input map[string]any) (string, error) {
		return input["name"].(string), nil
	})

	graph := vewf.New("agent_struct_output_case")
	graph.StartWith(node)
	graph.Then(node, consume)
	root, err := graph.Agent()
	if err != nil {
		t.Fatalf("Agent() error = %v", err)
	}
	events := runWorkflow(t, root, "run")
	if got := events[len(events)-1].Output; got != "quality" {
		t.Fatalf("final output = %#v, want quality", got)
	}
}
