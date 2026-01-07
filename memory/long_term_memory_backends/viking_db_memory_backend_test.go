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
	"github.com/volcengine/veadk-go/integrations/ve_viking"
	"github.com/volcengine/veadk-go/integrations/ve_viking/viking_memory"
	"github.com/volcengine/veadk-go/utils"
)

func TestNewVikingDbMemoryBackend(t *testing.T) {
	mockey.PatchConvey("TestNewVikingDbMemoryBackend", t, func() {
		mockey.Mock(ve_viking.NewConfig).Return(&ve_viking.ClientConfig{}, nil).Build()
		mockey.PatchConvey("collection info failed", func() {
			mockey.Mock((*viking_memory.Client).CollectionInfo).Return(errors.New("collection info failed")).Build()
			memoryService, err := NewVikingDbMemoryBackend(&VikingDbMemoryConfig{})
			assert.Nil(t, memoryService)
			assert.NotNil(t, err)
		})
		mockey.PatchConvey("collection info failed and not create", func() {
			mockey.Mock((*viking_memory.Client).CollectionInfo).Return(errors.New("collection not exist")).Build()
			createNotExist := new(bool)
			*createNotExist = false
			memoryService, err := NewVikingDbMemoryBackend(&VikingDbMemoryConfig{CreateIfNotExist: createNotExist})
			assert.Nil(t, memoryService)
			assert.NotNil(t, err)
		})

		mockey.PatchConvey("collection info failed and create failed", func() {
			mockey.Mock((*viking_memory.Client).CollectionInfo).Return(errors.New("collection not exist")).Build()
			mockey.Mock((*viking_memory.Client).CollectionCreate).Return(nil, errors.New("collection info failed")).Build()
			memoryService, err := NewVikingDbMemoryBackend(&VikingDbMemoryConfig{})
			assert.Nil(t, memoryService)
			assert.NotNil(t, err)
		})

		mockey.PatchConvey("collection info failed and create success", func() {
			mockey.Mock((*viking_memory.Client).CollectionInfo).Return(errors.New("collection not exist")).Build()
			mockey.Mock((*viking_memory.Client).CollectionCreate).Return(&ve_viking.CommonResponse{Code: ve_viking.VikingKnowledgeBaseSuccessCode}, nil).Build()
			memoryService, err := NewVikingDbMemoryBackend(&VikingDbMemoryConfig{})
			assert.NotNil(t, memoryService)
			assert.Nil(t, err)
		})

		mockey.PatchConvey("collection info success", func() {
			mockey.Mock((*viking_memory.Client).CollectionInfo).Return(nil).Build()
			memoryService, err := NewVikingDbMemoryBackend(&VikingDbMemoryConfig{})
			assert.NotNil(t, memoryService)
			assert.Nil(t, err)
		})
	})
}

func TestVikingDbMemoryBackend_SaveMemory(t *testing.T) {
	v := &VikingDBMemoryBackend{
		client: &viking_memory.Client{},
		config: &VikingDbMemoryConfig{
			Index: DefaultIndex,
		},
	}
	ctx := context.Background()
	mockey.PatchConvey("TestVikingDbMemoryBackend_SaveMemory", t, func() {
		mockey.Mock((*viking_memory.Client).AddSession).Return(&ve_viking.CommonResponse{Code: ve_viking.VikingKnowledgeBaseSuccessCode}, nil).Build()
		err := v.SaveMemory(ctx, "test", []string{"test1", "test2"})
		assert.Nil(t, err)
	})
}

func TestVikingDbMemoryBackend_SearchMemory(t *testing.T) {
	v := &VikingDBMemoryBackend{
		client: &viking_memory.Client{},
		config: &VikingDbMemoryConfig{
			Index: DefaultIndex,
		},
	}
	ctx := context.Background()
	nowUnix := time.Now().UnixMilli()
	now := utils.ConvertTimeMillToTime(nowUnix)
	mockey.PatchConvey("TestVikingDbMemoryBackend_SearchMemory", t, func() {
		mockey.Mock((*viking_memory.Client).CollectionSearchMemory).Return(&viking_memory.CollectionSearchMemoryResponse{
			Code: ve_viking.VikingKnowledgeBaseSuccessCode,
			Data: &viking_memory.CollectionSearchMemoryResponseData{
				ResultList: []*viking_memory.CollectionSearchResponseItem{
					{
						MemoryInfo: &viking_memory.MemoryInfo{
							Summary: "test1",
						},
						Time: nowUnix,
					},
					{
						MemoryInfo: &viking_memory.MemoryInfo{
							Summary: "test2",
						},
						Time: nowUnix,
					},
				},
			},
		}, nil).Build()
		resp, err := v.SearchMemory(ctx, "test", "test", 2)
		assert.Nil(t, err)
		assert.Equal(t, 2, len(resp))
		for i, v := range resp {
			assert.Equal(t, now, v.Timestamp)
			if i == 0 {
				assert.Equal(t, "test1", v.Content)
			} else {
				assert.Equal(t, "test2", v.Content)
			}
		}
	})
}
