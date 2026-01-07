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
	"encoding/json"
	"time"

	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

type MemItem struct {
	Content   string
	Timestamp time.Time
}

type LongTermMemoryBackend interface {
	SaveMemory(ctx context.Context, userId string, eventList []string) error
	SearchMemory(ctx context.Context, userId, query string, topK int) ([]*MemItem, error)
}

func LongTermMemoryFactory(backend LongTermMemoryBackend, tokK int) memory.Service {
	return &basicLongTermMemory{
		backend: backend,
		topK:    tokK,
	}
}

type basicLongTermMemory struct {
	backend LongTermMemoryBackend
	topK    int
}

func (*basicLongTermMemory) filterAndConvertEvents(s session.Session) []string {
	var eventList []string
	for event := range s.Events().All() {
		if event.Content == nil || len(event.Content.Parts) == 0 || event.Content.Role != "user" || event.Content.Parts[0].Text == "" {
			continue
		}

		bytes, _ := json.Marshal(event.Content)
		eventList = append(eventList, string(bytes))
	}
	return eventList
}

func (b *basicLongTermMemory) AddSession(ctx context.Context, s session.Session) error {
	userId := s.UserID()
	events := b.filterAndConvertEvents(s)
	return b.backend.SaveMemory(ctx, userId, events)
}

func (b *basicLongTermMemory) Search(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	result, err := b.backend.SearchMemory(ctx, req.UserID, req.Query, b.topK)
	if err != nil {
		return nil, err
	}
	memResp := &memory.SearchResponse{
		Memories: make([]memory.Entry, 0),
	}
	for _, item := range result {
		if len(item.Content) > 0 && item.Content[0] == '{' {
			var content genai.Content
			_ = json.Unmarshal([]byte(item.Content), &content)
			memResp.Memories = append(memResp.Memories, memory.Entry{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{
							Text: content.Parts[0].Text,
						},
					},
					Role: content.Role,
				},
				Author:    "user",
				Timestamp: item.Timestamp,
			})
		} else {
			memResp.Memories = append(memResp.Memories, memory.Entry{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{
							Text: item.Content,
						},
					},
					Role: "user",
				},
				Author:    "user",
				Timestamp: item.Timestamp,
			})
		}
	}
	return memResp, nil
}
