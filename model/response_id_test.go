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
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/volcengine/veadk-go/v2/common"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/genai"
)

type responseIDTransport func(*http.Request) (*http.Response, error)

func (f responseIDTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type responseIDBody struct {
	io.ReadCloser
	closed *atomic.Int32
}

func (b *responseIDBody) Close() error {
	b.closed.Add(1)
	return b.ReadCloser.Close()
}

func responseIDTracer(t *testing.T, enabled bool) (trace.Tracer, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	opts := []sdktrace.TracerProviderOption{sdktrace.WithSyncer(exporter)}
	if !enabled {
		opts = append(opts, sdktrace.WithSampler(sdktrace.NeverSample()))
	}
	provider := sdktrace.NewTracerProvider(opts...)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return provider.Tracer("response-id-test"), exporter
}

// Route the real Ark URL to a local fixture without changing the model's service
// identity. Only the test transport rewrites the destination; production does not.
func responseIDModel(t *testing.T, adapter string, server *httptest.Server, provider APIKeyProvider, closed *atomic.Int32) adkmodel.LLM {
	t.Helper()
	destination, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	base := server.URL + "/api/v3"
	if adapter == "ark-compatible" {
		base = common.DEFAULT_MODEL_AGENT_API_BASE
	}
	client := &http.Client{Transport: responseIDTransport(func(req *http.Request) (*http.Response, error) {
		copyReq := req.Clone(req.Context())
		copyReq.URL.Scheme = destination.Scheme
		copyReq.URL.Host = destination.Host
		resp, err := server.Client().Transport.RoundTrip(copyReq)
		if err == nil {
			resp.Body = &responseIDBody{ReadCloser: resp.Body, closed: closed}
		}
		return resp, err
	})}
	key := "fixture-key"
	if provider != nil {
		key = ""
	}
	var llm adkmodel.LLM
	if adapter == "ark-sdk" {
		llm, err = NewArkModel(t.Context(), "fixture-model", &ArkClientConfig{
			APIKey: key, APIKeyProvider: provider, BaseURL: base, HTTPClient: client,
			ExtraHeaders: map[string]string{"x-request-id": "request-only-id"},
		})
	} else {
		llm, err = NewOpenAIModel(t.Context(), "fixture-model", &ClientConfig{
			APIKey: key, APIKeyProvider: provider, BaseURL: base, HTTPClient: client,
			ExtraHeaders: map[string]string{"x-request-id": "request-only-id"},
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	return llm
}

func writeResponseIDChat(w http.ResponseWriter, stream bool, id string, usageOnly bool) {
	if !stream {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":%q,"model":"fixture-model","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`, id)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	if usageOnly {
		_, _ = fmt.Fprintf(w, "data: {\"id\":%q,\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\n\n", id)
	} else {
		_, _ = fmt.Fprintf(w, "data: {\"id\":%q,\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n", id)
		// Neither the empty terminal ID nor an empty choices list may erase the
		// provider ID observed in the earlier chunk.
		_, _ = io.WriteString(w, "data: {\"id\":\"\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\n\n")
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

func exportedResponseID(span tracetest.SpanStub) (string, bool) {
	for _, attr := range span.Attributes {
		if string(attr.Key) == responseIDAttribute {
			return attr.Value.AsString(), true
		}
	}
	return "", false
}

func checkResponseID(t *testing.T, span tracetest.SpanStub, want string) {
	t.Helper()
	got, exists := exportedResponseID(span)
	if got != want || exists != (want != "") {
		t.Errorf("span %q response ID = (%q, %v), want (%q, %v)", span.Name, got, exists, want, want != "")
	}
}

func TestChatResponseIDExport(t *testing.T) {
	cases := []struct {
		name, header, body string
	}{
		{"header-precedence", "header-provider-id", "different-body-id"},
		{"body-fallback", "", "body-provider-id"},
		{"blank-header", " ", "body-provider-id"},
		{"header-only", "header-provider-id", ""},
		{"no-provider-id", "", ""},
		{"blank-body", "", " \t"},
		{"real-uuid", "", "f019bfa2-065e-4b3e-a357-4809208e6f79"},
		{"real-chatcmpl", "", "chatcmpl_provider"},
	}
	for _, adapter := range []string{"ark-sdk", "ark-compatible", "other-compatible"} {
		for _, stream := range []bool{false, true} {
			for _, tc := range cases {
				t.Run(fmt.Sprintf("%s/stream=%v/%s", adapter, stream, tc.name), func(t *testing.T) {
					tracer, exporter := responseIDTracer(t, true)
					var requests, closed atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests.Add(1)
						if r.Header.Get("x-request-id") != "request-only-id" {
							t.Error("fixture request header was not preserved")
						}
						w.Header().Set("X-ReQuEsT-Id", tc.header)
						writeResponseIDChat(w, stream, tc.body, false)
					}))
					t.Cleanup(server.Close)
					llm := responseIDModel(t, adapter, server, nil, &closed)
					ctx, span := tracer.Start(t.Context(), "generate_content fixture-model")
					seq := llm.GenerateContent(ctx, &adkmodel.LLMRequest{Contents: genai.Text("hi")}, stream)
					var final *adkmodel.LLMResponse
					var partialText strings.Builder
					for resp, err := range seq {
						if err != nil {
							t.Fatal(err)
						}
						if resp.Partial {
							partialText.WriteString(resp.Content.Parts[0].Text)
						} else {
							final = resp
							// Reproduce ADK's end-before-yield behavior.
							span.End()
						}
						_, consumer := tracer.Start(ctx, "consumer")
						consumer.End()
					}
					span.End()
					if final == nil || final.Content.Parts[0].Text != "hello" || final.UsageMetadata.TotalTokenCount != 3 {
						t.Fatalf("content/usage changed: %#v", final)
					}
					if stream && partialText.String() != "hello" {
						t.Errorf("partial chunks = %q", partialText.String())
					}
					want := tc.body
					if strings.TrimSpace(want) == "" {
						want = ""
					}
					if adapter != "other-compatible" && strings.TrimSpace(tc.header) != "" {
						want = tc.header
					}
					var calls int
					for _, exported := range exporter.GetSpans() {
						if exported.Name == "consumer" {
							checkResponseID(t, exported, "")
						} else {
							calls++
							checkResponseID(t, exported, want)
						}
					}
					if calls != 1 {
						t.Errorf("exported call spans=%d, want 1", calls)
					}
					if requests.Load() != 1 || closed.Load() < 1 {
						t.Errorf("requests=%d closed=%d", requests.Load(), closed.Load())
					}
				})
			}
		}
	}
}

func TestChatResponseIDRetry(t *testing.T) {
	for _, adapter := range []string{"ark-sdk", "ark-compatible", "other-compatible"} {
		for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
			if adapter != "ark-sdk" && status == http.StatusInternalServerError {
				continue // compatible models do not implement HTTP 500 retry.
			}
			for _, stream := range []bool{false, true} {
				for _, finalID := range []string{"final-provider-id", ""} {
					t.Run(fmt.Sprintf("%s/status=%d/stream=%v/id=%s", adapter, status, stream, finalID), func(t *testing.T) {
						tracer, exporter := responseIDTracer(t, true)
						var requests, closed atomic.Int32
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							if requests.Add(1) == 1 {
								w.Header().Set("x-request-id", "failed-attempt-id")
								w.WriteHeader(status)
								_, _ = io.WriteString(w, `{"id":"failed-body-id","error":{"message":"retry fixture","type":"fixture"}}`)
								return
							}
							w.Header().Set("x-request-id", finalID)
							writeResponseIDChat(w, stream, finalID, false)
						}))
						t.Cleanup(server.Close)
						provider := &rotatingTestAPIKeyProvider{keys: []string{"old-key", "new-key"}}
						llm := responseIDModel(t, adapter, server, provider, &closed)
						ctx, span := tracer.Start(t.Context(), "generate_content retry")
						for _, err := range llm.GenerateContent(ctx, &adkmodel.LLMRequest{Contents: genai.Text("hi")}, stream) {
							if err != nil {
								t.Fatal(err)
							}
						}
						span.End()
						spans := exporter.GetSpans()
						if len(spans) != 1 {
							t.Fatalf("spans=%d, want one existing call span", len(spans))
						}
						checkResponseID(t, spans[0], finalID)
						wantClosed := int32(2)
						if adapter == "ark-sdk" && stream {
							// SDK v1.1.53 does not close failed stream-creation
							// responses in sendChatCompletionRequestStream.
							// Preserve that baseline; the successful stream is closed.
							wantClosed = 1
						}
						if requests.Load() != 2 || closed.Load() != wantClosed {
							t.Errorf("requests=%d closed=%d", requests.Load(), closed.Load())
						}
					})
				}
			}
		}
	}
}

func TestChatResponseIDStreamBoundaries(t *testing.T) {
	for _, adapter := range []string{"ark-sdk", "ark-compatible", "other-compatible"} {
		for _, mode := range []string{"usage-only", "early-stop", "cancel", "disabled"} {
			t.Run(adapter+"/"+mode, func(t *testing.T) {
				tracer, exporter := responseIDTracer(t, mode != "disabled")
				var closed atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if mode == "early-stop" || mode == "cancel" {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"id\":\"stream-provider-id\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
						w.(http.Flusher).Flush()
						<-r.Context().Done()
						return
					}
					writeResponseIDChat(w, true, "stream-provider-id", mode == "usage-only")
				}))
				t.Cleanup(server.Close)
				llm := responseIDModel(t, adapter, server, nil, &closed)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				ctx, span := tracer.Start(ctx, "generate_content boundary")
				var responses int
				var streamErr error
				for resp, err := range llm.GenerateContent(ctx, &adkmodel.LLMRequest{Contents: genai.Text("hi")}, true) {
					if err != nil {
						streamErr = err
						break
					}
					responses++
					if mode == "usage-only" && (resp.UsageMetadata == nil || resp.UsageMetadata.TotalTokenCount != 3) {
						t.Errorf("usage-only result lost usage: %#v", resp)
					}
					if mode == "early-stop" {
						break
					}
					if mode == "cancel" {
						cancel()
					}
				}
				span.End()
				if responses == 0 || closed.Load() == 0 {
					t.Errorf("responses=%d closed=%d", responses, closed.Load())
				}
				if mode == "cancel" && streamErr == nil {
					t.Error("cancellation error was lost")
				}
				spans := exporter.GetSpans()
				if mode == "disabled" {
					if len(spans) != 0 {
						t.Errorf("disabled tracing exported %d spans", len(spans))
					}
				} else if len(spans) != 1 {
					t.Errorf("spans=%d", len(spans))
				} else {
					checkResponseID(t, spans[0], "stream-provider-id")
				}
			})
		}
	}
}

func TestChatResponseIDConcurrentStreams(t *testing.T) {
	for _, adapter := range []string{"ark-sdk", "ark-compatible", "other-compatible"} {
		t.Run(adapter, func(t *testing.T) {
			tracer, exporter := responseIDTracer(t, true)
			var closed atomic.Int32
			var arrived sync.WaitGroup
			arrived.Add(3)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Messages []struct{ Content string }
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				id := request.Messages[len(request.Messages)-1].Content
				w.Header().Set("x-request-id", "header-"+id)
				w.Header().Set("Content-Type", "text/event-stream")
				w.(http.Flusher).Flush()
				arrived.Done()
				arrived.Wait()
				writeResponseIDChat(w, true, "body-"+id, false)
			}))
			t.Cleanup(server.Close)
			llm := responseIDModel(t, adapter, server, nil, &closed)
			var workers sync.WaitGroup
			for i := range 3 {
				workers.Add(1)
				go func() {
					defer workers.Done()
					id := fmt.Sprintf("request-%d", i)
					ctx, span := tracer.Start(t.Context(), id)
					defer span.End()
					for _, err := range llm.GenerateContent(ctx, &adkmodel.LLMRequest{Contents: genai.Text(id)}, true) {
						if err != nil {
							t.Error(err)
							return
						}
						_, consumer := tracer.Start(ctx, "consumer-"+id)
						consumer.End()
					}
				}()
			}
			workers.Wait()
			var modelSpans int
			for _, span := range exporter.GetSpans() {
				if strings.HasPrefix(span.Name, "consumer-") {
					checkResponseID(t, span, "")
					continue
				}
				modelSpans++
				prefix := "header-"
				if adapter == "other-compatible" {
					prefix = "body-"
				}
				checkResponseID(t, span, prefix+span.Name)
			}
			if modelSpans != 3 || closed.Load() != 3 {
				t.Errorf("modelSpans=%d closed=%d", modelSpans, closed.Load())
			}
		})
	}
}

func TestOfficialResponsesIDExport(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, id := range []string{"resp_provider_fixture", ""} {
			t.Run(fmt.Sprintf("stream=%v/id=%s", stream, id), func(t *testing.T) {
				tracer, exporter := responseIDTracer(t, true)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("x-request-id", "chat-header-must-not-win")
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":%q,\"model\":\"fixture-model\"}}\n\n", id)
						_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
						_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":%q,\"model\":\"fixture-model\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1,\"total_tokens\":3}}}\n\ndata: [DONE]\n\n", id)
					} else {
						w.Header().Set("Content-Type", "application/json")
						_, _ = fmt.Fprintf(w, `{"id":%q,"model":"fixture-model","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`, id)
					}
				}))
				t.Cleanup(server.Close)
				original, err := openaimodel.NewModel(t.Context(), "fixture-model", &openaimodel.ClientConfig{APIKey: "fixture-key", BaseURL: server.URL, HTTPClient: server.Client()})
				if err != nil {
					t.Fatal(err)
				}
				llm := WithResponseIDTracing(original)
				if llm.Name() != original.Name() || WithResponseIDTracing(llm) != llm {
					t.Fatal("model identity/idempotent wrapping changed")
				}
				ctx, span := tracer.Start(t.Context(), "generate_content responses")
				var final *adkmodel.LLMResponse
				for resp, err := range llm.GenerateContent(ctx, &adkmodel.LLMRequest{Contents: genai.Text("hi")}, stream) {
					if err != nil {
						t.Fatal(err)
					}
					if !resp.Partial {
						final = resp
						span.End() // ADK ends here, before AfterModel callbacks.
					}
				}
				span.End()
				if final == nil || final.Content.Parts[0].Text != "hello" || final.UsageMetadata.TotalTokenCount != 3 || final.CustomMetadata["openai_response_id"] != id {
					t.Fatalf("official response was changed: %#v", final)
				}
				spans := exporter.GetSpans()
				if len(spans) != 1 {
					t.Fatalf("spans=%d", len(spans))
				}
				checkResponseID(t, spans[0], id)
			})
		}
	}
}

func TestOfficialResponsesFailureAndEarlyStop(t *testing.T) {
	for _, earlyStop := range []bool{false, true} {
		t.Run(fmt.Sprintf("early-stop=%v", earlyStop), func(t *testing.T) {
			tracer, exporter := responseIDTracer(t, true)
			var closed atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-request-id", "chat-header-must-not-win")
				if !earlyStop {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":{"message":"fixture-rejection","type":"invalid_request_error"}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_early_stop\"}}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\ndata: [DONE]\n\n")
			}))
			t.Cleanup(server.Close)
			client := &http.Client{Transport: responseIDTransport(func(req *http.Request) (*http.Response, error) {
				resp, err := server.Client().Transport.RoundTrip(req)
				if err == nil {
					resp.Body = &responseIDBody{ReadCloser: resp.Body, closed: &closed}
				}
				return resp, err
			})}
			original, err := openaimodel.NewModel(t.Context(), "fixture-model", &openaimodel.ClientConfig{APIKey: "fixture-key", BaseURL: server.URL, HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			ctx, span := tracer.Start(t.Context(), "generate_content responses")
			var modelErr error
			for resp, err := range WithResponseIDTracing(original).GenerateContent(ctx, &adkmodel.LLMRequest{Contents: genai.Text("hi")}, earlyStop) {
				modelErr = err
				if earlyStop && (err != nil || !resp.Partial || resp.Content.Parts[0].Text != "hello") {
					t.Fatalf("first partial changed: response=%#v error=%v", resp, err)
				}
				break
			}
			span.End()
			if !earlyStop && (modelErr == nil || !strings.Contains(modelErr.Error(), "fixture-rejection")) {
				t.Errorf("provider error was lost: %v", modelErr)
			}
			spans := exporter.GetSpans()
			if len(spans) != 1 || closed.Load() == 0 {
				t.Fatalf("spans=%d closed=%d", len(spans), closed.Load())
			}
			want := ""
			if earlyStop {
				want = "resp_early_stop"
			}
			checkResponseID(t, spans[0], want)
		})
	}
}

func TestArkChatEndpointIdentity(t *testing.T) {
	for _, tc := range []struct {
		url string
		ark bool
	}{
		{common.DEFAULT_MODEL_AGENT_API_BASE, true},
		{"https://ARK.cn-beijing.volces.com:443/api/v3/", true},
		{"https://ark.ap-southeast-1.volces.com/api/v3", true},
		{"https://api.openai.com/v1", false},
		{"https://ark.cn-beijing.volces.com.evil.example/v1", false},
		{"https://evil.example/ark.cn-beijing.volces.com", false},
		{"https://ark.cn-beijing.volces.com@evil.example/v1", false},
		{"http://localhost:8000/api/v3", false},
		{"not-a-url", false},
	} {
		if got := isArkChatEndpoint(tc.url); got != tc.ark {
			t.Errorf("isArkChatEndpoint(%q)=%v want %v", tc.url, got, tc.ark)
		}
	}
}

type synthesizedIDModel struct{}

func (synthesizedIDModel) Name() string { return "custom-model" }

func (synthesizedIDModel) GenerateContent(context.Context, *adkmodel.LLMRequest, bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		yield(&adkmodel.LLMResponse{CustomMetadata: map[string]any{
			"openai_response_id": "locally-synthesized-id",
		}}, nil)
	}
}

func TestResponseIDDoesNotTrustCustomMetadata(t *testing.T) {
	tracer, exporter := responseIDTracer(t, true)
	ctx, span := tracer.Start(t.Context(), "generate_content custom-model")
	llm := synthesizedIDModel{}
	wrapped := WithResponseIDTracing(llm)
	if wrapped != llm || WithResponseIDTracing(nil) != nil {
		t.Fatal("unrecognized models must remain unchanged")
	}
	for _, err := range wrapped.GenerateContent(ctx, &adkmodel.LLMRequest{}, false) {
		if err != nil {
			t.Fatal(err)
		}
	}
	span.End()
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans=%d", len(spans))
	}
	checkResponseID(t, spans[0], "")
}
