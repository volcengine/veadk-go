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
	"context"
	"fmt"
	"sort"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	adkwf "google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

const (
	EventTypeProgress = "workflow.progress"
	EventTypeState    = "workflow.state"
)

type Context interface {
	agent.Context

	EmitProgress(message string, fields ...Field) error
	EmitState(values map[string]any) error
	EmitOutput(value any) error
	Ask(req session.RequestInput) (any, error)
	Trace(name string, fields ...Field)
	Span() trace.Span
}

type contextWithEmit struct {
	agent.Context
	emit func(*session.Event) error
}

func newContext(ctx agent.Context, emit func(*session.Event) error) Context {
	return &contextWithEmit{Context: ctx, emit: emit}
}

func (c *contextWithEmit) EmitProgress(message string, fields ...Field) error {
	if c.emit == nil {
		return fmt.Errorf("workflow: progress events are unavailable in this node")
	}
	ev := c.newEvent()
	ev.LLMResponse = model.LLMResponse{
		Content: genai.NewContentFromText(message, genai.RoleModel),
		CustomMetadata: map[string]any{
			"type":    EventTypeProgress,
			"message": message,
			"fields":  fieldsMap(fields),
		},
	}
	return c.emit(ev)
}

func (c *contextWithEmit) EmitState(values map[string]any) error {
	if c.emit == nil {
		return fmt.Errorf("workflow: state events are unavailable in this node")
	}
	if len(values) == 0 {
		return nil
	}
	stateDelta := make(map[string]any, len(values))
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if err := c.State().Set(key, value); err != nil {
			return err
		}
		stateDelta[key] = value
		keys = append(keys, key)
	}
	sort.Strings(keys)

	ev := c.newEvent()
	ev.Actions.StateDelta = stateDelta
	ev.LLMResponse.CustomMetadata = map[string]any{
		"type": EventTypeState,
		"keys": keys,
	}
	return c.emit(ev)
}

func (c *contextWithEmit) EmitOutput(value any) error {
	if c.emit == nil {
		return fmt.Errorf("workflow: output events are unavailable in this node")
	}
	ev := c.newEvent()
	ev.Output = value
	if text, ok := value.(string); ok {
		ev.Content = genai.NewContentFromText(text, genai.RoleModel)
	}
	return c.emit(ev)
}

func (c *contextWithEmit) Ask(req session.RequestInput) (any, error) {
	if c.emit == nil {
		return nil, fmt.Errorf("workflow: input requests are unavailable in this node")
	}
	return adkwf.ResumeOrRequestInput(c.Context, c.emit, req)
}

func (c *contextWithEmit) Trace(name string, fields ...Field) {
	span := c.Span()
	if span == nil || !span.SpanContext().IsValid() {
		return
	}
	span.AddEvent(name, trace.WithAttributes(attributes(fields)...))
}

func (c *contextWithEmit) Span() trace.Span {
	return trace.SpanFromContext(context.Context(c.Context))
}

func (c *contextWithEmit) newEvent() *session.Event {
	ev := session.NewEvent(c.Context, c.InvocationID())
	ev.Author = c.AgentName()
	ev.Branch = c.Branch()
	ev.IsolationScope = c.IsolationScope()
	ev.NodeInfo = &session.NodeInfo{Path: c.Path()}
	return ev
}

func fieldsMap(fields []Field) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]any, len(fields))
	for _, field := range fields {
		out[field.Key] = field.Value
	}
	return out
}

func attributes(fields []Field) []attribute.KeyValue {
	if len(fields) == 0 {
		return nil
	}
	out := make([]attribute.KeyValue, 0, len(fields))
	for _, field := range fields {
		switch v := field.Value.(type) {
		case string:
			out = append(out, attribute.String(field.Key, v))
		case bool:
			out = append(out, attribute.Bool(field.Key, v))
		case int:
			out = append(out, attribute.Int(field.Key, v))
		case int64:
			out = append(out, attribute.Int64(field.Key, v))
		case float64:
			out = append(out, attribute.Float64(field.Key, v))
		default:
			out = append(out, attribute.String(field.Key, fmt.Sprint(v)))
		}
	}
	return out
}
