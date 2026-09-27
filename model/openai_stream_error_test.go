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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestOpenAIModelStreamingAPIError(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%t", partial), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if partial {
					_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"before error\"}}]}\n\n")
				}
				_, _ = fmt.Fprint(w, "data: {\"error\":{\"message\":\"quota exhausted\",\"type\":\"insufficient_quota\",\"code\":\"quota_limit\"}}\n\n")
				// Nothing after a terminal API error should be yielded as success.
				_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"after error\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()

			llm := newTestModel(t, server)
			req := &model.LLMRequest{Contents: genai.Text("test")}
			var partials, failures int
			for response, err := range llm.GenerateContent(t.Context(), req, true) {
				if err != nil {
					failures++
					if response != nil {
						t.Error("API error must not include a successful response")
					}
					for _, detail := range []string{"quota exhausted", "insufficient_quota", "quota_limit"} {
						if !strings.Contains(err.Error(), detail) {
							t.Errorf("error %q is missing %q", err, detail)
						}
					}
					continue
				}
				partials++
				if !response.Partial || response.Content.Parts[0].Text != "before error" {
					t.Errorf("unexpected successful response: %+v", response)
				}
			}
			if failures != 1 {
				t.Errorf("got %d errors, want exactly one", failures)
			}
			wantPartials := 0
			if partial {
				wantPartials = 1
			}
			if partials != wantPartials {
				t.Errorf("got %d successful responses, want %d partials", partials, wantPartials)
			}
		})
	}
}

func TestOpenAIModelStreamingAPIErrorClosesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"error\":{\"message\":\"generation failed\"}}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	llm := newTestModel(t, server)
	var failures int
	for response, err := range llm.GenerateContent(ctx, &model.LLMRequest{Contents: genai.Text("test")}, true) {
		failures++
		if response != nil || err == nil || !strings.Contains(err.Error(), "generation failed") {
			t.Errorf("expected the API error, got response=%+v error=%v", response, err)
		}
	}
	if failures != 1 {
		t.Errorf("got %d results, want one API error", failures)
	}
	if ctx.Err() != nil {
		t.Errorf("waited for request cancellation instead of returning the API error: %v", ctx.Err())
	}
}

func TestOpenAIModelStreamingNullError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"error\":null,\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	llm := newTestModel(t, server)
	var partials, finals int
	for response, err := range llm.GenerateContent(t.Context(), &model.LLMRequest{Contents: genai.Text("test")}, true) {
		if err != nil {
			t.Fatalf("null error interrupted a successful stream: %v", err)
		}
		if response.Content.Parts[0].Text != "hello" {
			t.Errorf("unexpected text: %+v", response.Content)
		}
		if response.Partial {
			partials++
		} else {
			finals++
		}
	}
	if partials != 1 || finals != 1 {
		t.Errorf("got %d partials and %d finals, want one of each", partials, finals)
	}
}
