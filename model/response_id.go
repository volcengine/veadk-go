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
	"iter"
	"net/url"
	"reflect"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/v2/model"
)

const responseIDAttribute = "gen_ai.response.id"

// responseIDRecorder belongs to one consumption of a model iterator. The span
// comes from the producer's context, never from a consumer or shared model state.
type responseIDRecorder struct {
	span trace.Span
	id   string
}

func (r *responseIDRecorder) record(id string) {
	if r.id != "" || strings.TrimSpace(id) == "" || !r.span.IsRecording() {
		return
	}
	r.id = id
	r.span.SetAttributes(attribute.String(responseIDAttribute, id))
}

// Ark's header precedence applies to known Ark service hosts, not arbitrary
// compatible endpoints, model names, or endpoints that merely return the header.
func isArkChatEndpoint(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return strings.HasPrefix(host, "ark.") && strings.HasSuffix(host, ".volces.com")
}

// WithResponseIDTracing preserves the native response.id of the official ADK
// OpenAI Responses adapter before ADK ends its model span. Other models are
// returned unchanged: their private metadata is not a provider ID contract.
func WithResponseIDTracing(llm model.LLM) model.LLM {
	t := reflect.TypeOf(llm)
	if t == nil || t.Kind() != reflect.Pointer {
		return llm
	}
	// The official adapter has no exported concrete type or metadata interface.
	// Check its defining package rather than accepting a similarly named field
	// from an arbitrary user model (which may contain a synthesized ID).
	if t.Elem().PkgPath() != "google.golang.org/adk/v2/model/openaimodel" {
		return llm
	}
	return &responsesTraceModel{LLM: llm}
}

type responsesTraceModel struct {
	model.LLM
}

func (m *responsesTraceModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	span := trace.SpanFromContext(ctx)
	seq := m.LLM.GenerateContent(ctx, req, stream)
	return func(yield func(*model.LLMResponse, error) bool) {
		recorder := responseIDRecorder{span: span}
		for resp, err := range seq {
			if err == nil && resp != nil {
				if id, ok := resp.CustomMetadata["openai_response_id"].(string); ok {
					recorder.record(id)
				}
			}
			if !yield(resp, err) {
				return
			}
		}
	}
}
