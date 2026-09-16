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

package viking_memory

import (
	"testing"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/v2/integrations/ve_sign"
	"github.com/volcengine/veadk-go/v2/integrations/ve_viking"
)

func TestVikingMemoryClient_CollectionCreate(t *testing.T) {
	mockey.PatchConvey("TestVikingMemoryClient_CollectionCreate", t, func() {
		mockey.Mock(ve_viking.NewConfig).Return(&ve_viking.ClientConfig{}, nil).Build()
		mockey.Mock(ve_sign.VeRequest.DoRequestWithContext).Return([]byte(`{"code": 0}`), nil).Build()

		client, err := New(&ve_viking.ClientConfig{Index: "test"})
		assert.Nil(t, err)

		resp, err := client.CollectionCreate(&CollectionCreateRequest{})
		assert.Nil(t, err)
		assert.Equal(t, int(resp.Code), ve_viking.VikingKnowledgeBaseSuccessCode)
	})
}

func TestVikingMemoryClient_CollectionInfo(t *testing.T) {
	mockey.PatchConvey("TestVikingMemoryClient_CollectionInfo", t, func() {
		mockey.Mock(ve_viking.NewConfig).Return(&ve_viking.ClientConfig{}, nil).Build()
		mockey.Mock(ve_sign.VeRequest.DoRequestWithContext).Return([]byte(`{"code": 0}`), nil).Build()
		client, err := New(&ve_viking.ClientConfig{Index: "test"})
		assert.Nil(t, err)
		err = client.CollectionInfo()
		assert.Nil(t, err)
	})
}

func TestVikingMemoryClient_CollectionSearchMemory(t *testing.T) {
	mockey.PatchConvey("TestVikingMemoryClient_CollectionSearchMemory", t, func() {
		mockey.Mock(ve_viking.NewConfig).Return(&ve_viking.ClientConfig{}, nil).Build()
		mockey.Mock(ve_sign.VeRequest.DoRequestWithContext).Return([]byte(`{"code": 0, "data": {"result_list": [{"memory_info":{"summary":"test"}}]}}`), nil).Build()
		client, err := New(&ve_viking.ClientConfig{Index: "test"})
		assert.Nil(t, err)

		resp, err := client.CollectionSearchMemory(&CollectionSearchMemoryRequest{})
		assert.Nil(t, err)
		assert.Equal(t, int(resp.Code), ve_viking.VikingKnowledgeBaseSuccessCode)
		assert.Equal(t, resp.Data.ResultList[0].MemoryInfo.Summary, "test")
	})
}

func TestVikingMemoryClient_AddSession(t *testing.T) {
	mockey.PatchConvey("TestVikingMemoryClient_AddSession", t, func() {
		mockey.Mock(ve_sign.VeRequest.DoRequestWithContext).Return([]byte(`{"code": 0}`), nil).Build()
		mockey.Mock(ve_viking.NewConfig).Return(&ve_viking.ClientConfig{}, nil).Build()

		client, err := New(&ve_viking.ClientConfig{Index: "test"})
		assert.Nil(t, err)

		resp, err := client.AddSession(&AddSessionRequest{})
		assert.Nil(t, err)
		assert.Equal(t, int(resp.Code), ve_viking.VikingKnowledgeBaseSuccessCode)
	})
}
