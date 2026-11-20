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
	"fmt"

	"google.golang.org/adk/v2/agent"
	adkwfagent "google.golang.org/adk/v2/agent/workflowagent"
	adkwf "google.golang.org/adk/v2/workflow"
)

type Builder struct {
	name        string
	description string
	edges       []Edge
	subAgents   []agent.Agent
	seenAgents  map[string]bool
}

type BuilderOption func(*Builder)

func WithDescription(description string) BuilderOption {
	return func(b *Builder) {
		b.description = description
	}
}

func New(name string, opts ...BuilderOption) *Builder {
	b := &Builder{
		name:       name,
		seenAgents: map[string]bool{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(b)
		}
	}
	return b
}

func (b *Builder) AddEdge(from, to Node) *Builder {
	b.edges = append(b.edges, Edge{From: from, To: to})
	b.collectAgents(from, to)
	return b
}

func (b *Builder) StartWith(node Node) *Builder {
	return b.AddEdge(Start, node)
}

func (b *Builder) Then(from, to Node) *Builder {
	return b.AddEdge(from, to)
}

type RouteCase struct {
	Route Route
	Node  Node
}

func Case(route any, node Node) RouteCase {
	return RouteCase{Route: toRoute(route), Node: node}
}

func (b *Builder) Route(from Node, cases ...RouteCase) *Builder {
	for _, c := range cases {
		b.edges = append(b.edges, Edge{From: from, To: c.Node, Route: c.Route})
		b.collectAgents(from, c.Node)
	}
	return b
}

func (b *Builder) Parallel(from Node, nodes ...Node) *ParallelGroup {
	for _, node := range nodes {
		b.AddEdge(from, node)
	}
	return &ParallelGroup{builder: b, nodes: append([]Node(nil), nodes...)}
}

func (b *Builder) Merge(name string, from ...Node) Node {
	join := JoinNode(name)
	for _, node := range from {
		b.AddEdge(node, join)
	}
	return join
}

func (b *Builder) Edges() []Edge {
	return append([]Edge(nil), b.edges...)
}

func (b *Builder) Build() (*adkwf.Workflow, error) {
	return adkwf.New(b.name, b.edges)
}

func (b *Builder) Agent() (agent.Agent, error) {
	return adkwfagent.New(adkwfagent.Config{
		Name:        b.name,
		Description: b.description,
		Edges:       b.Edges(),
		SubAgents:   append([]agent.Agent(nil), b.subAgents...),
	})
}

func (b *Builder) collectAgents(nodes ...Node) {
	for _, node := range nodes {
		agentNode, ok := node.(interface{ workflowAgent() agent.Agent })
		if !ok {
			continue
		}
		a := agentNode.workflowAgent()
		if a == nil || b.seenAgents[a.Name()] {
			continue
		}
		b.seenAgents[a.Name()] = true
		b.subAgents = append(b.subAgents, a)
	}
}

type ParallelGroup struct {
	builder *Builder
	nodes   []Node
}

func (g *ParallelGroup) Merge(name string) Node {
	if g == nil || g.builder == nil {
		return nil
	}
	return g.builder.Merge(name, g.nodes...)
}

func toRoute(route any) Route {
	switch v := route.(type) {
	case nil:
		return Default
	case Route:
		return v
	case string:
		return StringRoute(v)
	case int:
		return IntRoute(v)
	case bool:
		return BoolRoute(v)
	default:
		panic(fmt.Sprintf("workflow: unsupported route type %T", route))
	}
}
