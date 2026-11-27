// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package skilltool

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	agentkit "github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"github.com/volcengine/veadk-go/v2/skills"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
)

type aioTestState struct{ values map[string]any }

func (s *aioTestState) Get(key string) (any, error) {
	value, ok := s.values[key]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}
	return value, nil
}
func (s *aioTestState) Set(key string, value any) error { s.values[key] = value; return nil }
func (s *aioTestState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for k, v := range s.values {
			if !yield(k, v) {
				return
			}
		}
	}
}

type aioTestContext struct {
	agent.Context
	state         *aioTestState
	app, user, id string
}

func (c *aioTestContext) Deadline() (time.Time, bool)          { return time.Time{}, false }
func (c *aioTestContext) Done() <-chan struct{}                { return nil }
func (c *aioTestContext) Err() error                           { return nil }
func (c *aioTestContext) Value(any) any                        { return nil }
func (c *aioTestContext) AppName() string                      { return c.app }
func (c *aioTestContext) UserID() string                       { return c.user }
func (c *aioTestContext) SessionID() string                    { return c.id }
func (c *aioTestContext) ReadonlyState() session.ReadonlyState { return c.state }
func (c *aioTestContext) State() session.State                 { return c.state }

func TestAIOSandboxBindingAndTools(t *testing.T) {
	var mu sync.Mutex
	var creates, closes int
	var physicalID string
	var createdSkillSpace string
	var catalogCalls int
	var writes []map[string]any
	var edits []map[string]any
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respond := func(data any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(data)
		}
		if r.URL.Path == "/" {
			switch r.URL.Query().Get("Action") {
			case "ListSessions":
				respond(map[string]any{"Result": map[string]any{"SessionInfos": []any{}}})
			case "CreateSession":
				var request map[string]any
				_ = json.NewDecoder(r.Body).Decode(&request)
				mu.Lock()
				creates++
				physicalID, _ = request["UserSessionId"].(string)
				if envs, ok := request["Envs"].([]any); ok && len(envs) == 1 {
					if env, ok := envs[0].(map[string]any); ok && env["Key"] == "SKILL_SPACE_ID" {
						createdSkillSpace, _ = env["Value"].(string)
					}
				}
				mu.Unlock()
				respond(map[string]any{"Result": map[string]any{"SessionId": "remote-1", "Status": "Ready"}})
			case "GetSession":
				var request map[string]any
				_ = json.NewDecoder(r.Body).Decode(&request)
				mu.Lock()
				userID := physicalID
				mu.Unlock()
				remoteID, _ := request["SessionId"].(string)
				if remoteID == "another-remote" {
					userID = "veadk-aio-another-session"
				}
				respond(map[string]any{"Result": map[string]any{"SessionId": remoteID, "UserSessionId": userID, "Status": "Ready", "Endpoint": endpoint}})
			default:
				t.Errorf("unexpected control action %s", r.URL.Query().Get("Action"))
				w.WriteHeader(404)
			}
			return
		}
		if r.URL.Query().Get("token") != "keep" {
			t.Error("endpoint query parameter was lost")
		}
		var body map[string]any
		if r.Method == "POST" {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		switch r.URL.Path {
		case "/sandbox/v1/skills":
			catalogCalls++
			t.Error("sandbox Skill catalog must not be queried")
			w.WriteHeader(http.StatusNotFound)
		case "/sandbox/v1/file/read":
			file, _ := body["file"].(string)
			content := "first\nsecond\n"
			if strings.HasSuffix(file, "SKILL.md") {
				content = "# Demo"
			}
			respond(map[string]any{"success": true, "data": map[string]any{"content": content}})
		case "/sandbox/v1/file/write":
			mu.Lock()
			writes = append(writes, body)
			mu.Unlock()
			respond(map[string]any{"success": true, "data": map[string]any{}})
		case "/sandbox/v1/file/str_replace_editor":
			mu.Lock()
			edits = append(edits, body)
			mu.Unlock()
			respond(map[string]any{"success": true, "data": map[string]any{"output": "ok"}})
		case "/sandbox/v1/bash/sessions/create":
			respond(map[string]any{"success": true, "data": map[string]any{}})
		case "/sandbox/v1/bash/exec":
			respond(map[string]any{"success": true, "data": map[string]any{"status": "completed", "exit_code": 0, "stdout": "hello\n"}})
		case "/sandbox/v1/bash/kill":
			respond(map[string]any{"success": true, "data": map[string]any{}})
		default:
			if strings.HasSuffix(r.URL.Path, "/close") {
				mu.Lock()
				closes++
				mu.Unlock()
				respond(map[string]any{"success": true, "data": map[string]any{}})
				return
			}
			t.Errorf("unexpected data path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	endpoint = server.URL + "/sandbox?token=keep"
	u, _ := url.Parse(server.URL)
	t.Setenv("AGENTKIT_TOOL_ID", "tool-1")
	t.Setenv("VOLCENGINE_ACCESS_KEY", "test-ak")
	t.Setenv("VOLCENGINE_SECRET_KEY", "test-sk")
	t.Setenv("AGENTKIT_TOOL_SCHEME", "http")
	t.Setenv("AGENTKIT_TOOL_HOST", u.Host)
	t.Setenv("AGENTKIT_TOOL_SERVICE_CODE", "agentkit")
	t.Setenv("AGENTKIT_TOOL_REGION", "cn-beijing")
	ctx := &aioTestContext{state: &aioTestState{values: map[string]any{}}, app: "app", user: "user", id: "session"}
	s, err := NewAIOSandboxToolset([]skills.RemoteSkill{{Name: "demo", Description: "Demo Skill", Version: "v1"}}, "ss-demo")
	if err != nil {
		t.Fatal(err)
	}
	tools, err := s.Tools(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 4 || tools[0].Name() != "bash" || tools[1].Name() != "read_file" || tools[2].Name() != "write_file" || tools[3].Name() != "edit_file" {
		t.Fatalf("AIO tools = %v, want bash/read_file/write_file/edit_file", tools)
	}
	if !strings.Contains(s.Instruction(), "<path>/workspace/skills/demo</path>") || strings.Contains(s.Instruction(), "load_skill") {
		t.Fatalf("AIO instruction does not describe direct Skill reads: %q", s.Instruction())
	}
	if catalogCalls != 0 {
		t.Fatalf("sandbox catalog calls = %d, want 0", catalogCalls)
	}
	if creates != 0 {
		t.Fatal("constructing or listing tools allocated a sandbox")
	}
	loaded, err := s.readFile(ctx, readFileArgs{FilePath: "/workspace/skills/demo/SKILL.md", Offset: 1, Limit: 0})
	if err != nil || !strings.Contains(loaded["content"].(string), "# Demo") {
		t.Fatalf("read_file SKILL.md = %v, %v", loaded, err)
	}
	if _, err := agentkit.EnsureAIOSandboxWithSkills(ctx, []string{"ss-demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := agentkit.EnsureAIOSandboxWithSkills(ctx, []string{"ss-demo"}); err != nil {
		t.Fatal(err)
	}
	if creates != 1 || createdSkillSpace != "ss-demo" {
		t.Fatalf("CreateSession calls = %d, SKILL_SPACE_ID = %q", creates, createdSkillSpace)
	}
	if _, err := agentkit.EnsureAIOSandboxWithSkills(ctx, []string{"ss-other"}); err == nil {
		t.Fatal("binding was reused with a different Skill Space")
	}
	other := &aioTestContext{state: ctx.state, app: "app", user: "another", id: "session"}
	if _, err := agentkit.ResolveAIOEndpoint(other); err == nil {
		t.Fatal("cross-user binding was accepted")
	}
	if _, err := s.writeFile(ctx, writeFileArgs{FilePath: "hello.txt", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	read, err := s.readFile(ctx, readFileArgs{FilePath: "hello.txt", Offset: 2, Limit: 1})
	if err != nil || !strings.Contains(fmt.Sprint(read["content"]), "2|first") {
		t.Fatalf("read_file = %v, %v", read, err)
	}
	if _, err := s.editFile(ctx, editFileArgs{FilePath: "hello.txt", OldString: "hello", NewString: "hi", ReplaceAll: true}); err != nil {
		t.Fatal(err)
	}
	result, err := s.bash(ctx, bashArgs{Command: "echo hello"})
	if err != nil || result["status"] != "success" || result["stdout"] != "hello\n" {
		t.Fatalf("bash = %v, %v", result, err)
	}
	if len(writes) != 1 || writes[0]["file"] != "/workspace/hello.txt" {
		t.Fatalf("writes = %v", writes)
	}
	if len(edits) != 1 || edits[0]["replace_mode"] != "ALL" {
		t.Fatalf("edits = %v", edits)
	}
	if closes != 1 {
		t.Fatalf("shell close calls = %d", closes)
	}
	bound := ctx.state.values[agentkit.AIOSandboxStateKey].(map[string]string)
	forged := map[string]string{"tool_id": bound["tool_id"], "identity": bound["identity"], "session_id": "another-remote"}
	ctx.state.values[agentkit.AIOSandboxStateKey] = forged
	if _, err := agentkit.ResolveAIOEndpoint(ctx); err == nil {
		t.Fatal("forged binding to another physical sandbox was accepted")
	}
}

func TestAIOBashFailureAndCancellationCleanup(t *testing.T) {
	for _, status := range []string{"nonzero", "timeout"} {
		t.Run(status, func(t *testing.T) {
			var mu sync.Mutex
			var kills, closes int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				respond := func(data map[string]any) {
					_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/create"):
					respond(map[string]any{})
				case strings.HasSuffix(r.URL.Path, "/exec"):
					if status == "nonzero" {
						respond(map[string]any{"status": "completed", "exit_code": 7})
					} else {
						respond(map[string]any{"status": "running", "command_id": "command-1"})
					}
				case strings.HasSuffix(r.URL.Path, "/output"):
					time.Sleep(100 * time.Millisecond)
					respond(map[string]any{"command": map[string]any{"status": "running", "command_id": "command-1"}})
				case strings.HasSuffix(r.URL.Path, "/kill"):
					mu.Lock()
					kills++
					mu.Unlock()
					respond(map[string]any{})
				case strings.HasSuffix(r.URL.Path, "/close"):
					mu.Lock()
					closes++
					mu.Unlock()
					respond(map[string]any{})
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			client := &aioClient{base: u, client: aioHTTPClient}
			duration := time.Second
			if status == "timeout" {
				duration = 25 * time.Millisecond
			}
			result, err := client.bash(context.Background(), "test", duration)
			mu.Lock()
			defer mu.Unlock()
			if closes != 1 {
				t.Fatalf("close calls = %d, want 1", closes)
			}
			if status == "nonzero" && (err != nil || result["status"] != "error" || result["exit_code"] != 7 || kills != 0) {
				t.Fatalf("nonzero result=%v err=%v kills=%d", result, err, kills)
			}
			if status == "timeout" && (err == nil || kills != 1) {
				t.Fatalf("timeout result=%v err=%v kills=%d", result, err, kills)
			}
		})
	}
}

func TestAIOClientDoesNotFollowRedirectWithCredential(t *testing.T) {
	var reached bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	client := &aioClient{base: u, client: aioHTTPClient, headers: http.Header{"X-Api-Key": {"secret"}}}
	if _, err := client.call(context.Background(), "POST", "/v1/file/write", map[string]any{"file": "/workspace/test"}); err == nil {
		t.Fatal("redirect was accepted")
	}
	if reached {
		t.Fatal("credential was forwarded to redirect target")
	}
}
