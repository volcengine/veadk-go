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

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	mem0 "github.com/volcengine/veadk-go/integrations/ve_mem0"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/utils"
)

var (
	ErrApiKeyNotSet  = errors.New("API Key not set, auto fetching api key needs `ProjectId`")
	ErrBaseUrlNotSet = errors.New("BaseUrl not set")
)

type Mem0MemoryConfig struct {
	BaseUrl   string
	ApiKey    string
	ProjectId string
	Region    string
}

type Mem0MemoryBackend struct {
	config *Mem0MemoryConfig
	client *mem0.Mem0Client
}

func NewDefaultMem0MemoryConfig() *Mem0MemoryConfig {
	return &Mem0MemoryConfig{
		BaseUrl: utils.GetEnvWithDefault(common.DATABASE_MEM0_BASE_URL, configs.GetGlobalConfig().Database.Mem0.BaseUrl),
		ApiKey:  utils.GetEnvWithDefault(common.DATABASE_MEM0_API_KEY, configs.GetGlobalConfig().Database.Mem0.ApiKey),
		Region:  utils.GetEnvWithDefault(common.DATABASE_MEM0_REGION, configs.GetGlobalConfig().Database.Mem0.Region),
	}
}

func NewMem0MemoryBackend(config *Mem0MemoryConfig) (LongTermMemoryBackend, error) {
	if config.BaseUrl == "" {
		return nil, ErrBaseUrlNotSet
	}
	if config.ApiKey == "" {
		if config.ProjectId == "" {
			return nil, ErrApiKeyNotSet
		}
		apiKey, err := veauth.GetVikingMem0Token(config.ProjectId, config.Region)
		if err != nil {
			return nil, err
		}
		config.ApiKey = apiKey
	}

	backend := &Mem0MemoryBackend{
		client: mem0.NewMem0Client(config.BaseUrl, config.ApiKey),
		config: config,
	}

	return backend, nil
}

func (mem *Mem0MemoryBackend) SaveMemory(ctx context.Context, userId string, eventList []string) error {
	asyncMode := true
	for _, event := range eventList {
		_, err := mem.client.Add(ctx, mem0.AddMemoriesRequest{
			Messages: []mem0.Message{
				{
					Role:    "user",
					Content: event,
				},
			},
			UserId:    &userId,
			AsyncMode: &asyncMode,
		})
		if err != nil {
			return fmt.Errorf("failed to save memory to Mem0: %w", err)
		}
	}
	log.Infof("Successfully saved user %s %d events to Mem0", userId, len(eventList))
	return nil
}

func (mem *Mem0MemoryBackend) SearchMemory(ctx context.Context, userId, query string, topK int) ([]*MemItem, error) {
	log.Infof("Searching Mem0 for query: %s, user: %s, top_k: %d", query, userId, topK)

	var memResp []*MemItem

	result, err := mem.client.Search(ctx, mem0.SearchMemoriesRequest{
		Query:  query,
		UserId: &userId,
		TopK:   &topK,
	})
	if err != nil {
		return memResp, fmt.Errorf("failed to search memory from Mem0: %w", err)
	}

	for _, v := range result.Results {
		memResp = append(memResp, &MemItem{
			Content:   v.Memory,
			Timestamp: v.CreatedAt,
		})
	}

	return memResp, nil
}
