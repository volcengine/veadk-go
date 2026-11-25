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

package model

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/volcengine/veadk-go/v2/observability"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/utils"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// streamingTPOT is local to one SDK call. It retains no generated content.
type streamingTPOT struct {
	invalid      bool
	first, last  time.Time
	increments   int
	sawReasoning bool
	terminal     bool
	usageAtEnd   bool
	usageValid   bool
	completion   int
	reasoning    int
	recorded     bool
}

func newStreamingTPOT(stream *utils.ChatCompletionStreamReader, req *arkmodel.CreateChatCompletionRequest) *streamingTPOT {
	t := &streamingTPOT{}
	if req.N != nil && *req.N != 1 {
		t.invalid = true
	}
	// Only the official decoder gives us both the original JSON and SDK chunk.
	// Unknown wrappers/replays cannot supply trustworthy output-scope evidence.
	if decoder, ok := stream.Unmarshaler.(*utils.JSONUnmarshaler); ok && decoder != nil && !stream.IsFinished {
		stream.Unmarshaler = &tpotUnmarshaler{original: decoder, timing: t}
	} else {
		t.invalid = true
	}
	return t
}

type tpotUnmarshaler struct {
	original utils.Unmarshaler
	timing   *streamingTPOT
}

func (u *tpotUnmarshaler) Unmarshal(data []byte, target interface{}) error {
	// Never recover or replace an error/panic from the original SDK decoder.
	if err := u.original.Unmarshal(data, target); err != nil {
		return err
	}
	if _, ok := target.(*arkmodel.ChatCompletionStreamResponse); ok {
		if !safeStreamingTelemetry(func() { u.timing.inspectJSON(data) }) {
			u.timing.invalid = true
		}
	}
	return nil
}

// inspectJSON checks fields that the SDK would otherwise silently discard.
// Unknown output/usage schema is fail-closed, while the original response is untouched.
func (t *streamingTPOT) inspectJSON(data []byte) {
	if t.invalid {
		return
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || raw == nil ||
		!onlyTPOTKeys(raw, "id", "object", "created", "model", "service_tier", "choices", "usage") {
		t.invalid = true
		return
	}
	var object string
	if json.Unmarshal(raw["object"], &object) != nil || object != "chat.completion.chunk" {
		t.invalid = true
		return
	}
	var choices []map[string]json.RawMessage
	if json.Unmarshal(raw["choices"], &choices) != nil || choices == nil || len(choices) > 1 {
		t.invalid = true
		return
	}
	for _, choice := range choices {
		var index *int
		var delta map[string]json.RawMessage
		if !onlyTPOTKeys(choice, "index", "delta", "finish_reason", "logprobs", "moderation_hit_type") ||
			json.Unmarshal(choice["index"], &index) != nil || index == nil || *index != 0 ||
			json.Unmarshal(choice["delta"], &delta) != nil || delta == nil ||
			!onlyTPOTKeys(delta, "role", "content", "reasoning_content", "tool_calls", "function_call") {
			t.invalid = true
			return
		}
		// Legacy function_call deltas are not emitted by the current Ark adapter.
		if v := delta["function_call"]; len(v) != 0 && string(v) != "null" {
			t.invalid = true
			return
		}
		if v := choice["moderation_hit_type"]; len(v) != 0 && string(v) != "null" && string(v) != `""` {
			t.invalid = true
			return
		}
		if v := delta["tool_calls"]; len(v) != 0 && string(v) != "null" {
			var tools []map[string]json.RawMessage
			if json.Unmarshal(v, &tools) != nil {
				t.invalid = true
				return
			}
			for _, tool := range tools {
				var function map[string]json.RawMessage
				if !onlyTPOTKeys(tool, "index", "id", "type", "function") ||
					json.Unmarshal(tool["function"], &function) != nil ||
					!onlyTPOTKeys(function, "name", "arguments") {
					t.invalid = true
					return
				}
			}
		}
	}
	// Usage is cumulative. Missing/null usage in content chunks is normal.
	// A non-null but invalid later usage must not revive an earlier valid count.
	if usage := raw["usage"]; len(usage) != 0 && string(usage) != "null" {
		var fields map[string]json.RawMessage
		var completion *int
		t.usageValid = false
		if json.Unmarshal(usage, &fields) != nil ||
			!onlyTPOTKeys(fields, "prompt_tokens", "completion_tokens", "total_tokens", "prompt_tokens_details", "completion_tokens_details") {
			t.invalid = true
			return
		}
		if json.Unmarshal(fields["completion_tokens"], &completion) != nil || completion == nil || *completion <= 1 {
			return
		}
		t.usageValid = true
		if details := fields["completion_tokens_details"]; len(details) != 0 && string(details) != "null" {
			var counters map[string]json.RawMessage
			if json.Unmarshal(details, &counters) != nil ||
				!onlyTPOTKeys(counters, "reasoning_tokens", "provisioned_tokens") {
				t.invalid = true
				return
			}
			if v := counters["reasoning_tokens"]; len(v) != 0 {
				var reasoning *int
				if json.Unmarshal(v, &reasoning) != nil || reasoning == nil || *reasoning < 0 || *reasoning > *completion {
					t.usageValid = false
				}
			}
		}
	}
}

func onlyTPOTKeys(fields map[string]json.RawMessage, allowed ...string) bool {
	for key := range fields {
		found := false
		for _, name := range allowed {
			if key == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (t *streamingTPOT) observe(chunk arkmodel.ChatCompletionStreamResponse, received time.Time) {
	if t.invalid {
		return
	}
	if len(chunk.Choices) > 1 {
		t.invalid = true
		return
	}
	if len(chunk.Choices) == 1 {
		choice := chunk.Choices[0]
		if choice == nil || choice.Index != 0 {
			t.invalid = true
			return
		}
		delta := choice.Delta
		reasoning := delta.ReasoningContent != nil && *delta.ReasoningContent != ""
		output := delta.Content != "" || reasoning
		for _, tool := range delta.ToolCalls {
			if tool != nil && tool.Function.Arguments != "" {
				output = true
			}
		}
		if output {
			if t.terminal {
				t.invalid = true
				return
			}
			if t.increments == 0 {
				t.first = received
			}
			t.last = received
			t.increments++
			t.sawReasoning = t.sawReasoning || reasoning
			// Usage before this output cannot be the final output count.
			t.usageAtEnd = false
		}
		reason := choice.FinishReason
		if reason != "" && reason != arkmodel.FinishReasonNull {
			if t.terminal || (reason != arkmodel.FinishReasonStop &&
				reason != arkmodel.FinishReasonToolCalls && reason != arkmodel.FinishReasonFunctionCall) {
				t.invalid = true
				return
			}
			t.terminal = true
		}
	}
	if chunk.Usage != nil {
		t.completion = chunk.Usage.CompletionTokens
		t.reasoning = chunk.Usage.CompletionTokensDetails.ReasoningTokens
		t.usageAtEnd = t.terminal
	}
}

func (t *streamingTPOT) value() (float64, bool) {
	if t.invalid || t.increments < 2 || !t.terminal || !t.usageAtEnd || !t.usageValid ||
		t.completion <= 1 || t.reasoning < 0 || t.reasoning > t.completion ||
		(t.reasoning > 0 && !t.sawReasoning) {
		return 0, false
	}
	duration := t.last.Sub(t.first).Seconds()
	value := duration / float64(t.completion-1)
	return value, duration >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (t *streamingTPOT) record(ctx context.Context, span trace.Span, modelName string) {
	if t.recorded {
		return
	}
	t.recorded = true
	value, ok := t.value()
	if !ok {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String(observability.AttrGenAIRequestModel, modelName),
		attribute.String(observability.AttrGenAIOperationName, "chat"),
	}
	// Each sink is isolated, so a failing trace sink cannot disable metrics.
	safeStreamingTelemetry(func() {
		span.SetAttributes(attribute.Float64(observability.MetricNameStreamingTimePerOutputToken, value))
	})
	safeStreamingTelemetry(func() {
		observability.RecordStreamingTimePerOutputToken(ctx, value, attrs...)
	})
}

func startStreamingRequestSpan(ctx context.Context, modelName string) (context.Context, trace.Span) {
	requestCtx := ctx
	span := trace.SpanFromContext(context.Background())
	safeStreamingTelemetry(func() {
		requestCtx, span = otel.Tracer(observability.InstrumentationName).Start(ctx, "model_request",
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String(observability.AttrGenAIRequestModel, modelName),
				attribute.String(observability.AttrGenAIOperationName, "chat"),
			))
	})
	return requestCtx, span
}

func endStreamingRequestSpan(span trace.Span) {
	if span != nil {
		safeStreamingTelemetry(func() { span.End() })
	}
}

// Recovery is restricted to newly introduced telemetry calls, never model/yield/Close.
func safeStreamingTelemetry(fn func()) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	fn()
	return true
}
