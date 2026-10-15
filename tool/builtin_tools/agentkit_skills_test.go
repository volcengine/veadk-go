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

package builtin_tools

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/volcengine/veadk-go/v2/common"
	"github.com/volcengine/veadk-go/v2/integrations/ve_sign"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
)

type skillsTestState map[string]any

func (s skillsTestState) Get(key string) (any, error) {
	value, ok := s[key]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}
	return value, nil
}

func (s skillsTestState) Set(key string, value any) error {
	s[key] = value
	return nil
}

func (s skillsTestState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for key, value := range s {
			if !yield(key, value) {
				return
			}
		}
	}
}

type skillsTestContext struct {
	agent.Context
	base  context.Context
	state skillsTestState
}

func newSkillsTestContext() *skillsTestContext {
	return &skillsTestContext{base: context.Background(), state: skillsTestState{}}
}

func (c *skillsTestContext) Deadline() (time.Time, bool) { return c.base.Deadline() }
func (c *skillsTestContext) Done() <-chan struct{}       { return c.base.Done() }
func (c *skillsTestContext) Err() error                  { return c.base.Err() }
func (c *skillsTestContext) Value(key any) any           { return c.base.Value(key) }
func (c *skillsTestContext) UserContent() *genai.Content { return nil }
func (c *skillsTestContext) InvocationID() string        { return "invocation-1" }
func (c *skillsTestContext) AgentName() string           { return "agent" }
func (c *skillsTestContext) ReadonlyState() session.ReadonlyState {
	return c.state
}
func (c *skillsTestContext) UserID() string              { return "user" }
func (c *skillsTestContext) AppName() string             { return "app" }
func (c *skillsTestContext) SessionID() string           { return "session" }
func (c *skillsTestContext) Branch() string              { return "" }
func (c *skillsTestContext) Session() session.Session    { return nil }
func (c *skillsTestContext) Agent() agent.Agent          { return nil }
func (c *skillsTestContext) Memory() agent.Memory        { return nil }
func (c *skillsTestContext) RunConfig() *agent.RunConfig { return nil }
func (c *skillsTestContext) EndInvocation()              {}
func (c *skillsTestContext) Ended() bool                 { return false }
func (c *skillsTestContext) WithContext(context.Context) agent.InvocationContext {
	return c
}
func (c *skillsTestContext) WithICDelta(*agent.InvocationContextDelta) agent.InvocationContext {
	return c
}
func (c *skillsTestContext) Artifacts() agent.Artifacts     { return nil }
func (c *skillsTestContext) State() session.State           { return c.state }
func (c *skillsTestContext) FunctionCallID() string         { return "call-1" }
func (c *skillsTestContext) Actions() *session.EventActions { return &session.EventActions{} }
func (c *skillsTestContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return nil
}
func (c *skillsTestContext) RequestConfirmation(string, any) error { return nil }
func (c *skillsTestContext) ResumedInput(string) (any, bool)       { return nil, false }
func (c *skillsTestContext) Path() string                          { return "" }
func (c *skillsTestContext) RunID() string                         { return "" }
func (c *skillsTestContext) SubScheduler() agent.DynamicSubScheduler {
	return nil
}
func (c *skillsTestContext) WithAgentContext(context.Context) agent.Context {
	return c
}
func (c *skillsTestContext) WithAgentTimeout(time.Duration) (agent.Context, context.CancelFunc) {
	return c, func() {}
}
func (c *skillsTestContext) WithAgentCancel() (agent.Context, context.CancelFunc) {
	return c, func() {}
}
func (c *skillsTestContext) OutputForAncestors() []string { return nil }
func (c *skillsTestContext) WithDelta(*agent.CommonContextDelta) agent.Context {
	return c
}
func (c *skillsTestContext) SearchMemory(context.Context, string) (*memory.SearchResponse, error) {
	return nil, nil
}
func (c *skillsTestContext) ArtifactsService() artifact.Service { return nil }

func withSkillsToolEnv(t *testing.T) {
	t.Helper()
	t.Setenv(common.AGENTKIT_TOOL_ID_SKILLS, "skills-tool")
	t.Setenv(common.VOLCENGINE_ACCESS_KEY, "ak")
	t.Setenv(common.VOLCENGINE_SECRET_KEY, "sk")
}

func TestExecuteSkillsWaitsForCompletedA2ATask(t *testing.T) {
	withSkillsToolEnv(t)
	t.Setenv("SKILL_SPACE_POLICY", `{"mode":"allow","ids":["skill-1"]}`)

	var requests []ve_sign.VeRequest
	previousDo := agentkitDoRequest
	t.Cleanup(func() { agentkitDoRequest = previousDo })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/sandbox/a2a" {
			t.Fatalf("A2A path = %q, want /sandbox/a2a", got)
		}
		if got := r.Header.Get("inbound_auth"); got != "bearer-token" {
			t.Fatalf("inbound_auth = %q, want bearer-token", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		switch payload["method"] {
		case "message/send":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"kind": "task",
					"id":   "task-1",
					"status": map[string]any{
						"state": "working",
					},
				},
			})
		case "tasks/get":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"kind": "task",
					"id":   "task-1",
					"status": map[string]any{
						"state": "completed",
					},
					"artifacts": []any{
						map[string]any{
							"parts": []any{
								map[string]any{"kind": "text", "text": "done"},
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected method %v", payload["method"])
		}
	}))
	defer server.Close()

	agentkitDoRequest = func(req ve_sign.VeRequest) ([]byte, error) {
		requests = append(requests, req)
		switch req.Action {
		case "ListSessions":
			return []byte(`{"Result":{"SessionInfos":[]}}`), nil
		case "CreateSession":
			return []byte(`{"Result":{"SessionId":"session-1","Status":"Creating"}}`), nil
		case "GetSession":
			return []byte(`{"Result":{"SessionId":"session-1","Status":"Ready","Endpoint":"` + server.URL + `/sandbox"}}`), nil
		default:
			t.Fatalf("unexpected action %s", req.Action)
			return nil, nil
		}
	}

	ctx := newSkillsTestContext()
	ctx.state["inbound_auth"] = "bearer-token"
	result, err := executeSkillsHandler(ctx, executeSkillsArgs{WorkflowPrompt: "hello", Timeout: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := result["result"]; got != "done" {
		t.Fatalf("result = %v, want done", got)
	}
	if got := len(requests); got != 3 {
		t.Fatalf("AgentKit request count = %d, want 3", got)
	}
	if got := requests[1].Body.(map[string]any)["UserSessionId"]; got != "agent_user_session" {
		t.Fatalf("UserSessionId = %v", got)
	}
	envs, ok := requests[1].Body.(map[string]any)["Envs"].([]map[string]string)
	if !ok || len(envs) != 1 || envs[0]["Key"] != "SKILL_SPACE_POLICY" {
		t.Fatalf("CreateSession Envs = %#v", requests[1].Body.(map[string]any)["Envs"])
	}
}

func TestInvokeAndPollSkillReuseReadySession(t *testing.T) {
	withSkillsToolEnv(t)

	previousDo := agentkitDoRequest
	t.Cleanup(func() { agentkitDoRequest = previousDo })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		method := payload["method"]
		task := map[string]any{
			"kind": "task",
			"id":   "task-2",
			"status": map[string]any{
				"state": "completed",
			},
		}
		if method == "message/send" {
			task["status"] = map[string]any{"state": "submitted"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": task})
	}))
	defer server.Close()

	agentkitDoRequest = func(req ve_sign.VeRequest) ([]byte, error) {
		if req.Action != "ListSessions" {
			t.Fatalf("unexpected action %s", req.Action)
		}
		return []byte(`{"Result":{"SessionInfos":[{"SessionId":"session-2","UserSessionId":"agent_user_session","Status":"Ready","Endpoint":"` + server.URL + `","CreatedAt":"2026-01-01T00:00:00Z"}]}}`), nil
	}

	ctx := newSkillsTestContext()
	invoked, err := invokeSkillHandler(ctx, invokeSkillArgs{WorkflowPrompt: "hello", Timeout: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := a2aTaskState(invoked); got != "submitted" {
		t.Fatalf("invoke state = %q, want submitted", got)
	}

	polled, err := pollSkillHandler(ctx, pollSkillArgs{TaskID: "task-2", Timeout: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := a2aTaskState(polled); got != "completed" {
		t.Fatalf("poll state = %q, want completed", got)
	}
}

func TestResolveSkillsToolIDFallback(t *testing.T) {
	t.Setenv(common.AGENTKIT_TOOL_ID, "generic-tool")
	_ = os.Unsetenv(common.AGENTKIT_TOOL_ID_SKILLS)
	if got := resolveSkillsToolID(); got != "generic-tool" {
		t.Fatalf("resolveSkillsToolID() = %q, want generic-tool", got)
	}
}

func TestExecuteSkillsRejectsUnsupportedEnvVars(t *testing.T) {
	ctx := newSkillsTestContext()
	_, err := executeSkillsHandler(ctx, executeSkillsArgs{
		WorkflowPrompt: "hello",
		EnvVars:        map[string]string{"A": "B"},
	})
	if err == nil || !strings.Contains(err.Error(), "env_vars is not supported") {
		t.Fatalf("executeSkillsHandler error = %v, want env_vars unsupported", err)
	}
}

func TestPollSkillRequiresTaskID(t *testing.T) {
	ctx := newSkillsTestContext()
	_, err := pollSkillHandler(ctx, pollSkillArgs{})
	if err == nil || !strings.Contains(err.Error(), "task_id is required") {
		t.Fatalf("pollSkillHandler error = %v, want missing task_id", err)
	}
}

func TestExecuteSkillsReturnsFailedTaskError(t *testing.T) {
	withSkillsToolEnv(t)
	previousDo := agentkitDoRequest
	t.Cleanup(func() { agentkitDoRequest = previousDo })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		state := "failed"
		if payload["method"] == "message/send" {
			state = "working"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"kind":   "task",
				"id":     "task-failed",
				"status": map[string]any{"state": state},
			},
		})
	}))
	defer server.Close()
	stubReadyAgentkitSession(t, server.URL)

	_, err := executeSkillsHandler(newSkillsTestContext(), executeSkillsArgs{
		WorkflowPrompt: "hello",
		Timeout:        3,
	})
	if err == nil || !strings.Contains(err.Error(), "ended with state failed") {
		t.Fatalf("executeSkillsHandler error = %v, want failed task", err)
	}
}

func TestInvokeSkillReturnsJSONRPCError(t *testing.T) {
	withSkillsToolEnv(t)
	previousDo := agentkitDoRequest
	t.Cleanup(func() { agentkitDoRequest = previousDo })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    -32000,
				"message": "sandbox rejected request",
			},
		})
	}))
	defer server.Close()
	stubReadyAgentkitSession(t, server.URL)

	_, err := invokeSkillHandler(newSkillsTestContext(), invokeSkillArgs{
		WorkflowPrompt: "hello",
		Timeout:        3,
	})
	if err == nil || !strings.Contains(err.Error(), "sandbox rejected request") {
		t.Fatalf("invokeSkillHandler error = %v, want JSON-RPC error", err)
	}
}

func TestA2AURLKeepsExistingA2APath(t *testing.T) {
	got, err := a2aJSONRPCURL("https://example.test/base/a2a")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.test/base/a2a" {
		t.Fatalf("a2aJSONRPCURL = %q", got)
	}
}

func TestAgentkitEndpointConfigUsesPythonCloudDefaults(t *testing.T) {
	t.Run("volcengine", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", "")
		t.Setenv("REGION", "")
		t.Setenv(common.AGENTKIT_TOOL_REGION, "")
		t.Setenv(common.AGENTKIT_TOOL_HOST, "")

		service, region, host, scheme := agentkitEndpointConfig()
		if service != common.DEFAULT_AGENTKIT_TOOL_SERVICE_CODE {
			t.Fatalf("service = %q, want %q", service, common.DEFAULT_AGENTKIT_TOOL_SERVICE_CODE)
		}
		if region != common.DEFAULT_AGENTKIT_TOOL_REGION {
			t.Fatalf("region = %q, want %q", region, common.DEFAULT_AGENTKIT_TOOL_REGION)
		}
		if host != "open.volcengineapi.com" {
			t.Fatalf("host = %q, want open.volcengineapi.com", host)
		}
		if scheme != DefaultScheme {
			t.Fatalf("scheme = %q, want %q", scheme, DefaultScheme)
		}
	})

	t.Run("byteplus", func(t *testing.T) {
		t.Setenv("CLOUD_PROVIDER", "byteplus")
		t.Setenv("REGION", "")
		t.Setenv(common.AGENTKIT_TOOL_REGION, "")
		t.Setenv(common.AGENTKIT_TOOL_HOST, "")

		service, region, host, scheme := agentkitEndpointConfig()
		if service != common.DEFAULT_AGENTKIT_TOOL_SERVICE_CODE {
			t.Fatalf("service = %q, want %q", service, common.DEFAULT_AGENTKIT_TOOL_SERVICE_CODE)
		}
		if region != "ap-southeast-1" {
			t.Fatalf("region = %q, want ap-southeast-1", region)
		}
		if host != "agentkit.ap-southeast-1.byteplusapi.com" {
			t.Fatalf("host = %q, want agentkit.ap-southeast-1.byteplusapi.com", host)
		}
		if scheme != DefaultScheme {
			t.Fatalf("scheme = %q, want %q", scheme, DefaultScheme)
		}
	})
}

func stubReadyAgentkitSession(t *testing.T, endpoint string) {
	t.Helper()
	agentkitDoRequest = func(req ve_sign.VeRequest) ([]byte, error) {
		switch req.Action {
		case "ListSessions":
			return []byte(`{"Result":{"SessionInfos":[{"SessionId":"session-ready","UserSessionId":"agent_user_session","Status":"Ready","Endpoint":"` + endpoint + `","CreatedAt":"2026-01-01T00:00:00Z"}]}}`), nil
		default:
			t.Fatalf("unexpected action %s", req.Action)
			return nil, nil
		}
	}
}
