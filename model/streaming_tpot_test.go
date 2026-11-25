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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/volcengine/veadk-go/v2/configs"
	"github.com/volcengine/veadk-go/v2/observability"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/utils"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func tpotChunk(delta string, reason string) string {
	return fmt.Sprintf(`{"object":"chat.completion.chunk","choices":[{"index":0,"delta":%s,"finish_reason":%q}]}`, delta, reason)
}

func tpotUsage(tokens string, details string) string {
	return fmt.Sprintf(`{"object":"chat.completion.chunk","choices":[],"usage":{"completion_tokens":%s,"completion_tokens_details":%s}}`, tokens, details)
}

func tpotNormalChunks() []string {
	return []string{
		tpotChunk(`{"content":"hello "}`, ""),
		tpotChunk(`{"content":"world"}`, ""),
		tpotChunk(`{}`, "stop"),
		tpotUsage("100", `{}`),
	}
}

func tpotFeed(t *testing.T, chunks []string, received []time.Duration) *streamingTPOT {
	t.Helper()
	timing := &streamingTPOT{}
	decoder := &tpotUnmarshaler{original: &utils.JSONUnmarshaler{}, timing: timing}
	base := time.Now()
	for i, raw := range chunks {
		var chunk arkmodel.ChatCompletionStreamResponse
		require.NoError(t, decoder.Unmarshal([]byte(raw), &chunk))
		timing.observe(chunk, base.Add(received[i]))
	}
	return timing
}

// REQ-001/002/003: raw SDK evidence, final cumulative counts, exact timing and omissions.
func TestStreamingTPOTEvidence(t *testing.T) {
	normal := tpotNormalChunks()
	t.Run("exact_formula_excludes_tail", func(t *testing.T) {
		timing := tpotFeed(t, normal, []time.Duration{5 * time.Second, 5990 * time.Millisecond, 9 * time.Second, 14 * time.Second})
		value, ok := timing.value()
		require.True(t, ok)
		require.Equal(t, 0.01, value)
	})
	t.Run("measured_zero", func(t *testing.T) {
		value, ok := tpotFeed(t, normal, []time.Duration{0, 0, 5 * time.Second, 10 * time.Second}).value()
		require.True(t, ok)
		require.Zero(t, value)
	})
	t.Run("cumulative_usage_is_replaced", func(t *testing.T) {
		chunks := []string{normal[0], tpotUsage("1", `{}`), normal[1], tpotUsage("25", `{}`), normal[2], normal[3]}
		value, ok := tpotFeed(t, chunks, []time.Duration{0, 0, 990 * time.Millisecond, time.Second, 2 * time.Second, 7 * time.Second}).value()
		require.True(t, ok)
		require.Equal(t, 0.01, value)
	})
	t.Run("mixed_fields_are_one_increment", func(t *testing.T) {
		chunks := []string{
			tpotChunk(`{"content":"text","reasoning_content":"full reasoning","tool_calls":[{"index":0,"function":{"name":"f","arguments":"{"}}]}`, ""),
			tpotChunk(`{"content":"more","reasoning_content":"more reasoning","tool_calls":[{"index":0,"function":{"arguments":"}"}}]}`, "tool_calls"),
			tpotUsage("100", `{"reasoning_tokens":80}`),
		}
		timing := tpotFeed(t, chunks, []time.Duration{0, 990 * time.Millisecond, 10 * time.Second})
		value, ok := timing.value()
		require.True(t, ok)
		require.Equal(t, 2, timing.increments)
		require.Equal(t, 0.01, value)
	})
	for _, field := range []string{`{"content":"x"}`, `{"reasoning_content":"full"}`, `{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}`} {
		t.Run("output_"+field, func(t *testing.T) {
			details := `{}`
			if strings.Contains(field, "reasoning") {
				details = `{"reasoning_tokens":10}`
			}
			chunks := []string{tpotChunk(`{"role":"assistant"}`, ""), tpotChunk(field, ""), tpotChunk(`{"tool_calls":[{"index":0,"id":"tool","function":{"name":"f"}}]}`, ""), tpotChunk(field, "stop"), tpotUsage("100", details)}
			timing := tpotFeed(t, chunks, []time.Duration{0, time.Second, 10 * time.Second, 1990 * time.Millisecond, 20 * time.Second})
			value, ok := timing.value()
			require.True(t, ok)
			require.Equal(t, 2, timing.increments)
			require.Equal(t, 0.01, value)
		})
	}
	for _, reason := range []string{"stop", "tool_calls", "function_call"} {
		t.Run("terminal_"+reason, func(t *testing.T) {
			chunks := []string{normal[0], normal[1], tpotChunk(`{}`, reason), normal[3]}
			_, ok := tpotFeed(t, chunks, []time.Duration{0, time.Second, 2 * time.Second, 3 * time.Second}).value()
			require.True(t, ok)
		})
	}
	t.Run("tokens_above_int32_use_original_int", func(t *testing.T) {
		chunks := []string{normal[0], normal[1], normal[2], tpotUsage("2147483649", `{}`)}
		value, ok := tpotFeed(t, chunks, []time.Duration{0, time.Second, 2 * time.Second, 3 * time.Second}).value()
		require.True(t, ok)
		require.Equal(t, 1.0/2147483648, value)
	})
	cases := []struct {
		name   string
		chunks []string
	}{
		{"no_output", []string{normal[2], normal[3]}},
		{"single_increment", []string{normal[0], normal[2], normal[3]}},
		{"role_and_empty_only", []string{tpotChunk(`{"role":"assistant","content":"","reasoning_content":""}`, ""), normal[2], normal[3]}},
		{"missing_usage", normal[:3]},
		{"usage_before_terminal", []string{normal[0], normal[1], normal[3], normal[2]}},
		{"missing_real_terminal", []string{normal[0], normal[1], normal[3]}},
		{"length", []string{normal[0], normal[1], tpotChunk(`{}`, "length"), normal[3]}},
		{"content_filter", []string{normal[0], normal[1], tpotChunk(`{}`, "content_filter"), normal[3]}},
		{"unknown_terminal", []string{normal[0], normal[1], tpotChunk(`{}`, "unknown"), normal[3]}},
		{"repeated_terminal", append(append([]string{}, normal...), normal[2])},
		{"generation_after_terminal", append(append([]string{}, normal...), normal[0])},
		{"hidden_reasoning", []string{normal[0], normal[1], normal[2], tpotUsage("100", `{"reasoning_tokens":20}`)}},
		{"negative_reasoning", []string{normal[0], normal[1], normal[2], tpotUsage("100", `{"reasoning_tokens":-1}`)}},
		{"reasoning_exceeds_total", []string{normal[0], normal[1], normal[2], tpotUsage("100", `{"reasoning_tokens":101}`)}},
		{"null_reasoning", []string{normal[0], normal[1], normal[2], tpotUsage("100", `{"reasoning_tokens":null}`)}},
		{"audio_tokens", []string{normal[0], normal[1], normal[2], tpotUsage("100", `{"audio_tokens":1}`)}},
		{"audio_delta", []string{tpotChunk(`{"content":"x","audio":{"data":"ignored"}}`, ""), normal[1], normal[2], normal[3]}},
		{"reasoning_summary", []string{tpotChunk(`{"content":"x","reasoning_summary":"ignored"}`, ""), normal[1], normal[2], normal[3]}},
		{"refusal", []string{tpotChunk(`{"content":"x","refusal":"ignored"}`, ""), normal[1], normal[2], normal[3]}},
		{"legacy_function_delta", []string{tpotChunk(`{"function_call":{"name":"f","arguments":"{}"}}`, ""), normal[1], normal[2], normal[3]}},
		{"unknown_output", []string{tpotChunk(`{"content":"x","new_output":"ignored"}`, ""), normal[1], normal[2], normal[3]}},
		{"cached_replay", []string{strings.Replace(normal[0], `"object"`, `"cached":true,"object"`, 1), normal[1], normal[2], normal[3]}},
		{"nonzero_index", []string{strings.Replace(normal[0], `"index":0`, `"index":1`, 1), normal[1], normal[2], normal[3]}},
		{"missing_index", []string{strings.Replace(normal[0], `"index":0,`, "", 1), normal[1], normal[2], normal[3]}},
		{"multiple_choices", []string{`{"object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"x"}},{"index":1,"delta":{"content":"y"}}]}`, normal[1], normal[2], normal[3]}},
		{"unknown_wrapper", []string{strings.Replace(normal[0], "chat.completion.chunk", "unknown", 1), normal[1], normal[2], normal[3]}},
		{"null_choices", []string{normal[0], normal[1], normal[2], `{"object":"chat.completion.chunk","choices":null,"usage":{"completion_tokens":100}}`}},
		{"null_delta", []string{normal[0], normal[1], tpotChunk(`null`, "stop"), normal[3]}},
	}
	for _, token := range []string{"0", "1", "-1", "null"} {
		cases = append(cases, struct {
			name   string
			chunks []string
		}{"invalid_tokens_" + token, []string{normal[0], normal[1], normal[2], tpotUsage(token, `{}`)}})
	}
	cases = append(cases, struct {
		name   string
		chunks []string
	}{"missing_completion_tokens", []string{normal[0], normal[1], normal[2], `{"object":"chat.completion.chunk","choices":[],"usage":{}}`}})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			times := make([]time.Duration, len(tc.chunks))
			for i := range times {
				times[i] = time.Duration(i) * time.Second
			}
			_, ok := tpotFeed(t, tc.chunks, times).value()
			require.False(t, ok)
		})
	}
	t.Run("negative_interval", func(t *testing.T) {
		_, ok := tpotFeed(t, normal, []time.Duration{time.Second, 0, 2 * time.Second, 3 * time.Second}).value()
		require.False(t, ok)
	})
	t.Run("decoder_errors_unchanged", func(t *testing.T) {
		for _, tokens := range []string{`"100"`, "1.5", "9223372036854775808"} {
			raw := []byte(tpotUsage(tokens, `{}`))
			var expected, actual arkmodel.ChatCompletionStreamResponse
			original := &utils.JSONUnmarshaler{}
			want := original.Unmarshal(raw, &expected)
			got := (&tpotUnmarshaler{original: original, timing: &streamingTPOT{}}).Unmarshal(raw, &actual)
			require.Error(t, want)
			require.EqualError(t, got, want.Error())
		}
	})
	t.Run("unknown_decoder_and_finished_stream", func(t *testing.T) {
		for _, stream := range []*utils.ChatCompletionStreamReader{{}, {Unmarshaler: &utils.JSONUnmarshaler{}, IsFinished: true}} {
			require.True(t, newStreamingTPOT(stream, &arkmodel.CreateChatCompletionRequest{}).invalid)
		}
		n := 2
		require.True(t, newStreamingTPOT(&utils.ChatCompletionStreamReader{Unmarshaler: &utils.JSONUnmarshaler{}}, &arkmodel.CreateChatCompletionRequest{N: &n}).invalid)
	})
}

type tpotTransport func(*http.Request) (*http.Response, error)

func (f tpotTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type tpotBody struct {
	io.Reader
	closeFn func() error
	closes  atomic.Int32
}

type tpotErrorReader struct {
	io.Reader
	err error
}

func (r tpotErrorReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		err = r.err
	}
	return n, err
}

func (b *tpotBody) Close() error {
	b.closes.Add(1)
	if b.closeFn != nil {
		return b.closeFn()
	}
	return nil
}

func tpotSDKModel(t *testing.T, name string, chunks []string, closeFn func() error) (*arkModel, *tpotBody) {
	t.Helper()
	body := &tpotBody{Reader: strings.NewReader("data: " + strings.Join(chunks, "\n\ndata: ") + "\n\ndata: [DONE]\n\n"), closeFn: closeFn}
	client := &http.Client{Transport: tpotTransport(func(r *http.Request) (*http.Response, error) {
		var req struct {
			Stream        bool `json:"stream"`
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.True(t, req.Stream)
		require.True(t, req.StreamOptions.IncludeUsage)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body, Request: r}, nil
	})}
	llm, err := NewArkModel(context.Background(), name, &ArkClientConfig{APIKey: "fixture-key", HTTPClient: client})
	require.NoError(t, err)
	return llm.(*arkModel), body
}

func tpotClock() func() time.Time {
	base := time.Now()
	i := 0
	times := []time.Duration{0, 990 * time.Millisecond, 6 * time.Second, 11 * time.Second}
	return func() time.Time {
		j := i
		i++
		if j >= len(times) {
			j = len(times) - 1
		}
		return base.Add(times[j])
	}
}

func tpotSpanValues(spans tracetest.SpanStubs) map[string][]float64 {
	result := map[string][]float64{}
	for _, span := range spans {
		if span.Name != "model_request" {
			continue
		}
		var modelName string
		for _, attr := range span.Attributes {
			if string(attr.Key) == observability.AttrGenAIRequestModel {
				modelName = attr.Value.AsString()
			}
		}
		for _, attr := range span.Attributes {
			if string(attr.Key) == observability.MetricNameStreamingTimePerOutputToken {
				result[modelName] = append(result[modelName], attr.Value.AsFloat64())
			}
		}
	}
	return result
}

// Read the last cumulative snapshot from the standard file exporter.
// No business path is replaced by a direct call to the metric recorder.
func tpotFileSamples(t *testing.T, path string) map[string]struct {
	count uint64
	sum   float64
} {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()
	result := map[string]struct {
		count uint64
		sum   float64
	}{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if x["Name"] == observability.MetricNameStreamingTimePerOutputToken {
				data, ok := x["Data"].(map[string]any)
				if !ok {
					return
				}
				points, _ := data["DataPoints"].([]any)
				for _, v := range points {
					point := v.(map[string]any)
					attrs := point["Attributes"].([]any)
					var name string
					for _, v := range attrs {
						a := v.(map[string]any)
						if a["Key"] == observability.AttrGenAIRequestModel {
							name = a["Value"].(map[string]any)["Value"].(string)
						}
					}
					result[name] = struct {
						count uint64
						sum   float64
					}{uint64(point["Count"].(float64)), point["Sum"].(float64)}
				}
			}
			for _, v := range x {
				walk(v)
			}
		case []any:
			for _, v := range x {
				walk(v)
			}
		}
	}
	decoder := json.NewDecoder(file)
	for {
		var data any
		err := decoder.Decode(&data)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		walk(data)
	}
	return result
}

// REQ-001/004: actual Ark SDK Recv/Close with an isolated in-process HTTP transport.
func TestStreamingTPOTSDKAndTelemetry(t *testing.T) {
	t.Setenv("OTEL_SDK_DISABLED", "false")
	path := filepath.Join(t.TempDir(), "metrics.json")
	enabled := true
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()); otel.SetTracerProvider(previous) })
	require.NoError(t, observability.Init(context.Background(), &configs.ObservabilityConfig{OpenTelemetry: &configs.OpenTelemetryConfig{EnableMetrics: &enabled, File: &configs.FileConfig{Path: path}}}))

	ctx, parent := tp.Tracer("test").Start(context.Background(), "parent")
	cases := []struct {
		name          string
		chunks        []string
		stopAt        int
		cancelAt      int
		closeFail     bool
		cancelOnClose bool
		clockFailure  bool
		want          bool
		wantErr       bool
	}{
		{name: "normal", chunks: tpotNormalChunks(), want: true},
		{name: "no_terminal", chunks: []string{tpotNormalChunks()[0], tpotNormalChunks()[1], tpotNormalChunks()[3]}},
		{name: "early_stop", chunks: tpotNormalChunks(), stopAt: 1},
		{name: "reject_final", chunks: tpotNormalChunks(), stopAt: 3},
		{name: "cancel_before_terminal", chunks: tpotNormalChunks(), cancelAt: 2},
		{name: "cancel_during_close", chunks: tpotNormalChunks(), cancelOnClose: true},
		{name: "close_failure", chunks: tpotNormalChunks(), closeFail: true},
		{name: "recv_error", chunks: []string{tpotNormalChunks()[0], tpotNormalChunks()[1], `{"invalid"`}, wantErr: true},
		{name: "clock_failure", chunks: tpotNormalChunks(), clockFailure: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requestCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			llm, body := tpotSDKModel(t, tc.name, tc.chunks, func() error {
				if tc.cancelOnClose {
					cancel()
				}
				if tc.closeFail {
					return errors.New("fixture close failure")
				}
				return nil
			})
			var responses []*adkmodel.LLMResponse
			var runErr error
			req, err := llm.convertArkRequest(&adkmodel.LLMRequest{})
			require.NoError(t, err)
			clock := tpotClock()
			if tc.clockFailure {
				clock = func() time.Time { panic("fixture clock failure") }
			}
			llm.generateStreamWithClock(requestCtx, req, clock)(func(resp *adkmodel.LLMResponse, err error) bool {
				if err != nil {
					runErr = err
					return true
				}
				responses = append(responses, resp)
				if len(responses) == tc.cancelAt {
					cancel()
				}
				return len(responses) != tc.stopAt
			})
			require.Equal(t, int32(1), body.closes.Load())
			require.Equal(t, tc.wantErr, runErr != nil)
			if tc.wantErr {
				require.Contains(t, runErr.Error(), "ark: stream recv failed")
			}
			if tc.stopAt == 0 && !tc.wantErr {
				require.Len(t, responses, 3)
				require.Equal(t, "hello ", responses[0].Content.Parts[0].Text)
				require.Equal(t, "world", responses[1].Content.Parts[0].Text)
				require.Equal(t, genai.FinishReasonStop, responses[2].FinishReason)
				require.Equal(t, int32(100), responses[2].UsageMetadata.CandidatesTokenCount)
			}
			values := tpotSpanValues(exporter.GetSpans())[tc.name]
			if tc.want {
				require.Equal(t, []float64{0.01}, values)
			} else {
				require.Empty(t, values)
			}
		})
	}
	t.Run("trace_not_sampled_still_records_metric", func(t *testing.T) {
		unsampled := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
		defer func() { require.NoError(t, unsampled.Shutdown(context.Background())) }()
		otel.SetTracerProvider(unsampled)
		defer otel.SetTracerProvider(tp)
		llm, _ := tpotSDKModel(t, "unsampled", tpotNormalChunks(), nil)
		req, err := llm.convertArkRequest(&adkmodel.LLMRequest{})
		require.NoError(t, err)
		for _, err := range llm.generateStreamWithClock(context.Background(), req, tpotClock()) {
			require.NoError(t, err)
		}
		require.Empty(t, tpotSpanValues(exporter.GetSpans())["unsampled"])
	})
	t.Run("reasoning_text_and_tool_results_are_unchanged", func(t *testing.T) {
		chunks := []string{
			tpotChunk(`{"reasoning_content":"r1","content":"t1","tool_calls":[{"index":0,"id":"call-1","function":{"name":"weather","arguments":"{\"city\":"}}]}`, ""),
			tpotChunk(`{"reasoning_content":"r2","content":"t2","tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]}`, "tool_calls"),
			tpotUsage("100", `{"reasoning_tokens":20}`),
		}
		llm, body := tpotSDKModel(t, "mixed-output", chunks, nil)
		req, err := llm.convertArkRequest(&adkmodel.LLMRequest{})
		require.NoError(t, err)
		var responses []*adkmodel.LLMResponse
		for resp, err := range llm.generateStreamWithClock(ctx, req, tpotClock()) {
			require.NoError(t, err)
			responses = append(responses, resp)
		}
		require.Len(t, responses, 5)
		for i, text := range []string{"r1", "t1", "r2", "t2"} {
			require.True(t, responses[i].Partial)
			require.Equal(t, text, responses[i].Content.Parts[0].Text)
			require.Equal(t, i%2 == 0, responses[i].Content.Parts[0].Thought)
		}
		parts := responses[4].Content.Parts
		require.Equal(t, "r1r2", parts[0].Text)
		require.Equal(t, "t1t2", parts[1].Text)
		require.Equal(t, "call-1", parts[2].FunctionCall.ID)
		require.Equal(t, "weather", parts[2].FunctionCall.Name)
		require.Equal(t, map[string]any{"city": "Paris"}, parts[2].FunctionCall.Args)
		require.Equal(t, int32(100), responses[4].UsageMetadata.CandidatesTokenCount)
		require.Equal(t, int32(1), body.closes.Load())
		require.Equal(t, []float64{.01}, tpotSpanValues(exporter.GetSpans())["mixed-output"])
	})
	t.Run("consumer_panic_is_not_recovered", func(t *testing.T) {
		llm, body := tpotSDKModel(t, "consumer-panic", tpotNormalChunks(), nil)
		req, err := llm.convertArkRequest(&adkmodel.LLMRequest{})
		require.NoError(t, err)
		require.PanicsWithValue(t, "fixture consumer panic", func() {
			llm.generateStreamWithClock(ctx, req, tpotClock())(func(*adkmodel.LLMResponse, error) bool { panic("fixture consumer panic") })
		})
		require.Equal(t, int32(1), body.closes.Load())
		require.Empty(t, tpotSpanValues(exporter.GetSpans())["consumer-panic"])
	})
	t.Run("timeout_and_cancellation_errors_are_preserved", func(t *testing.T) {
		for _, originalErr := range []error{context.DeadlineExceeded, context.Canceled} {
			name := "recv-" + originalErr.Error()
			llm, body := tpotSDKModel(t, name, tpotNormalChunks(), nil)
			body.Reader = tpotErrorReader{Reader: strings.NewReader("data: " + strings.Join(tpotNormalChunks()[:2], "\n\ndata: ") + "\n\n"), err: originalErr}
			req, err := llm.convertArkRequest(&adkmodel.LLMRequest{})
			require.NoError(t, err)
			count := 0
			var returned error
			for resp, err := range llm.generateStreamWithClock(ctx, req, tpotClock()) {
				if resp != nil {
					count++
				}
				if err != nil {
					returned = err
				}
			}
			require.Equal(t, 2, count)
			require.ErrorIs(t, returned, originalErr)
			require.Equal(t, int32(1), body.closes.Load())
			require.Empty(t, tpotSpanValues(exporter.GetSpans())[name])
		}
	})
	t.Run("nonstreaming_does_not_record_tpot", func(t *testing.T) {
		client := &http.Client{Transport: tpotTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"model":"nonstream","choices":[{"index":0,"message":{"role":"assistant","content":"hello world"},"finish_reason":"stop"}],"usage":{"completion_tokens":100}}`)), Request: r}, nil
		})}
		llm, err := NewArkModel(ctx, "nonstream", &ArkClientConfig{APIKey: "fixture-key", HTTPClient: client})
		require.NoError(t, err)
		count := 0
		for resp, err := range llm.GenerateContent(ctx, &adkmodel.LLMRequest{}, false) {
			require.NoError(t, err)
			require.Equal(t, "hello world", resp.Content.Parts[0].Text)
			count++
		}
		require.Equal(t, 1, count)
		require.Empty(t, tpotSpanValues(exporter.GetSpans())["nonstream"])
	})
	t.Run("transport_eof_without_done_is_truncated", func(t *testing.T) {
		llm, body := tpotSDKModel(t, "truncated-eof", tpotNormalChunks(), nil)
		body.Reader = strings.NewReader("data: " + strings.Join(tpotNormalChunks(), "\n\ndata: ") + "\n\n")
		req, err := llm.convertArkRequest(&adkmodel.LLMRequest{})
		require.NoError(t, err)
		var responses []*adkmodel.LLMResponse
		for resp, err := range llm.generateStreamWithClock(ctx, req, tpotClock()) {
			require.NoError(t, err)
			responses = append(responses, resp)
		}
		require.Len(t, responses, 3)
		require.Equal(t, genai.FinishReasonStop, responses[2].FinishReason)
		require.Equal(t, int32(1), body.closes.Load())
		require.Empty(t, tpotSpanValues(exporter.GetSpans())["truncated-eof"])
	})
	t.Run("concurrent_calls_do_not_share_timing", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := range 6 {
			name := fmt.Sprintf("concurrent-%d", i)
			llm, _ := tpotSDKModel(t, name, tpotNormalChunks(), nil)
			wg.Go(func() {
				req, err := llm.convertArkRequest(&adkmodel.LLMRequest{})
				if err != nil {
					t.Error(err)
					return
				}
				base := time.Now()
				j := 0
				clock := func() time.Time {
					v := base.Add(time.Duration(j) * time.Duration(i+1) * 99 * time.Millisecond)
					j++
					return v
				}
				for _, err := range llm.generateStreamWithClock(ctx, req, clock) {
					if err != nil {
						t.Error(err)
					}
				}
			})
		}
		wg.Wait()
		values := tpotSpanValues(exporter.GetSpans())
		for i := range 6 {
			require.Equal(t, []float64{float64(i+1) * .001}, values[fmt.Sprintf("concurrent-%d", i)])
		}
	})
	t.Run("visible_auth_retry_is_a_separate_request_span", func(t *testing.T) {
		provider := &rotatingTestAPIKeyProvider{keys: []string{"expired-key", "rotated-key"}}
		var calls atomic.Int32
		body := &tpotBody{Reader: strings.NewReader("data: " + strings.Join(tpotNormalChunks(), "\n\ndata: ") + "\n\ndata: [DONE]\n\n")}
		client := &http.Client{Transport: tpotTransport(func(r *http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"fixture expired","type":"authentication_error","code":"AuthenticationError"}}`)), Request: r}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body, Request: r}, nil
		})}
		llm, err := NewArkModel(ctx, "visible-retry", &ArkClientConfig{APIKeyProvider: provider, HTTPClient: client})
		require.NoError(t, err)
		am := llm.(*arkModel)
		req, err := am.convertArkRequest(&adkmodel.LLMRequest{})
		require.NoError(t, err)
		for _, err := range am.generateStreamWithClock(ctx, req, tpotClock()) {
			require.NoError(t, err)
		}
		require.Equal(t, int32(2), calls.Load())
		require.Equal(t, int32(1), body.closes.Load())
		require.Equal(t, []float64{.01}, tpotSpanValues(exporter.GetSpans())["visible-retry"])
		spanCount := 0
		for _, span := range exporter.GetSpans() {
			for _, attr := range span.Attributes {
				if string(attr.Key) == observability.AttrGenAIRequestModel && attr.Value.AsString() == "visible-retry" {
					spanCount++
				}
			}
		}
		require.Equal(t, 2, spanCount)
	})
	t.Run("sdk_hidden_retry_remains_one_request_span", func(t *testing.T) {
		var calls atomic.Int32
		body := &tpotBody{Reader: strings.NewReader("data: " + strings.Join(tpotNormalChunks(), "\n\ndata: ") + "\n\ndata: [DONE]\n\n")}
		client := &http.Client{Transport: tpotTransport(func(r *http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				return &http.Response{StatusCode: http.StatusInternalServerError, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"fixture retryable","code":"InternalServiceError"}}`)), Request: r}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body, Request: r}, nil
		})}
		am := &arkModel{name: "hidden-retry", config: &ArkClientConfig{}, client: arkruntime.NewClientWithApiKey("fixture-key", arkruntime.WithHTTPClient(client), arkruntime.WithRetryTimes(1))}
		req, err := am.convertArkRequest(&adkmodel.LLMRequest{})
		require.NoError(t, err)
		for _, err := range am.generateStreamWithClock(ctx, req, tpotClock()) {
			require.NoError(t, err)
		}
		require.Equal(t, int32(2), calls.Load())
		require.Equal(t, []float64{.01}, tpotSpanValues(exporter.GetSpans())["hidden-retry"])
		spanCount := 0
		for _, span := range exporter.GetSpans() {
			for _, attr := range span.Attributes {
				if string(attr.Key) == observability.AttrGenAIRequestModel && attr.Value.AsString() == "hidden-retry" {
					spanCount++
				}
			}
		}
		require.Equal(t, 1, spanCount)
	})
	parent.End()
	for _, span := range exporter.GetSpans() {
		if span.Name == "model_request" {
			require.Equal(t, trace.SpanKindClient, span.SpanKind)
			require.Equal(t, parent.SpanContext().SpanID(), span.Parent.SpanID())
		}
		if span.Name == "parent" {
			for _, attr := range span.Attributes {
				require.NotEqual(t, observability.MetricNameStreamingTimePerOutputToken, string(attr.Key))
			}
		}
	}
	require.NoError(t, observability.ForceFlush(context.Background()))
	samples := tpotFileSamples(t, path)
	for _, name := range []string{"normal", "unsampled", "visible-retry", "hidden-retry", "mixed-output"} {
		require.Equal(t, uint64(1), samples[name].count)
		require.Equal(t, .01, samples[name].sum)
	}
	for _, tc := range cases {
		if !tc.want {
			require.Zero(t, samples[tc.name].count)
		}
	}
	for _, name := range []string{"consumer-panic", "recv-" + context.DeadlineExceeded.Error(), "recv-" + context.Canceled.Error(), "nonstream", "truncated-eof"} {
		require.Zero(t, samples[name].count)
	}
	for i := range 6 {
		name := fmt.Sprintf("concurrent-%d", i)
		require.Equal(t, uint64(1), samples[name].count)
		require.Equal(t, float64(i+1)*.001, samples[name].sum)
	}
}

// Initialization is process-wide, so the disabled configuration needs a fresh process.
func TestStreamingTPOTSDKMetricsDisabled(t *testing.T) {
	if os.Getenv("VEADK_TPOT_DISABLED_HELPER") != "1" {
		binary, err := os.Executable()
		require.NoError(t, err)
		cmd := exec.Command(binary, "-test.run=^TestStreamingTPOTSDKMetricsDisabled$")
		cmd.Env = append(os.Environ(), "VEADK_TPOT_DISABLED_HELPER=1", "OTEL_SDK_DISABLED=false")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}
	path := filepath.Join(t.TempDir(), "trace-only.json")
	disabled := false
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { require.NoError(t, tp.Shutdown(context.Background())) }()
	require.NoError(t, observability.Init(context.Background(), &configs.ObservabilityConfig{OpenTelemetry: &configs.OpenTelemetryConfig{EnableMetrics: &disabled, File: &configs.FileConfig{Path: path}}}))
	llm, _ := tpotSDKModel(t, "metrics-disabled", tpotNormalChunks(), nil)
	req, err := llm.convertArkRequest(&adkmodel.LLMRequest{})
	require.NoError(t, err)
	for _, err := range llm.generateStreamWithClock(context.Background(), req, tpotClock()) {
		require.NoError(t, err)
	}
	require.Equal(t, []float64{.01}, tpotSpanValues(exporter.GetSpans())["metrics-disabled"])
	require.NoError(t, observability.ForceFlush(context.Background()))
	require.Empty(t, tpotFileSamples(t, path))
}

type tpotPanicSpan struct{ trace.Span }

func (s tpotPanicSpan) SetAttributes(...attribute.KeyValue) { panic("fixture telemetry failure") }

func TestStreamingTPOTTelemetryFailureAndOnce(t *testing.T) {
	timing := tpotFeed(t, tpotNormalChunks(), []time.Duration{0, 990 * time.Millisecond, time.Second, 2 * time.Second})
	require.NotPanics(t, func() { timing.record(context.Background(), tpotPanicSpan{}, "fixture") })
	require.True(t, timing.recorded)
	require.NotPanics(t, func() { timing.record(context.Background(), nil, "fixture") })
	require.False(t, safeStreamingTelemetry(func() { panic("fixture clock failure") }))
}
