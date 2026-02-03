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
	"time"

	"github.com/google/uuid"
	"github.com/volcengine/veadk-go/integrations/ve_viking"
	"github.com/volcengine/veadk-go/integrations/ve_viking/viking_memory"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/utils"
)

const (
	DefaultIndex = "veadk"
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
}

type VikingDBMemoryBackend struct {
	config *VikingDbMemoryConfig
	client *viking_memory.Client
}

func NewVikingDbMemoryBackend(config *VikingDbMemoryConfig) (LongTermMemoryBackend, error) {
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

func (v *VikingDBMemoryBackend) SaveMemory(ctx context.Context, userId string, eventList []string) error {
	req := &viking_memory.AddSessionRequest{}
	uuid1, err := uuid.NewUUID()
	if err != nil {
		return fmt.Errorf("generate uuid failed: %w", err)
	}
	req.SessionId = uuid1.String()

	for _, event := range eventList {
		req.Messages = append(req.Messages, &viking_memory.Message{
			Content: event,
			Role:    "user",
			Time:    time.Now().UnixMilli(),
		})
	}

	req.Metadata.DefaultUserId = userId
	req.Metadata.DefaultAssistantId = "assistant"
	req.Metadata.Time = time.Now().UnixMilli()

	resp, err := v.client.AddSession(req)
	if err != nil {
		return err
	}
	if resp.Code != ve_viking.VikingKnowledgeBaseSuccessCode {
		return fmt.Errorf("viking add memories failed: %v", resp)
	}

	log.Infof("Successfully saved user %s %d events to viking", userId, len(eventList))
	return nil
}

func (v *VikingDBMemoryBackend) SearchMemory(ctx context.Context, userId, query string, topK int) ([]*MemItem, error) {
	log.Infof("Searching viking for query: %s, user: %s, top_k: %d", query, userId, topK)
	var memResp []*MemItem

	vikingReq := &viking_memory.CollectionSearchMemoryRequest{
		Filter: viking_memory.Filter{
			UserId:     []string{userId},
			MemoryType: v.config.MemoryTypes,
		},
		Query: query,
		Limit: topK,
	}

	resp, err := v.client.CollectionSearchMemory(vikingReq)
	if err != nil {
		return nil, err
	}

	if resp.Code != ve_viking.VikingKnowledgeBaseSuccessCode {
		return nil, fmt.Errorf("search viking failed: %v", resp)
	}

	if resp.Data != nil {
		for _, v := range resp.Data.ResultList {
			memResp = append(memResp, &MemItem{
				Content:   v.MemoryInfo.Summary,
				Timestamp: utils.ConvertTimeMillToTime(v.Time),
			})
		}
	}

	return memResp, nil
}
