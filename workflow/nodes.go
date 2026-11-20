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

package workflow

import (
	"encoding/json"
	"fmt"
	"iter"
	"reflect"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	adkwf "google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

type Func[IN, OUT any] func(Context, IN) (OUT, error)
type DynamicFunc[IN, OUT any] func(Context, IN) (OUT, error)
type RouteFunc[IN any] func(Context, IN) (string, error)

func FuncNode[IN, OUT any](name string, fn Func[IN, OUT], opts ...NodeOption) Node {
	resolved := resolveNodeOptions(opts)
	return adkwf.NewEmittingFunctionNode[IN, OUT](
		name,
		func(ctx agent.Context, input IN, emit func(*session.Event) error) (OUT, error) {
			return fn(newContext(ctx, emit), input)
		},
		resolved.cfg,
	)
}

func DynamicNode[IN, OUT any](name string, fn DynamicFunc[IN, OUT], opts ...NodeOption) Node {
	resolved := resolveNodeOptions(opts)
	return adkwf.NewDynamicNode[IN, OUT](
		name,
		func(ctx agent.Context, input IN, emit func(*session.Event) error) (OUT, error) {
			return fn(newContext(ctx, emit), input)
		},
		resolved.cfg,
	)
}

type routeNode[IN any] struct {
	adkwf.BaseNode
	fn RouteFunc[IN]
}

func RouteNode[IN any](name string, fn RouteFunc[IN], opts ...NodeOption) Node {
	resolved := resolveNodeOptions(opts)
	return &routeNode[IN]{
		BaseNode: adkwf.NewBaseNode(name, "", resolved.cfg),
		fn:       fn,
	}
}

func (n *routeNode[IN]) Run(ctx agent.Context, input any) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		typed, err := coerceInput[IN](input)
		if err != nil {
			yield(nil, fmt.Errorf("route node %q: %w", n.Name(), err))
			return
		}

		emit := func(ev *session.Event) error {
			if !yield(ev, nil) {
				return ctx.Err()
			}
			return nil
		}
		wctx := newContext(ctx, emit)
		route, err := n.fn(wctx, typed)
		if err != nil {
			yield(nil, err)
			return
		}

		ev := session.NewEvent(ctx, ctx.InvocationID())
		ev.Author = ctx.AgentName()
		ev.Branch = ctx.Branch()
		ev.IsolationScope = ctx.IsolationScope()
		ev.NodeInfo = &session.NodeInfo{Path: ctx.Path()}
		ev.Output = input
		if route != "" {
			ev.Routes = []string{route}
		}
		yield(ev, nil)
	}
}

type AskBuilder[IN any] func(Context, IN) (session.RequestInput, error)
type AskHandler[IN, OUT any] func(Context, IN, any) (OUT, error)

func AskNode[IN, OUT any](name string, build AskBuilder[IN], handle AskHandler[IN, OUT], opts ...NodeOption) Node {
	resolved := resolveNodeOptions(opts)
	if resolved.cfg.RerunOnResume == nil {
		enabled := true
		resolved.cfg.RerunOnResume = &enabled
	}
	return adkwf.NewEmittingFunctionNode[IN, OUT](
		name,
		func(ctx agent.Context, input IN, emit func(*session.Event) error) (OUT, error) {
			var zero OUT
			wctx := newContext(ctx, emit)
			req, err := build(wctx, input)
			if err != nil {
				return zero, err
			}
			reply, err := wctx.Ask(req)
			if err != nil {
				return zero, err
			}
			if handle == nil {
				out, ok := reply.(OUT)
				if !ok {
					return zero, fmt.Errorf("ask node %q: response type %T does not satisfy expected output", name, reply)
				}
				return out, nil
			}
			return handle(wctx, input, reply)
		},
		resolved.cfg,
	)
}

type agentNode struct {
	Node
	agent agent.Agent
}

func AgentNode(a agent.Agent, opts ...NodeOption) (Node, error) {
	return AgentNodeTyped[any, any](a, opts...)
}

func AgentNodeTyped[IN, OUT any](a agent.Agent, opts ...NodeOption) (Node, error) {
	resolved := resolveNodeOptions(opts)
	node, err := adkwf.NewAgentNodeTyped[IN, OUT](a, resolved.cfg)
	if err != nil {
		return nil, err
	}
	return &agentNode{Node: node, agent: a}, nil
}

func (n *agentNode) workflowAgent() agent.Agent {
	return n.agent
}

func (n *agentNode) ValidateOutput(output any) (any, error) {
	normalized, err := normalizeAgentOutput(output)
	if err != nil {
		return nil, err
	}
	return n.Node.ValidateOutput(normalized)
}

type RunNodeOption = adkwf.RunNodeOption

var (
	WithRunID                      = adkwf.WithRunID
	WithUseSubBranch               = adkwf.WithUseSubBranch
	WithUseAsOutput                = adkwf.WithUseAsOutput
	WithOverrideBranch             = adkwf.WithOverrideBranch
	WithIsolationScope             = adkwf.WithIsolationScope
	WithIsolationScopeFromNodePath = adkwf.WithIsolationScopeFromNodePath
	WithRaiseOnWait                = adkwf.WithRaiseOnWait
)

func RunNode[OUT any](ctx Context, child Node, input any, opts ...RunNodeOption) (OUT, error) {
	return adkwf.RunNode[OUT](ctx, child, input, opts...)
}

func JoinNode(name string) Node {
	return adkwf.NewJoinNode(name)
}

func coerceInput[T any](input any) (T, error) {
	var zero T
	if input == nil {
		return zero, nil
	}
	if typed, ok := input.(T); ok {
		return typed, nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return zero, err
	}
	if err := json.Unmarshal(data, &zero); err != nil {
		return zero, err
	}
	return zero, nil
}

func normalizeAgentOutput(output any) (any, error) {
	if output == nil {
		return nil, nil
	}
	switch output.(type) {
	case string, bool, int, int64, float64, map[string]any, []any, *session.Event, *session.RequestInput, *genai.Content, genai.Content:
		return output, nil
	}
	value := reflect.ValueOf(output)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, nil
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct && value.Kind() != reflect.Map && value.Kind() != reflect.Slice {
		return output, nil
	}
	data, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("workflow: normalize agent output: %w", err)
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, fmt.Errorf("workflow: normalize agent output: %w", err)
	}
	return normalized, nil
}
