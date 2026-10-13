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

package runner

import (
	"context"
	"iter"
	"testing"

	vememory "github.com/volcengine/veadk-go/v2/memory"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type recordingMemoryService struct {
	sessions []session.Session
	policies []*vememory.MemoryAutoSavePolicy
}

func (m *recordingMemoryService) AddSessionToMemory(_ context.Context, s session.Session) error {
	m.sessions = append(m.sessions, s)
	m.policies = append(m.policies, nil)
	return nil
}

func (m *recordingMemoryService) AddSessionToMemoryWithPolicy(_ context.Context, s session.Session, policy *vememory.MemoryAutoSavePolicy) error {
	m.sessions = append(m.sessions, s)
	m.policies = append(m.policies, policy)
	return nil
}

func (m *recordingMemoryService) SearchMemory(context.Context, *memory.SearchRequest) (*memory.SearchResponse, error) {
	return &memory.SearchResponse{}, nil
}

func newNoopAgent(t *testing.T) agent.Agent {
	t.Helper()
	a, err := agent.New(agent.Config{
		Name: "noop",
		Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(func(*session.Event, error) bool) {}
		},
	})
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}
	return a
}

func newEchoAgent(t *testing.T) agent.Agent {
	t.Helper()
	a, err := agent.New(agent.Config{
		Name: "echo",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				event := session.NewEvent(ctx, ctx.InvocationID())
				event.Author = "echo"
				event.LLMResponse = model.LLMResponse{
					Content: genai.NewContentFromText("pong", genai.RoleModel),
				}
				yield(event, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}
	return a
}

func TestNormalizeConfigUsesMemoryAliases(t *testing.T) {
	shortTerm := session.InMemoryService()
	longTerm := &recordingMemoryService{}
	normalized, err := NormalizeConfig(Config{
		Agent:           newNoopAgent(t),
		ShortTermMemory: shortTerm,
		LongTermMemory:  longTerm,
		AutoSaveSession: true,
	})
	if err != nil {
		t.Fatalf("NormalizeConfig() error = %v", err)
	}
	if normalized.AppName != defaultAppName {
		t.Fatalf("AppName = %q, want %q", normalized.AppName, defaultAppName)
	}
	if normalized.SessionService != shortTerm {
		t.Fatal("SessionService did not use ShortTermMemory")
	}
	if normalized.MemoryService != longTerm {
		t.Fatal("MemoryService did not use LongTermMemory")
	}
	if len(normalized.PluginConfig.Plugins) != 1 || normalized.PluginConfig.Plugins[0].Name() != autoSaveMemoryPluginName {
		t.Fatalf("auto save plugin not configured: %+v", normalized.PluginConfig.Plugins)
	}
}

func TestSaveSessionToLongTermMemoryUsesPolicyExtension(t *testing.T) {
	ctx := context.Background()
	sessionService := session.InMemoryService()
	memoryService := &recordingMemoryService{}
	createResp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName:   "app",
		UserID:    "user",
		SessionID: "session",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	err = sessionService.AppendEvent(ctx, createResp.Session, &session.Event{
		LLMResponse: model.LLMResponse{
			Content: genai.NewContentFromText("remember this", genai.RoleUser),
		},
		Author: genai.RoleUser,
	})
	if err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	policy := &vememory.MemoryAutoSavePolicy{Preset: vememory.MemoryAutoSaveAll}
	if err := SaveSessionToLongTermMemory(ctx, sessionService, memoryService, "app", "user", "session", policy); err != nil {
		t.Fatalf("SaveSessionToLongTermMemory() error = %v", err)
	}
	if len(memoryService.sessions) != 1 {
		t.Fatalf("saved sessions = %d, want 1", len(memoryService.sessions))
	}
	if memoryService.policies[0] != policy {
		t.Fatal("policy was not forwarded to policy-aware memory service")
	}
}

func TestAutoSaveMemoryPluginRunsAfterSessionEventsArePersisted(t *testing.T) {
	ctx := context.Background()
	memoryService := &recordingMemoryService{}
	r, err := New(Config{
		AppName:           "app",
		Agent:             newEchoAgent(t),
		SessionService:    session.InMemoryService(),
		MemoryService:     memoryService,
		AutoCreateSession: true,
		AutoSaveSession:   true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for _, wantSavedEvents := range []int{2, 2} {
		for _, err := range r.Run(ctx, "user", "session", genai.NewContentFromText("ping", genai.RoleUser), agent.RunConfig{}) {
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
		}
		if len(memoryService.sessions) == 0 {
			t.Fatal("auto-save did not save any session")
		}
		saved := memoryService.sessions[len(memoryService.sessions)-1]
		if got := saved.Events().Len(); got != wantSavedEvents {
			t.Fatalf("saved event count = %d, want %d", got, wantSavedEvents)
		}
	}
	if len(memoryService.sessions) != 2 {
		t.Fatalf("saved sessions = %d, want 2 incremental saves", len(memoryService.sessions))
	}
}
