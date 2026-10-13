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

package long_term_memory_backends

import (
	"context"
	"testing"
	"time"

	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type recordingBackend struct {
	events []string
}

func (b *recordingBackend) SaveMemory(_ context.Context, _ string, eventList []string) error {
	b.events = append(b.events, eventList...)
	return nil
}

func (b *recordingBackend) SearchMemory(context.Context, string, string, int) ([]*MemItem, error) {
	items := make([]*MemItem, 0, len(b.events))
	for _, event := range b.events {
		items = append(items, &MemItem{Content: event, Timestamp: time.Unix(1, 0)})
	}
	return items, nil
}

func TestSearchMemoryDoesNotPanicForEmptyJSONContent(t *testing.T) {
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
	if err := sessionService.AppendEvent(ctx, createResp.Session, &session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{Role: genai.RoleUser},
		},
		Author: genai.RoleUser,
	}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	backend := &recordingBackend{}
	memoryService := LongTermMemoryFactory(backend, 5).(PolicyMemoryService)
	if err := memoryService.AddSessionToMemoryWithPolicy(ctx, createResp.Session, &MemoryAutoSavePolicy{Preset: MemoryAutoSaveAll}); err != nil {
		t.Fatalf("AddSessionToMemoryWithPolicy() error = %v", err)
	}
	resp, err := LongTermMemoryFactory(backend, 5).SearchMemory(ctx, &memory.SearchRequest{
		AppName: "app",
		UserID:  "user",
		Query:   "anything",
	})
	if err != nil {
		t.Fatalf("SearchMemory() error = %v", err)
	}
	if got, want := len(resp.Memories), 1; got != want {
		t.Fatalf("memories = %d, want %d", got, want)
	}
	if resp.Memories[0].Content == nil || len(resp.Memories[0].Content.Parts) != 1 {
		t.Fatalf("memory content not normalized: %+v", resp.Memories[0].Content)
	}
}
