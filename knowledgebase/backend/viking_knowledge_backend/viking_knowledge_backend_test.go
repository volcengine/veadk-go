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

package viking_knowledge_backend

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/integrations/ve_tos"
	"github.com/volcengine/veadk-go/integrations/ve_viking"
	"github.com/volcengine/veadk-go/integrations/ve_viking/viking_knowledge"
)

func TestNewVikingKnowledgeBackend(t *testing.T) {
	mockey.PatchConvey("TestNewVikingKnowledgeBackend", t, func() {
		cfg := &Config{
			AK:               "ak",
			SK:               "sk",
			Index:            "test_index",
			Project:          "proj",
			Region:           "cn-beijing",
			CreateIfNotExist: true,
			TosConfig: &ve_tos.Config{
				AK:       "ak",
				SK:       "sk",
				Region:   "cn-beijing",
				Endpoint: "https://xxxxxxx.com",
				Bucket:   "veadk-tests",
			},
		}

		mockey.PatchConvey("collection info error", func() {
			mockey.Mock((*viking_knowledge.Client).CollectionInfo).Return(nil, assert.AnError).Build()
			kb, err := NewVikingKnowledgeBackend(cfg)
			assert.Nil(t, kb)
			assert.NotNil(t, err)
		})

		mockey.PatchConvey("index not exist and not create", func() {
			c := *cfg
			c.CreateIfNotExist = false
			mockey.Mock((*viking_knowledge.Client).CollectionInfo).Return(&ve_viking.CommonResponse{Code: ve_viking.VikingKnowledgeBaseIndexNotExistCode}, nil).Build()
			kb, err := NewVikingKnowledgeBackend(&c)
			assert.Nil(t, kb)
			assert.NotNil(t, err)
		})

		mockey.PatchConvey("index not exist and create failed", func() {
			mockey.Mock((*viking_knowledge.Client).CollectionInfo).Return(&ve_viking.CommonResponse{Code: ve_viking.VikingKnowledgeBaseIndexNotExistCode}, nil).Build()
			mockey.Mock((*viking_knowledge.Client).CollectionCreate).Return(nil, assert.AnError).Build()
			kb, err := NewVikingKnowledgeBackend(cfg)
			assert.Nil(t, kb)
			assert.NotNil(t, err)
		})

		mockey.PatchConvey("index not exist and create success", func() {
			mockey.Mock((*viking_knowledge.Client).CollectionInfo).Return(&ve_viking.CommonResponse{Code: ve_viking.VikingKnowledgeBaseIndexNotExistCode}, nil).Build()
			mockey.Mock((*viking_knowledge.Client).CollectionCreate).Return(&ve_viking.CommonResponse{Code: ve_viking.VikingKnowledgeBaseSuccessCode}, nil).Build()
			kb, err := NewVikingKnowledgeBackend(cfg)
			assert.NotNil(t, kb)
			assert.Nil(t, err)
		})

		mockey.PatchConvey("collection info success", func() {
			mockey.Mock((*viking_knowledge.Client).CollectionInfo).Return(&ve_viking.CommonResponse{Code: ve_viking.VikingKnowledgeBaseSuccessCode}, nil).Build()
			kb, err := NewVikingKnowledgeBackend(cfg)
			assert.NotNil(t, kb)
			assert.Nil(t, err)
		})
	})
}

func TestVikingKnowledgeBackend_Search(t *testing.T) {
	var chunkDiffusionCount int32 = 1
	var rerank = true
	v := &VikingKnowledgeBackend{
		viking: &viking_knowledge.Client{},
		tos:    &ve_tos.Client{},
		config: &Config{TopK: 3, ChunkDiffusionCount: &chunkDiffusionCount, Rerank: &rerank},
	}
	mockey.PatchConvey("TestVikingKnowledgeBackend_Search", t, func() {
		mockey.PatchConvey("search error", func() {
			mockey.Mock((*viking_knowledge.Client).SearchKnowledge).Return(nil, assert.AnError).Build()
			entries, err := v.Search("q")
			assert.Nil(t, entries)
			assert.NotNil(t, err)
		})

		mockey.PatchConvey("bad code", func() {
			mockey.Mock((*viking_knowledge.Client).SearchKnowledge).Return(&viking_knowledge.CollectionSearchKnowledgeResponse{Code: 1, Message: "bad"}, nil).Build()
			entries, err := v.Search("q")
			assert.Nil(t, entries)
			assert.NotNil(t, err)
		})

		mockey.PatchConvey("doc meta invalid", func() {
			mockey.Mock((*viking_knowledge.Client).SearchKnowledge).Return(&viking_knowledge.CollectionSearchKnowledgeResponse{
				Code: ve_viking.VikingKnowledgeBaseSuccessCode,
				Data: &viking_knowledge.CollectionSearchKnowledgeResponseData{
					ResultList: []*viking_knowledge.CollectionSearchResponseItem{
						{Content: "c1", DocInfo: viking_knowledge.CollectionSearchResponseItemDocInfo{DocMeta: "{"}},
					},
				},
			}, nil).Build()
			entries, err := v.Search("q")
			assert.Nil(t, entries)
			assert.NotNil(t, err)
		})

		mockey.PatchConvey("success", func() {
			mockey.Mock((*viking_knowledge.Client).SearchKnowledge).Return(&viking_knowledge.CollectionSearchKnowledgeResponse{
				Code: ve_viking.VikingKnowledgeBaseSuccessCode,
				Data: &viking_knowledge.CollectionSearchKnowledgeResponseData{
					ResultList: []*viking_knowledge.CollectionSearchResponseItem{
						{Content: "c1", DocInfo: viking_knowledge.CollectionSearchResponseItemDocInfo{DocMeta: "{\"key\":\"v\"}"}},
						{Content: "c2", DocInfo: viking_knowledge.CollectionSearchResponseItemDocInfo{DocMeta: ""}},
					},
				},
			}, nil).Build()
			entries, err := v.Search("q")
			assert.Nil(t, err)
			assert.Equal(t, 2, len(entries))
			assert.Equal(t, "c1", entries[0].Content)
			assert.Equal(t, "c2", entries[1].Content)
		})
	})
}

func TestVikingKnowledgeBackend_AddFromText(t *testing.T) {
	tosClient, _ := ve_tos.New(&ve_tos.Config{
		AK:       "ak",
		SK:       "sk",
		Region:   "cn-beijing",
		Endpoint: "https://xxxxxxx.com",
		Bucket:   "veadk-tests",
	})
	v := &VikingKnowledgeBackend{
		viking: &viking_knowledge.Client{},
		tos:    tosClient,
		config: &Config{},
	}
	mockey.PatchConvey("TestVikingKnowledgeBackend_AddFromText", t, func() {
		mockey.PatchConvey("success", func() {
			mockey.Mock((*ve_tos.Client).BuildObjectKeyForText).Return("knowledgebase/t.txt").Build()
			mockey.Mock((*ve_tos.Client).UploadText).Return(nil).Build()
			mockey.Mock((*ve_tos.Client).BuildTOSURL).Return("bucket/knowledgebase/t.txt").Build()
			mockey.Mock((*viking_knowledge.Client).DocumentAddTOS).Return(&ve_viking.CommonResponse{Code: ve_viking.VikingKnowledgeBaseSuccessCode}, nil).Build()
			err := v.AddFromText([]string{"a", "b"})
			assert.Nil(t, err)
		})
		mockey.PatchConvey("upload error", func() {
			mockey.Mock((*ve_tos.Client).BuildObjectKeyForText).Return("knowledgebase/t.txt").Build()
			mockey.Mock((*ve_tos.Client).UploadText).Return(assert.AnError).Build()
			err := v.AddFromText([]string{"a"})
			assert.NotNil(t, err)
		})
	})
}

func TestVikingKnowledgeBackend_AddFromFiles(t *testing.T) {
	tosClient, _ := ve_tos.New(&ve_tos.Config{
		AK:       "ak",
		SK:       "sk",
		Region:   "cn-beijing",
		Endpoint: "https://xxxxxxx.com",
		Bucket:   "veadk-tests",
	})
	v := &VikingKnowledgeBackend{
		viking: &viking_knowledge.Client{},
		tos:    tosClient,
		config: &Config{},
	}
	mockey.PatchConvey("TestVikingKnowledgeBackend_AddFromFiles", t, func() {
		mockey.PatchConvey("success", func() {
			mockey.Mock((*ve_tos.Client).BuildObjectKeyForFile).Return("knowledgebase/f.txt").Build()
			mockey.Mock((*ve_tos.Client).UploadFile).Return(nil).Build()
			mockey.Mock((*ve_tos.Client).BuildTOSURL).Return("bucket/knowledgebase/f.txt").Build()
			mockey.Mock((*viking_knowledge.Client).DocumentAddTOS).Return(&ve_viking.CommonResponse{Code: ve_viking.VikingKnowledgeBaseSuccessCode}, nil).Build()
			err := v.AddFromFiles([]string{"f1", "f2"})
			assert.Nil(t, err)
		})
		mockey.PatchConvey("upload error", func() {
			mockey.Mock((*ve_tos.Client).BuildObjectKeyForFile).Return("knowledgebase/f.txt").Build()
			mockey.Mock((*ve_tos.Client).UploadFile).Return(assert.AnError).Build()
			err := v.AddFromFiles([]string{"f"})
			assert.NotNil(t, err)
		})
	})
}

func TestVikingKnowledgeBackend_AddFromDirectory(t *testing.T) {
	tosClient, _ := ve_tos.New(&ve_tos.Config{
		AK:       "ak",
		SK:       "sk",
		Region:   "cn-beijing",
		Endpoint: "https://xxxxxxx.com",
		Bucket:   "veadk-tests",
	})
	v := &VikingKnowledgeBackend{
		viking: &viking_knowledge.Client{},
		tos:    tosClient,
		config: &Config{},
	}
	mockey.PatchConvey("TestVikingKnowledgeBackend_AddFromDirectory", t, func() {
		dir := t.TempDir()
		p1 := filepath.Join(dir, "a.txt")
		p2 := filepath.Join(dir, "b.txt")
		assert.Nil(t, os.WriteFile(p1, []byte("a"), 0o644))
		assert.Nil(t, os.WriteFile(p2, []byte("b"), 0o644))
		mockey.Mock((*ve_tos.Client).BuildObjectKeyForFile).Return("knowledgebase/d.txt").Build()
		mockey.Mock((*ve_tos.Client).UploadFile).Return(nil).Build()
		mockey.Mock((*ve_tos.Client).BuildTOSURL).Return("bucket/knowledgebase/d.txt").Build()
		mockey.Mock((*viking_knowledge.Client).DocumentAddTOS).Return(&ve_viking.CommonResponse{Code: ve_viking.VikingKnowledgeBaseSuccessCode}, nil).Build()
		err := v.AddFromDirectory(dir)
		assert.Nil(t, err)
	})
}
