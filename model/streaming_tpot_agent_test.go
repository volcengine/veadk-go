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

package model_test

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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"github.com/volcengine/veadk-go/v2/configs"
	"github.com/volcengine/veadk-go/v2/observability"
	verunner "github.com/volcengine/veadk-go/v2/runner"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/adk/v2/agent"
	adkllm "google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/plugin"
	adkrunner "google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

type toolLifecycleTransport func(*http.Request) (*http.Response, error)

func (f toolLifecycleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Each read supplies only one SSE frame, preventing SDK buffering from hiding
// the controlled inter-delta delay. This uses the actual official SDK decoder.
type toolLifecycleReader struct {
	frames                 []string
	index, offset, delayAt int
}

func (r *toolLifecycleReader) Read(p []byte) (int, error) {
	if r.index == len(r.frames) {
		return 0, io.EOF
	}
	if r.offset == 0 && r.index == r.delayAt {
		time.Sleep(100 * time.Millisecond)
	}
	n := copy(p, r.frames[r.index][r.offset:])
	r.offset += n
	if r.offset == len(r.frames[r.index]) {
		r.index++
		r.offset = 0
	}
	return n, nil
}

func lifecycleFrames(toolRequest bool) *toolLifecycleReader {
	chunk := func(delta, finish string) string {
		return "data: " + `{"object":"chat.completion.chunk","model":"fixture-model","choices":[{"index":0,"delta":` + delta + `,"finish_reason":"` + finish + `"}]}` + "\n\n"
	}
	frames := []string{
		chunk(`{"content":"hello "}`, ""),
		chunk(`{"content":"done"}`, ""),
		chunk(`{}`, "stop"),
	}
	delayAt := 1
	if toolRequest {
		frames = []string{
			chunk(`{"role":"assistant"}`, ""),
			chunk(`{"tool_calls":[{"index":0,"id":"call-add","type":"function","function":{"name":"fixture_add","arguments":"{\"a\":2,"}}]}`, ""),
			chunk(`{"tool_calls":[{"index":0,"function":{"arguments":"\"b\":3}"}}]}`, ""),
			chunk(`{}`, "tool_calls"),
		}
		delayAt = 2
	}
	frames = append(frames, "data: "+`{"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":100,"total_tokens":105}}`+"\n\n", "data: [DONE]\n\n")
	return &toolLifecycleReader{frames: frames, delayAt: delayAt}
}

func lifecycleMetric(t *testing.T, path string) (count uint64, sum float64) {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if x["Name"] == observability.MetricNameStreamingTimePerOutputToken {
				// Keep the last cumulative snapshot emitted by the file exporter.
				count, sum = 0, 0
				for _, item := range x["Data"].(map[string]any)["DataPoints"].([]any) {
					point := item.(map[string]any)
					count += uint64(point["Count"].(float64))
					sum += point["Sum"].(float64)
				}
				return
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	decoder := json.NewDecoder(f)
	for {
		var value any
		err := decoder.Decode(&value)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		walk(value)
	}
	return
}

// D-01 / REQ-001/002/004: regression for the actual ADK tool-turn lifecycle.
// Process isolation avoids ordering dependencies on observability.Init's singleton.
func TestStreamingTPOTAgentToolLifecycle(t *testing.T) {
	mode := os.Getenv("VEADK_TPOT_TOOL_HELPER")
	if mode == "" {
		for _, scenario := range []string{"normal", "reject-tool-final", "cancel-tool", "tool-error"} {
			t.Run(scenario, func(t *testing.T) {
				binary, err := os.Executable()
				require.NoError(t, err)
				cmd := exec.Command(binary, "-test.run=^TestStreamingTPOTAgentToolLifecycle$")
				cmd.Env = append(os.Environ(), "VEADK_TPOT_TOOL_HELPER="+scenario, "OTEL_SDK_DISABLED=false")
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
				for _, line := range strings.Split(string(output), "\n") {
					if strings.HasPrefix(line, "TPOT_LIFECYCLE_RESULT ") {
						t.Log(line)
					}
				}
			})
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "telemetry.json")
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer func() { require.NoError(t, tp.Shutdown(context.Background())) }()
	enabled := true
	require.NoError(t, observability.Init(ctx, &configs.ObservabilityConfig{OpenTelemetry: &configs.OpenTelemetryConfig{EnableMetrics: &enabled, File: &configs.FileConfig{Path: path}}}))

	requests, toolCalls := 0, 0
	var toolElapsed time.Duration
	client := &http.Client{Transport: toolLifecycleTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if requests > 2 {
			return nil, errors.New("unexpected third model request")
		}
		var request map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Equal(t, true, request["stream"])
		require.Equal(t, true, request["stream_options"].(map[string]any)["include_usage"])
		if requests == 2 {
			found := false
			for _, item := range request["messages"].([]any) {
				message := item.(map[string]any)
				if message["role"] == "tool" {
					require.Equal(t, "call-add", message["tool_call_id"])
					if mode == "tool-error" {
						require.Contains(t, message["content"], "fixture tool error")
					} else {
						require.Contains(t, message["content"], "5")
					}
					found = true
				}
			}
			require.True(t, found, "second request must contain the accepted tool result")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(lifecycleFrames(requests == 1)), Request: r}, nil
	})}
	add, err := functiontool.New(functiontool.Config{Name: "fixture_add", Description: "add integers"}, func(_ agent.Context, args struct {
		A int `json:"a"`
		B int `json:"b"`
	}) (map[string]any, error) {
		toolCalls++
		require.Equal(t, 2, args.A)
		require.Equal(t, 3, args.B)
		if mode == "tool-error" {
			return nil, errors.New("fixture tool error")
		}
		start := time.Now()
		time.Sleep(500 * time.Millisecond)
		toolElapsed = time.Since(start)
		if mode == "cancel-tool" {
			cancel()
		}
		return map[string]any{"result": args.A + args.B}, nil
	})
	require.NoError(t, err)
	a, err := veagent.New(&veagent.Config{
		Config:    adkllm.Config{Name: "fixture-agent", Tools: []tool.Tool{add}},
		ModelName: "fixture-model", ModelProvider: "ark", ModelAPIKey: "fixture-key", ModelHTTPClient: client,
	})
	require.NoError(t, err)
	r, err := verunner.New(verunner.Config{
		AppName: "tpot-lifecycle", Agent: a, AutoCreateSession: true,
		PluginConfig: adkrunner.PluginConfig{Plugins: []*plugin.Plugin{observability.NewPlugin(observability.WithEnableMetrics(true))}},
	})
	require.NoError(t, err)
	var runErr error
	toolResult, textResult := false, false
	for event, err := range r.Run(ctx, "fixture-user", "fixture-session", verunner.UserText("add"), agent.RunConfig{StreamingMode: agent.StreamingModeSSE}) {
		if err != nil {
			runErr = err
			break
		}
		if event.Content == nil {
			continue
		}
		reject := false
		for _, part := range event.Content.Parts {
			if mode == "reject-tool-final" && part.FunctionCall != nil {
				reject = true
			}
			if part.FunctionResponse != nil {
				require.Equal(t, "call-add", part.FunctionResponse.ID)
				if mode == "tool-error" {
					require.Contains(t, part.FunctionResponse.Response["error"], "fixture tool error")
				} else {
					require.EqualValues(t, 5, part.FunctionResponse.Response["result"])
				}
				toolResult = true
			}
			if !event.Partial && part.Text == "hello done" {
				textResult = true
			}
		}
		if reject {
			break
		}
	}
	require.NoError(t, observability.ForceFlush(context.Background()))
	var values []float64
	for _, span := range exporter.GetSpans() {
		for _, attr := range span.Attributes {
			if string(attr.Key) == observability.MetricNameStreamingTimePerOutputToken {
				require.Equal(t, "model_request", span.Name)
				values = append(values, attr.Value.AsFloat64())
			}
		}
	}
	count, sum := lifecycleMetric(t, path)
	if mode == "normal" || mode == "tool-error" {
		require.NoError(t, runErr)
		require.Equal(t, 2, requests)
		require.Equal(t, 1, toolCalls)
		require.True(t, toolResult)
		require.True(t, textResult)
		if mode == "normal" {
			require.GreaterOrEqual(t, toolElapsed, 500*time.Millisecond)
		}
		require.Len(t, values, 2, "both model requests must record their own TPOT")
		for _, value := range values {
			require.GreaterOrEqual(t, value, .08/99)
			require.Less(t, value, .3/99, "500ms tool execution must not enter TPOT")
		}
		require.Equal(t, uint64(2), count)
		require.InDelta(t, values[0]+values[1], sum, 1e-12)
	} else {
		require.Equal(t, 1, requests)
		require.Empty(t, values)
		require.Zero(t, count)
		if mode == "reject-tool-final" {
			require.NoError(t, runErr)
			require.Zero(t, toolCalls)
		}
		if mode == "cancel-tool" {
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.Equal(t, 1, toolCalls)
		}
	}
	fmt.Printf("TPOT_LIFECYCLE_RESULT mode=%s requests=%d samples=%d values=%v tool_elapsed=%s\n", mode, requests, count, values, toolElapsed)
}
