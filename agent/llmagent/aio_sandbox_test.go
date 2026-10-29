// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package llmagent

import (
	"context"
	"encoding/json"
	"github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"google.golang.org/adk/v2/session/database"
	"gorm.io/driver/sqlite"
	"iter"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/volcengine/veadk-go/v2/auth/veauth"
	"github.com/volcengine/veadk-go/v2/skills"
	"github.com/volcengine/veadk-go/v2/tool/skilltool"
	"google.golang.org/adk/v2/agent"
	adkllmagent "google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"
	adkrunner "google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type aioCountingModel struct{ calls atomic.Int32 }

func (*aioCountingModel) Name() string { return "counting-model" }
func (m *aioCountingModel) GenerateContent(context.Context, *adkmodel.LLMRequest, bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	m.calls.Add(1)
	return func(func(*adkmodel.LLMResponse, error) bool) {}
}

func TestAIOSkillsModeDiscoversAtConstruction(t *testing.T) {
	var calls atomic.Int32
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["SkillSpaceId"] != "ss-test" {
			t.Errorf("Skill Space request: %v, %v", body, err)
		}
		_, _ = w.Write([]byte(`{"Result":{"Items":[{"Name":"demo","Description":"Demo Skill","BucketName":"bucket","TosPath":"demo.zip","Version":"v1"}]}}`))
	}))
	defer service.Close()
	cfg := &Config{
		Config:     adkllmagent.Config{Name: "aio-agent", Model: testLLM{}},
		SkillsMode: "aio_sandbox",
		Skills:     []string{"ss-test"},
		SkillSpaceSourceConfig: skills.RemoteSourceConfig{
			Endpoint: service.URL,
			Credentials: veauth.NewRoleCredentialSource(veauth.RoleCredentialSourceConfig{
				AccessKeyID: "test-ak", SecretAccessKey: "test-sk",
			}),
		},
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New() did not discover Skill Space: %v", err)
	}
	if mode, ids, known := SkillsModeFor(a); !known || mode != "aio_sandbox" || len(ids) != 1 || ids[0] != "ss-test" {
		t.Fatalf("constructed Agent mode = %q, ids=%v, known=%t", mode, ids, known)
	}
	if calls.Load() != 1 || !strings.Contains(cfg.Instruction, "Demo Skill") || !strings.Contains(cfg.Instruction, "<name>demo</name>") || !strings.Contains(cfg.Instruction, "<path>/workspace/skills/demo</path>") {
		t.Fatalf("Agent prompt missing startup metadata: calls=%d instruction=%q", calls.Load(), cfg.Instruction)
	}
	if len(cfg.Toolsets) != 1 || len(cfg.BeforeAgentCallbacks) != 0 {
		t.Fatalf("AIO wiring: toolsets=%d callbacks=%d", len(cfg.Toolsets), len(cfg.BeforeAgentCallbacks))
	}
	if _, ok := cfg.Toolsets[0].(*skilltool.AIOSandboxToolset); !ok {
		t.Fatalf("toolset is %T, want AIOSandboxToolset", cfg.Toolsets[0])
	}
	empty := &Config{Config: adkllmagent.Config{Name: "empty-aio-agent", Model: testLLM{}}, SkillsMode: "aio_sandbox"}
	if _, err := New(empty); err != nil || len(empty.Toolsets) != 1 {
		t.Fatalf("empty AIO Skills must still install tools: %v", err)
	}
}

func TestLocalAgentExposesLocalMode(t *testing.T) {
	a, err := New(&Config{Config: adkllmagent.Config{Name: "local-agent", Model: testLLM{}}})
	if err != nil {
		t.Fatal(err)
	}
	if mode, _, known := SkillsModeFor(a); !known || mode != "local" {
		t.Fatalf("constructed local Agent mode = %q, known=%t", mode, known)
	}
}

func TestSkillsModeRejectsUnknownAndSandboxPaths(t *testing.T) {
	for _, cfg := range []*Config{
		{Config: adkllmagent.Config{Name: "unknown", Model: testLLM{}}, SkillsMode: "skills_sandbox"},
		{Config: adkllmagent.Config{Name: "absolute", Model: testLLM{}}, SkillsMode: "aio_sandbox", Skills: []string{"/workspace/skills"}},
		{Config: adkllmagent.Config{Name: "relative", Model: testLLM{}}, SkillsMode: "aio_sandbox", Skills: []string{"relative/skills"}},
	} {
		if _, err := New(cfg); err == nil {
			t.Fatalf("New(%q, %v) accepted invalid mode or path", cfg.SkillsMode, cfg.Skills)
		}
	}
}

func TestAIOChatDoesNotRequireSandbox(t *testing.T) {
	t.Setenv("AGENTKIT_TOOL_ID", "")
	model := &aioCountingModel{}
	a, err := New(&Config{Config: adkllmagent.Config{Name: "aio-failure", Model: model}, SkillsMode: "aio_sandbox"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := adkrunner.New(adkrunner.Config{AppName: "test", Agent: a, SessionService: session.InMemoryService(), AutoCreateSession: true})
	if err != nil {
		t.Fatal(err)
	}
	var sawError bool
	for _, err := range r.Run(context.Background(), "user", "session", genai.NewContentFromText("hello", genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			sawError = true
		}
	}
	if sawError {
		t.Fatal("chat unexpectedly required a sandbox")
	}
	if got := model.calls.Load(); got != 1 {
		t.Fatalf("model calls without sandbox = %d, want 1", got)
	}
}

type lazyFileModel struct{ calls int }

func (*lazyFileModel) Name() string { return "lazy-file-model" }
func (m *lazyFileModel) GenerateContent(context.Context, *adkmodel.LLMRequest, bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	m.calls++
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		content := genai.NewContentFromText("done", genai.RoleModel)
		if m.calls%2 == 1 {
			content = &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "read_file", Args: map[string]any{"file_path": "/workspace/demo.txt"}}}}}
		}
		yield(&adkmodel.LLMResponse{Content: content}, nil)
	}
}

func TestAIOLazyToolPersistsThroughDatabaseRunner(t *testing.T) {
	var creates, reads atomic.Int32
	var physicalUser, endpoint string
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		if r.URL.Path == "/v1/file/read" {
			reads.Add(1)
			_, _ = w.Write([]byte(`{"success":true,"data":{"content":"hello"}}`))
			return
		}
		result := map[string]any{}
		switch r.URL.Query().Get("Action") {
		case "ListSessions":
			result["SessionInfos"] = []any{}
		case "CreateSession":
			creates.Add(1)
			physicalUser, _ = request["UserSessionId"].(string)
			result["SessionId"] = "physical"
		case "GetSession":
			result = map[string]any{"SessionId": "physical", "UserSessionId": physicalUser, "Status": "Ready", "Endpoint": endpoint}
		default:
			t.Errorf("unexpected sandbox action: %s", r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Result": result})
	}))
	defer control.Close()
	endpoint = control.URL
	u, _ := url.Parse(control.URL)
	t.Setenv("AGENTKIT_TOOL_ID", "tool-test")
	t.Setenv("VOLCENGINE_ACCESS_KEY", "test-ak")
	t.Setenv("VOLCENGINE_SECRET_KEY", "test-sk")
	t.Setenv("AGENTKIT_TOOL_SCHEME", "http")
	t.Setenv("AGENTKIT_TOOL_HOST", u.Host)
	t.Setenv("AGENTKIT_TOOL_SERVICE_CODE", "agentkit")
	t.Setenv("AGENTKIT_TOOL_REGION", "cn-beijing")
	store, err := database.NewSessionService(sqlite.Open(filepath.Join(t.TempDir(), "sessions.db")))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(store); err != nil {
		t.Fatal(err)
	}
	manager, err := agentkit.NewAIOLifecycle(agentkit.AIOLifecycleConfig{SessionService: store})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := manager.Create(ctx, "test", "user", "session", nil); err != nil {
		t.Fatal(err)
	}
	model := &lazyFileModel{}
	root, err := New(&Config{Config: adkllmagent.Config{Name: "files", Model: model}, SkillsMode: "aio_sandbox"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := adkrunner.New(adkrunner.Config{AppName: "test", Agent: root, SessionService: store})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := manager.WithSessionContext(ctx, "test", "user", "session", func(work context.Context) error {
			for _, err := range r.Run(work, "user", "session", genai.NewContentFromText("read the file", genai.RoleUser), agent.RunConfig{}) {
				if err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if creates.Load() != 1 || reads.Load() != 2 || model.calls != 4 {
		t.Fatalf("creates=%d reads=%d model calls=%d", creates.Load(), reads.Load(), model.calls)
	}
	s, err := store.Get(ctx, &session.GetRequest{AppName: "test", UserID: "user", SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session.State().Get(agentkit.AIOSandboxStateKey); err != nil {
		t.Fatal("tool binding was not persisted", err)
	}
}
