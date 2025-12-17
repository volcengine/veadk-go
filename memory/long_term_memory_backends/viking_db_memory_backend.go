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
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/volcengine/veadk-go/integrations/ve_viking"
	"github.com/volcengine/veadk-go/integrations/ve_viking/viking_memory"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

const (
	DefaultIndex       = "veadk"
	DefaultSearchLimit = 5
)

var ErrCollectionInfo = errors.New("collection info error")
var ErrCollectionCreate = errors.New("collection create error")

type VikingDbMemoryConfig struct {
	AK               string
	SK               string
	SessionToken     string
	Index            string
	Project          string
	Region           string
	CreateIfNotExist *bool
	MemoryTypes      []string
	SearchLimit      int
}

type VikingDBMemoryBackend struct {
	config *VikingDbMemoryConfig
	client *viking_memory.Client
}

func NewVikingDbMemoryBackend(config *VikingDbMemoryConfig) (memory.Service, error) {
	if config.Index == "" {
		config.Index = DefaultIndex
	}
	if config.MemoryTypes == nil {
		config.MemoryTypes = []string{"sys_event_v1"}
	}
	if config.CreateIfNotExist == nil {
		config.CreateIfNotExist = new(bool)
		*config.CreateIfNotExist = true
	}
	if config.SearchLimit == 0 {
		config.SearchLimit = DefaultSearchLimit
	}

	client, err := viking_memory.New(&ve_viking.ClientConfig{
		AK:           config.AK,
		SK:           config.SK,
		SessionToken: config.SessionToken,
		Index:        config.Index,
		Project:      config.Project,
		Region:       config.Region,
	})
	if err != nil {
		return nil, err
	}
	backend := &VikingDBMemoryBackend{
		client: client,
		config: config,
	}

	err = client.CollectionInfo()
	if err != nil {
		if strings.Contains(err.Error(), "collection not exist") {
			if *config.CreateIfNotExist {
				_, err := client.CollectionCreate(&viking_memory.CollectionCreateRequest{
					BuiltinEventTypes: config.MemoryTypes,
				})
				if err != nil {
					return nil, fmt.Errorf("%w : create viking memory index error: %w", ErrCollectionCreate, err)
				}
				log.Info("Create viking knowledge index", config.Index, "successfully", "MemoryTypes", config.MemoryTypes)
			} else {
				return nil, fmt.Errorf("%w : viking memory index not exist", ErrCollectionCreate)
			}
		} else {
			return nil, fmt.Errorf("%w : get viking collection info error: %w", ErrCollectionInfo, err)
		}
	}

	return backend, nil
}

func (v *VikingDBMemoryBackend) AddSession(ctx context.Context, s session.Session) error {
	req := &viking_memory.AddSessionRequest{}
	uuid1, err := uuid.NewUUID()
	if err != nil {
		return fmt.Errorf("generate uuid failed: %w", err)
	}
	req.SessionId = uuid1.String()

	for i := 0; i < s.Events().Len(); i++ {
		event := s.Events().At(i)
		if event.Content == nil || len(event.Content.Parts) == 0 {
			continue
		}
		content := event.Content.Parts[0].Text
		if content == "" {
			continue
		}

		role := event.Content.Role
		if role != "user" {
			role = "assistant"
		}
		req.Messages = append(req.Messages, &viking_memory.Message{
			Content: content,
			Role:    role,
			Time:    event.Timestamp.UnixMilli(),
		})
	}
	req.Metadata.DefaultUserId = s.UserID()
	req.Metadata.DefaultAssistantId = "assistant"
	req.Metadata.Time = s.LastUpdateTime().UnixMilli()

	log.Info("add events to long term memory", "length", len(req.Messages), "index", v.config.Index)
	resp, err := v.client.AddSession(req)
	if err != nil {
		return err
	}
	if resp.Code != ve_viking.VikingKnowledgeBaseSuccessCode {
		return fmt.Errorf("add session failed: %v", resp)
	}
	return nil
}

func (v *VikingDBMemoryBackend) Search(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	vikingReq := &viking_memory.CollectionSearchMemoryRequest{
		Filter: viking_memory.Filter{
			UserId:     []string{req.UserID},
			MemoryType: v.config.MemoryTypes,
		},
		Query: req.Query,
		Limit: v.config.SearchLimit,
	}
	log.Debug("search viking memory", "filter", vikingReq.Filter, "collection", v.config.Index, "query", req.Query, "limit", v.config.SearchLimit)
	resp, err := v.client.CollectionSearchMemory(vikingReq)
	if err != nil {
		return nil, err
	}

	if resp.Code != ve_viking.VikingKnowledgeBaseSuccessCode {
		return nil, fmt.Errorf("search viking failed: %v", resp)
	}

	memResp := &memory.SearchResponse{}
	if resp.Data != nil {
		for _, v := range resp.Data.ResultList {
			memResp.Memories = append(memResp.Memories, memory.Entry{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{
							Text: v.MemoryInfo.Summary,
						},
					},
					Role: "user",
				},
				Author:    "user",
				Timestamp: utils.ConvertTimeMillToTime(v.Time),
			})
		}
	}

	return memResp, nil
}
