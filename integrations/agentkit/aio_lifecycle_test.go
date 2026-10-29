// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package agentkit

import (
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session/database"
	"gorm.io/driver/sqlite"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/adk/v2/session"
)

func TestAIOLifecyclePauseResumeRecreateAndDelete(t *testing.T) {
	type physical struct{ id, user, status string }
	var mu sync.Mutex
	var instances []physical
	var actions []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		action := r.URL.Query().Get("Action")
		mu.Lock()
		defer mu.Unlock()
		actions = append(actions, action)
		result := map[string]any{}
		find := func(id string) *physical {
			for i := range instances {
				if instances[i].id == id {
					return &instances[i]
				}
			}
			return nil
		}
		id, _ := request["SessionId"].(string)
		switch action {
		case "ListSessions":
			items := make([]any, 0, len(instances))
			for _, item := range instances {
				items = append(items, map[string]any{"SessionId": item.id, "UserSessionId": item.user, "Status": item.status})
			}
			result["SessionInfos"] = items
		case "CreateSession":
			id = fmt.Sprintf("physical-%d", len(instances)+1)
			user, _ := request["UserSessionId"].(string)
			instances = append(instances, physical{id: id, user: user, status: "Ready"})
			result["SessionId"] = id
		case "GetSession":
			item := find(id)
			if item == nil || item.status == "Deleted" {
				_ = json.NewEncoder(w).Encode(map[string]any{"ResponseMetadata": map[string]any{"Error": map[string]any{"Code": "InvalidResource.NotFound"}}})
				return
			}
			result = map[string]any{"SessionId": item.id, "UserSessionId": item.user, "Status": item.status, "Endpoint": "https://sandbox.example"}
		case "PauseSession":
			find(id).status = "Paused"
		case "ResumeSession":
			find(id).status = "Ready"
		case "DeleteSession":
			find(id).status = "Deleted"
		default:
			t.Errorf("unexpected action %s", action)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Result": result})
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
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
	m, err := NewAIOLifecycle(AIOLifecycleConfig{SessionService: store, SkillSpaceIDs: []string{"ss-demo"}, IdleTimeout: time.Second, PausedRetention: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	created, err := m.Create(ctx, "app", "user", "logical", map[string]any{"business": "value"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID() != "logical" {
		t.Fatalf("ADK session ID = %q", created.ID())
	}
	if len(actions) != 0 {
		t.Fatalf("logical creation called control plane: %v", actions)
	}
	if err := m.WithSessionContext(ctx, "app", "user", "logical", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Fatalf("chat called control plane: %v", actions)
	}
	// Each tool starts with its own stale invocation snapshot. The lifecycle
	// must serialize first use and durably publish a single physical binding.
	runTools := func() {
		t.Helper()
		err := m.WithSessionContext(ctx, "app", "user", "logical", func(work context.Context) error {
			var wg sync.WaitGroup
			for range 8 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					toolCtx := &lazyToolContext{base: work, state: &aioMapState{values: map[string]any{}}}
					endpoint, err := EnsureAIOToolEndpoint(toolCtx, []string{"ss-demo"})
					if err != nil || endpoint != "https://sandbox.example" {
						t.Errorf("lazy tool endpoint = %q, %v", endpoint, err)
						return
					}
					if binding, err := bindingFromState(toolCtx.State()); err != nil || binding == nil {
						t.Errorf("current tool cannot see binding: %v", err)
					}
				}()
			}
			wg.Wait()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	runTools()
	if len(instances) != 1 {
		t.Fatalf("parallel first use created %d sandboxes", len(instances))
	}
	reloaded, err := GetADKSession(ctx, store, "app", "user", "logical")
	if err != nil {
		t.Fatal(err)
	}
	if binding, err := bindingFromState(reloaded.State()); err != nil || binding == nil || binding.SessionID != "physical-1" {
		t.Fatalf("binding not persisted: %+v, %v", binding, err)
	}
	entry := m.track("app", "user", "logical")
	entry.lastActivity = time.Now().Add(-2 * time.Second)
	m.Sweep(ctx)
	if entry.pausedAt.IsZero() || instances[0].status != "Paused" {
		t.Fatalf("sandbox did not pause: %+v", instances)
	}
	beforeChat := len(actions)
	idleSince := entry.lastActivity
	if err := m.WithSessionContext(ctx, "app", "user", "logical", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(actions) != beforeChat || instances[0].status != "Paused" || !entry.lastActivity.Equal(idleSince) {
		t.Fatal("chat resumed or extended sandbox lifetime")
	}
	runTools()
	if instances[0].status != "Ready" {
		t.Fatal("tool did not resume sandbox")
	}
	entry.lastActivity = time.Now().Add(-2 * time.Second)
	m.Sweep(ctx)
	entry.pausedAt = time.Now().Add(-2 * time.Second)
	m.Sweep(ctx)
	if instances[0].status != "Deleted" || !entry.deleted {
		t.Fatalf("expired sandbox not deleted: %+v", instances)
	}
	runTools()
	if len(instances) != 2 || instances[1].status != "Ready" {
		t.Fatalf("tool did not recreate sandbox: %+v", instances)
	}
	reloaded, err = GetADKSession(ctx, store, "app", "user", "logical")
	if err != nil {
		t.Fatal(err)
	}
	if binding, err := bindingFromState(reloaded.State()); err != nil || binding == nil || binding.SessionID != "physical-2" {
		t.Fatalf("replacement binding not persisted: %+v, %v", binding, err)
	}
	if err := m.Delete(ctx, "app", "user", "logical"); err != nil || instances[1].status != "Deleted" {
		t.Fatalf("explicit delete failed: %v, %+v", err, instances)
	}
	if _, err := store.Get(ctx, &session.GetRequest{AppName: "app", UserID: "user", SessionID: "logical"}); err == nil {
		t.Fatal("ADK session survived explicit deletion")
	}
	if len(actions) == 0 {
		t.Fatal("control plane was not called")
	}
}

func TestAIOLifecycleDurationConfiguration(t *testing.T) {
	t.Setenv("VEADK_AIO_IDLE_TIMEOUT", "7m")
	t.Setenv("VEADK_AIO_PAUSED_RETENTION", "3h")
	t.Setenv("VEADK_AIO_SCAN_INTERVAL", "12s")
	m, err := NewAIOLifecycle(AIOLifecycleConfig{SessionService: session.InMemoryService()})
	if err != nil {
		t.Fatal(err)
	}
	if m.cfg.IdleTimeout != 7*time.Minute || m.cfg.PausedRetention != 3*time.Hour || m.cfg.ScanInterval != 12*time.Second {
		t.Fatalf("environment durations were not applied: %+v", m.cfg)
	}
	if m.sessionTTLSeconds() < int((m.cfg.IdleTimeout+5*time.Minute)/time.Second) {
		t.Fatal("AgentKit TTL expires before configured idle timeout")
	}
	m, err = NewAIOLifecycle(AIOLifecycleConfig{SessionService: session.InMemoryService(), IdleTimeout: 2 * time.Minute})
	if err != nil || m.cfg.IdleTimeout != 2*time.Minute {
		t.Fatalf("explicit timeout did not take priority: %+v, %v", m, err)
	}
	t.Setenv("VEADK_AIO_PAUSED_RETENTION", "invalid")
	if _, err := NewAIOLifecycle(AIOLifecycleConfig{SessionService: session.InMemoryService()}); err == nil {
		t.Fatal("invalid retention was accepted")
	}
}

// lazyToolContext deliberately has an independent state snapshot, as database
// session services do; updating another loaded session cannot update it.
type lazyToolContext struct {
	agent.Context
	base  context.Context
	state session.State
}

func (c *lazyToolContext) Deadline() (time.Time, bool)          { return c.base.Deadline() }
func (c *lazyToolContext) Done() <-chan struct{}                { return c.base.Done() }
func (c *lazyToolContext) Err() error                           { return c.base.Err() }
func (c *lazyToolContext) Value(key any) any                    { return c.base.Value(key) }
func (c *lazyToolContext) AppName() string                      { return "app" }
func (c *lazyToolContext) UserID() string                       { return "user" }
func (c *lazyToolContext) SessionID() string                    { return "logical" }
func (c *lazyToolContext) State() session.State                 { return c.state }
func (c *lazyToolContext) ReadonlyState() session.ReadonlyState { return c.state }

func TestAIOUnusedSessionDoesNotNeedSandboxConfiguration(t *testing.T) {
	t.Setenv("AGENTKIT_TOOL_ID", "")
	store := session.InMemoryService()
	m, err := NewAIOLifecycle(AIOLifecycleConfig{SessionService: store, IdleTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := m.Create(ctx, "app", "user", "logical", nil); err != nil {
		t.Fatal(err)
	}
	entry := m.track("app", "user", "logical")
	entry.lastActivity = time.Now().Add(-time.Hour)
	m.Sweep(ctx)
	if !entry.pausedAt.IsZero() {
		t.Fatal("unallocated sandbox was paused")
	}
	if err := m.WithSessionContext(ctx, "app", "user", "logical", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "app", "user", "logical"); err != nil {
		t.Fatal(err)
	}
}
