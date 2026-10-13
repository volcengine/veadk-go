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

package memory

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/v2/memory/long_term_memory_backends"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type mockMemoryServiceImpl struct {
	memory.Service
}

type recordingLongTermBackend struct {
	events []string
}

func (b *recordingLongTermBackend) SaveMemory(_ context.Context, _ string, eventList []string) error {
	b.events = append(b.events, eventList...)
	return nil
}

func (b *recordingLongTermBackend) SearchMemory(context.Context, string, string, int) ([]*long_term_memory_backends.MemItem, error) {
	return nil, nil
}

func TestNewLongTermMemory(t *testing.T) {
	tests := []struct {
		name        string
		backend     LongTermBackendType
		config      interface{}
		setupMock   func()
		wantErr     bool
		expectedErr string
	}{
		{
			name:    "default config (local)",
			backend: "",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "local backend explicit",
			backend: BackendLongTermLocal,
			config:  nil,
			wantErr: false,
		},
		{
			name:    "viking backend default config",
			backend: BackendLongTermViking,
			config:  nil,
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewVikingDbMemoryBackend).Return(nil, nil).Build()
				mockey.Mock(long_term_memory_backends.LongTermMemoryFactory).Return(&mockMemoryServiceImpl{}).Build()
			},
			wantErr: false,
		},
		{
			name:    "viking backend valid config",
			backend: BackendLongTermViking,
			config:  &long_term_memory_backends.VikingDbMemoryConfig{},
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewVikingDbMemoryBackend).Return(nil, nil).Build()
				mockey.Mock(long_term_memory_backends.LongTermMemoryFactory).Return(&mockMemoryServiceImpl{}).Build()
			},
			wantErr: false,
		},
		{
			name:    "viking backend invalid config type",
			backend: BackendLongTermViking,
			config:  "invalid",
			wantErr: true,
		},
		{
			name:    "viking backend constructor error",
			backend: BackendLongTermViking,
			config:  nil,
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewVikingDbMemoryBackend).Return(nil, errors.New("init error")).Build()
			},
			wantErr: true,
		},
		{
			name:    "mem0 backend default config",
			backend: BackendLongTermMem0,
			config:  nil,
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewMem0MemoryBackend).Return(nil, nil).Build()
				mockey.Mock(long_term_memory_backends.LongTermMemoryFactory).Return(&mockMemoryServiceImpl{}).Build()
			},
			wantErr: false,
		},
		{
			name:    "mem0 backend valid config",
			backend: BackendLongTermMem0,
			config:  &long_term_memory_backends.Mem0MemoryConfig{},
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewMem0MemoryBackend).Return(nil, nil).Build()
				mockey.Mock(long_term_memory_backends.LongTermMemoryFactory).Return(&mockMemoryServiceImpl{}).Build()
			},
			wantErr: false,
		},
		{
			name:    "mem0 backend invalid config type",
			backend: BackendLongTermMem0,
			config:  "invalid",
			wantErr: true,
		},
		{
			name:    "mem0 backend constructor error",
			backend: BackendLongTermMem0,
			config:  nil,
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewMem0MemoryBackend).Return(nil, errors.New("init error")).Build()
			},
			wantErr: true,
		},
		{
			name:    "redis backend default config",
			backend: BackendLongTermRedis,
			config:  nil,
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewRedisMemoryBackend).Return(nil, nil).Build()
				mockey.Mock(long_term_memory_backends.LongTermMemoryFactory).Return(&mockMemoryServiceImpl{}).Build()
			},
			wantErr: false,
		},
		{
			name:    "redis backend valid config",
			backend: BackendLongTermRedis,
			config:  &long_term_memory_backends.RedisMemoryConfig{Host: "localhost"},
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewRedisMemoryBackend).Return(nil, nil).Build()
				mockey.Mock(long_term_memory_backends.LongTermMemoryFactory).Return(&mockMemoryServiceImpl{}).Build()
			},
			wantErr: false,
		},
		{
			name:    "redis backend invalid config type",
			backend: BackendLongTermRedis,
			config:  "invalid",
			wantErr: true,
		},
		{
			name:    "redis backend constructor error",
			backend: BackendLongTermRedis,
			config:  nil,
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewRedisMemoryBackend).Return(nil, errors.New("init error")).Build()
			},
			wantErr: true,
		},
		{
			name:    "opensearch backend default config",
			backend: BackendLongTermOpenSearch,
			config:  nil,
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewOpenSearchMemoryBackend).Return(nil, nil).Build()
				mockey.Mock(long_term_memory_backends.LongTermMemoryFactory).Return(&mockMemoryServiceImpl{}).Build()
			},
			wantErr: false,
		},
		{
			name:    "opensearch backend valid config",
			backend: BackendLongTermOpenSearch,
			config:  &long_term_memory_backends.OpenSearchMemoryConfig{Host: "localhost"},
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewOpenSearchMemoryBackend).Return(nil, nil).Build()
				mockey.Mock(long_term_memory_backends.LongTermMemoryFactory).Return(&mockMemoryServiceImpl{}).Build()
			},
			wantErr: false,
		},
		{
			name:    "opensearch backend invalid config type",
			backend: BackendLongTermOpenSearch,
			config:  "invalid",
			wantErr: true,
		},
		{
			name:    "opensearch backend constructor error",
			backend: BackendLongTermOpenSearch,
			config:  nil,
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewOpenSearchMemoryBackend).Return(nil, errors.New("init error")).Build()
			},
			wantErr: true,
		},
		{
			name:    "unsupported backend",
			backend: "test",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		mockey.PatchConvey(tt.name, t, func() {
			if tt.setupMock != nil {
				tt.setupMock()
			}
			memoryService, err := NewLongTermMemoryService(tt.backend, tt.config)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, memoryService)
			}
		})
	}
}

func TestLongTermMemoryDefaultPolicyKeepsUserTextOnly(t *testing.T) {
	ctx := context.Background()
	sessionService := session.InMemoryService()
	createResp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName:   "app",
		UserID:    "user",
		SessionID: "session",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	appendMemoryEvent(t, ctx, sessionService, createResp.Session, genai.RoleUser, genai.RoleUser, []*genai.Part{{Text: "user text"}})
	appendMemoryEvent(t, ctx, sessionService, createResp.Session, genai.RoleModel, "assistant", []*genai.Part{{Text: "assistant text"}})
	appendMemoryEvent(t, ctx, sessionService, createResp.Session, genai.RoleUser, genai.RoleUser, []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "tool"}}})

	backend := &recordingLongTermBackend{}
	memoryService := long_term_memory_backends.LongTermMemoryFactory(backend, 5)
	if err := memoryService.AddSessionToMemory(ctx, createResp.Session); err != nil {
		t.Fatalf("AddSessionToMemory() error = %v", err)
	}
	if len(backend.events) != 1 {
		t.Fatalf("saved events = %d, want 1: %v", len(backend.events), backend.events)
	}
	var content genai.Content
	if err := json.Unmarshal([]byte(backend.events[0]), &content); err != nil {
		t.Fatalf("saved event is not genai.Content JSON: %v", err)
	}
	if content.Role != genai.RoleUser || len(content.Parts) != 1 || content.Parts[0].Text != "user text" {
		t.Fatalf("saved content = %+v, want only user text", content)
	}
}

func TestLongTermMemoryAllPolicyKeepsStructuredEventsAndSanitizesMedia(t *testing.T) {
	ctx := context.Background()
	sessionService := session.InMemoryService()
	createResp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName:   "app",
		UserID:    "user",
		SessionID: "session",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	appendMemoryEvent(t, ctx, sessionService, createResp.Session, genai.RoleModel, "assistant", []*genai.Part{{Text: "assistant text"}})
	appendMemoryEvent(t, ctx, sessionService, createResp.Session, genai.RoleUser, genai.RoleUser, []*genai.Part{{InlineData: &genai.Blob{Data: []byte("secret-image"), MIMEType: "image/png"}}})

	backend := &recordingLongTermBackend{}
	memoryService := long_term_memory_backends.LongTermMemoryFactory(backend, 5)
	policyService, ok := memoryService.(PolicyMemoryService)
	if !ok {
		t.Fatal("memory service does not implement PolicyMemoryService")
	}
	if err := policyService.AddSessionToMemoryWithPolicy(ctx, createResp.Session, &MemoryAutoSavePolicy{Preset: MemoryAutoSaveAll}); err != nil {
		t.Fatalf("AddSessionToMemoryWithPolicy() error = %v", err)
	}
	if len(backend.events) != 2 {
		t.Fatalf("saved events = %d, want 2: %v", len(backend.events), backend.events)
	}
	var media genai.Content
	if err := json.Unmarshal([]byte(backend.events[1]), &media); err != nil {
		t.Fatalf("saved media event is not genai.Content JSON: %v", err)
	}
	if got := len(media.Parts[0].InlineData.Data); got != 0 {
		t.Fatalf("inline data bytes saved, len = %d", got)
	}
	if media.Parts[0].PartMetadata["data_omitted"] != true {
		t.Fatalf("media metadata = %+v, want data_omitted", media.Parts[0].PartMetadata)
	}
}

func TestLongTermMemoryCustomPolicyCanKeepFunctionCalls(t *testing.T) {
	ctx := context.Background()
	sessionService := session.InMemoryService()
	createResp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName:   "app",
		UserID:    "user",
		SessionID: "session",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	appendMemoryEvent(t, ctx, sessionService, createResp.Session, genai.RoleUser, genai.RoleUser, []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "load_memory"}}})

	backend := &recordingLongTermBackend{}
	memoryService := long_term_memory_backends.LongTermMemoryFactory(backend, 5)
	policyService := memoryService.(PolicyMemoryService)
	err = policyService.AddSessionToMemoryWithPolicy(ctx, createResp.Session, &MemoryAutoSavePolicy{
		Preset:            MemoryAutoSaveCustom,
		IncludeRoles:      []MemoryRole{MemoryRoleUser},
		IncludeEventTypes: []MemoryEventType{MemoryEventFunctionCall},
	})
	if err != nil {
		t.Fatalf("AddSessionToMemoryWithPolicy() error = %v", err)
	}
	if len(backend.events) != 1 {
		t.Fatalf("saved events = %d, want 1: %v", len(backend.events), backend.events)
	}
	if !json.Valid([]byte(backend.events[0])) {
		t.Fatalf("saved event is not valid JSON: %s", backend.events[0])
	}
}

func appendMemoryEvent(t *testing.T, ctx context.Context, sessionService session.Service, sess session.Session, role, author string, parts []*genai.Part) {
	t.Helper()
	err := sessionService.AppendEvent(ctx, sess, &session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Role:  role,
				Parts: parts,
			},
		},
		Author:    author,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
}
