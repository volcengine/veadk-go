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
	"testing"
	"time"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/auth/veauth"
	mem0 "github.com/volcengine/veadk-go/integrations/ve_mem0"
)

func TestNewMem0MemoryBackend(t *testing.T) {
	mockey.PatchConvey("TestNewMem0MemoryBackend", t, func() {
		mockey.PatchConvey("BaseUrl not set", func() {
			config := &Mem0MemoryConfig{
				BaseUrl: "",
			}
			backend, err := NewMem0MemoryBackend(config)
			assert.Nil(t, backend)
			assert.Equal(t, ErrBaseUrlNotSet, err)
		})

		mockey.PatchConvey("ApiKey not set and ProjectId not set", func() {
			config := &Mem0MemoryConfig{
				BaseUrl: "http://localhost",
				ApiKey:  "",
			}
			backend, err := NewMem0MemoryBackend(config)
			assert.Nil(t, backend)
			assert.Equal(t, ErrApiKeyNotSet, err)
		})

		mockey.PatchConvey("ApiKey not set, ProjectId set, GetVikingMem0Token failed", func() {
			config := &Mem0MemoryConfig{
				BaseUrl:   "http://localhost",
				ProjectId: "test_project",
				Region:    "test_region",
			}
			mockey.Mock(veauth.GetVikingMem0Token).Return("", errors.New("token error")).Build()
			backend, err := NewMem0MemoryBackend(config)
			assert.Nil(t, backend)
			assert.NotNil(t, err)
			assert.Equal(t, "token error", err.Error())
		})

		mockey.PatchConvey("ApiKey not set, ProjectId set, GetVikingMem0Token success", func() {
			config := &Mem0MemoryConfig{
				BaseUrl:   "http://localhost",
				ProjectId: "test_project",
				Region:    "test_region",
			}
			mockey.Mock(veauth.GetVikingMem0Token).Return("generated_api_key", nil).Build()
			// NewMem0Client returns only *Mem0Client, no error
			mockey.Mock(mem0.NewMem0Client).Return(&mem0.Mem0Client{}).Build()

			backend, err := NewMem0MemoryBackend(config)
			assert.NotNil(t, backend)
			assert.Nil(t, err)
			// check if apiKey was set in config
			assert.Equal(t, "generated_api_key", config.ApiKey)
		})

		mockey.PatchConvey("Success with ApiKey", func() {
			config := &Mem0MemoryConfig{
				BaseUrl: "http://localhost",
				ApiKey:  "test_api_key",
			}
			// NewMem0Client returns only *Mem0Client, no error
			mockey.Mock(mem0.NewMem0Client).Return(&mem0.Mem0Client{}).Build()

			backend, err := NewMem0MemoryBackend(config)
			assert.NotNil(t, backend)
			assert.Nil(t, err)
		})
	})
}

func TestMem0MemoryBackend_SaveMemory(t *testing.T) {
	backend := &Mem0MemoryBackend{
		client: &mem0.Mem0Client{},
		config: &Mem0MemoryConfig{},
	}
	ctx := context.Background()

	mockey.PatchConvey("TestMem0MemoryBackend_SaveMemory", t, func() {
		mockey.PatchConvey("Success", func() {
			eventList := []string{"event1", "event2"}
			var callCount int
			mockey.Mock((*mem0.Mem0Client).Add).To(func(ctx context.Context, req mem0.AddMemoriesRequest) (mem0.AddMemoriesResponse, error) {
				callCount++
				return mem0.AddMemoriesResponse{}, nil
			}).Build()

			err := backend.SaveMemory(ctx, "test_user", eventList)
			assert.Nil(t, err)
			assert.Equal(t, 2, callCount)
		})

		mockey.PatchConvey("Failure", func() {
			eventList := []string{"event1"}
			mockey.Mock((*mem0.Mem0Client).Add).Return(mem0.AddMemoriesResponse{}, errors.New("add error")).Build()

			err := backend.SaveMemory(ctx, "test_user", eventList)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "failed to save memory to Mem0")
		})
	})
}

func TestMem0MemoryBackend_SearchMemory(t *testing.T) {
	backend := &Mem0MemoryBackend{
		client: &mem0.Mem0Client{},
		config: &Mem0MemoryConfig{},
	}
	ctx := context.Background()

	mockey.PatchConvey("TestMem0MemoryBackend_SearchMemory", t, func() {
		mockey.PatchConvey("Success", func() {
			now := time.Now()
			mockey.Mock((*mem0.Mem0Client).Search).Return(mem0.SearchMemoriesResponse{
				Results: []mem0.MemoryItem{
					{
						Memory:    "memory 1",
						CreatedAt: now,
					},
					{
						Memory:    "memory 2",
						CreatedAt: now,
					},
				},
			}, nil).Build()

			results, err := backend.SearchMemory(ctx, "test_user", "test query", 10)

			assert.Nil(t, err)
			assert.NotNil(t, results)
			assert.Equal(t, 2, len(results))
			assert.Equal(t, "memory 1", results[0].Content)
			assert.Equal(t, now, results[0].Timestamp)
		})

		mockey.PatchConvey("Empty Results", func() {
			mockey.Mock((*mem0.Mem0Client).Search).Return(mem0.SearchMemoriesResponse{
				Results: []mem0.MemoryItem{},
			}, nil).Build()

			results, err := backend.SearchMemory(ctx, "test_user", "test query", 10)

			assert.Nil(t, err)
			assert.Equal(t, 0, len(results))
		})

		mockey.PatchConvey("Failure", func() {
			mockey.Mock((*mem0.Mem0Client).Search).Return(mem0.SearchMemoriesResponse{}, errors.New("search error")).Build()

			results, err := backend.SearchMemory(ctx, "test_user", "test query", 10)

			assert.Nil(t, results)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "failed to search memory from Mem0")
		})
	})
}
