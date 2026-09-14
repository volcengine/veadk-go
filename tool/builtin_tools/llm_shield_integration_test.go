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

package builtin_tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/volcengine/veadk-go/apps"
	"github.com/volcengine/veadk-go/apps/agentkit_server_app"
	vemodel "github.com/volcengine/veadk-go/model"
	"github.com/volcengine/veadk-go/tool/builtin_tools"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
)

// Runs the real server/model adapter in a separate Go executable with only
// synthetic environment values. No credentials or dotenv files are inherited.
func TestLLMShieldServerProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM process contract")
	}
	var mu sync.Mutex
	modelCalls := map[string]int{}
	shieldCalls := map[string]int{}
	shield := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Message struct{ Content string } }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		prompt := body.Message.Content
		mu.Lock()
		shieldCalls[prompt]++
		mu.Unlock()
		switch {
		case strings.HasPrefix(prompt, "blocked"):
			_, _ = io.WriteString(w, `{"Result":{"Decision":{"DecisionType":2},"RiskInfo":{"Risks":[{"Category":104}]}}}`)
		case strings.HasPrefix(prompt, "unavailable"):
			w.WriteHeader(500)
			_, _ = io.WriteString(w, "secret-response-marker")
		default:
			_, _ = io.WriteString(w, `{"Result":{"Decision":{"DecisionType":1}}}`)
		}
	}))
	defer shield.Close()
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream   bool
			Messages []struct {
				Role    string
				Content string
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		prompt := ""
		for _, m := range body.Messages {
			if m.Role == "user" {
				prompt = m.Content
			}
		}
		mu.Lock()
		modelCalls[prompt]++
		mu.Unlock()
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: ")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "completion", "object": "chat.completion.chunk", "model": "test-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": "model-result:" + prompt}, "finish_reason": "stop"}}})
			_, _ = io.WriteString(w, "\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "completion", "object": "chat.completion", "model": "test-model", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "model-result:" + prompt}, "finish_reason": "stop"}}})
	}))
	defer modelServer.Close()
	for _, mode := range []string{"enabled", "disabled", "fail-close"} {
		t.Run(mode, func(t *testing.T) {
			// A second launch verifies clean shutdown/restart without retained state.
			for launch := range 2 {
				address, stop := startShieldProcess(t, mode, shield.URL, modelServer.URL)
				transport := http.DefaultTransport.(*http.Transport).Clone()
				defer transport.CloseIdleConnections()
				client, err := a2aclient.NewFromCard(t.Context(), &a2a.AgentCard{URL: address + "/rpc", PreferredTransport: a2a.TransportProtocolJSONRPC}, a2aclient.WithConfig(a2aclient.Config{Polling: true}), a2aclient.WithJSONRPCTransport(&http.Client{Transport: transport}))
				if err != nil {
					t.Fatal(err)
				}
				var wg sync.WaitGroup
				for i := range 8 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						kind := []string{"allowed", "blocked", "unavailable"}[i%3]
						prompt := fmt.Sprintf("%s-%s-%d-%d", kind, mode, launch, i)
						ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
						defer cancel()
						message := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: prompt})
						message.ContextID = prompt
						result, err := client.SendMessage(ctx, &a2a.MessageSendParams{Message: message})
						if err != nil {
							t.Error(err)
							return
						}
						var task *a2a.Task
						for ctx.Err() == nil {
							task, err = client.GetTask(ctx, &a2a.TaskQueryParams{ID: result.TaskInfo().TaskID})
							if err != nil {
								t.Error(err)
								return
							}
							if task.Status.State == a2a.TaskStateCompleted || task.Status.State == a2a.TaskStateFailed {
								break
							}
							time.Sleep(10 * time.Millisecond)
						}
						if ctx.Err() != nil {
							t.Error("task did not finish")
							return
						}
						text := ""
						for _, artifact := range task.Artifacts {
							for _, part := range artifact.Parts {
								if p, ok := part.(a2a.TextPart); ok {
									text += p.Text
								}
							}
						}
						blocked := mode != "disabled" && kind == "blocked"
						failed := mode == "fail-close" && kind == "unavailable"
						if failed {
							if task.Status.State != a2a.TaskStateFailed {
								t.Errorf("%s state=%s", prompt, task.Status.State)
							}
						} else if task.Status.State != a2a.TaskStateCompleted {
							t.Errorf("%s state=%s", prompt, task.Status.State)
						} else if blocked {
							if !strings.Contains(text, "blocked due to: Prompt Injection") {
								t.Errorf("%s did not return a block", prompt)
							}
						} else if !strings.Contains(text, "model-result:"+prompt) {
							t.Errorf("%s response crossed sessions: %q", prompt, text)
						}
						mu.Lock()
						models, shields := modelCalls[prompt], shieldCalls[prompt]
						mu.Unlock()
						wantModels := 1
						if blocked || failed {
							wantModels = 0
						}
						wantShields := 1
						if mode == "disabled" {
							wantShields = 0
						}
						if models != wantModels || shields != wantShields {
							t.Errorf("%s model=%d shield=%d; want %d/%d", prompt, models, shields, wantModels, wantShields)
						}
					}()
				}
				wg.Wait()
				transport.CloseIdleConnections()
				if err := client.Destroy(); err != nil {
					t.Error(err)
				}
				stop()
			}
		})
	}
}

func startShieldProcess(t *testing.T, mode, shieldURL, modelURL string) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	command := exec.Command(os.Args[0], "-test.run=^TestLLMShieldProcessHelper$")
	command.Dir = t.TempDir()
	enabled := "true"
	if mode == "disabled" {
		enabled = "false"
	}
	command.Env = []string{
		"VEADK_SHIELD_PROCESS_HELPER=1", "VEADK_SHIELD_PROCESS_PORT=" + strconv.Itoa(port), "VEADK_SHIELD_PROCESS_MODE=" + mode,
		"ENABLE_LLM_SHIELD=" + enabled, "TOOL_LLM_SHIELD_APP_ID=test-app", "TOOL_LLM_SHIELD_API_KEY=synthetic-key-marker",
		"TOOL_LLM_SHIELD_URL=" + shieldURL, "VEADK_SHIELD_MODEL_URL=" + modelURL, "LOGGING_LEVEL=DEBUG",
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = command.Process.Signal(syscall.SIGTERM)
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("server exit: %v; %s", err, output.String())
				}
			case <-time.After(5 * time.Second):
				_ = command.Process.Kill()
				<-done
				t.Error("server shutdown exceeded bound")
			}
			logs := output.String()
			for _, marker := range []string{"synthetic-key-marker", "secret-response-marker", "blocked-", "allowed-", "unavailable-"} {
				if strings.Contains(logs, marker) {
					t.Errorf("sensitive marker %q appeared in debug logs", marker)
				}
			}
		})
	}
	t.Cleanup(stop)
	address := "http://127.0.0.1:" + strconv.Itoa(port)
	client := http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(address + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return address, stop
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server did not become ready")
	return "", stop
}

func TestLLMShieldProcessHelper(t *testing.T) {
	if os.Getenv("VEADK_SHIELD_PROCESS_HELPER") != "1" {
		return
	}
	port, err := strconv.Atoi(os.Getenv("VEADK_SHIELD_PROCESS_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	plugins, err := builtin_tools.LLMShieldPluginsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("VEADK_SHIELD_PROCESS_MODE") == "fail-close" {
		p, err := builtin_tools.NewLLMShieldPlugin(builtin_tools.LLMShieldConfig{Enabled: true, AppID: "test-app", APIKey: "synthetic-key-marker", URL: os.Getenv("TOOL_LLM_SHIELD_URL"), FailurePolicy: builtin_tools.LLMShieldFailClose})
		if err != nil {
			t.Fatal(err)
		}
		plugins[0] = p
	}
	m, err := vemodel.NewOpenAIModel(context.Background(), "test-model", &vemodel.ClientConfig{APIKey: "synthetic-model-key", BaseURL: os.Getenv("VEADK_SHIELD_MODEL_URL") + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := llmagent.New(llmagent.Config{Name: "shield_contract", Model: m})
	if err != nil {
		t.Fatal(err)
	}
	cfg := apps.DefaultApiConfig().SetHost("127.0.0.1").SetPort(port).SetA2APath("/rpc").SetWebUIEnabled(false)
	app := agentkit_server_app.NewAgentkitServerApp(cfg)
	if err := app.Run(context.Background(), &apps.RunConfig{AgentLoader: agent.NewSingleLoader(a), SessionService: session.InMemoryService(), PluginConfig: runner.PluginConfig{Plugins: plugins}, DisableObservability: true}); err != nil {
		t.Fatal(err)
	}
}
