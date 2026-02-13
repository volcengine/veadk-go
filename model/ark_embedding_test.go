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

package model

import (
	"context"
	"fmt"
	"testing"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

func TestNewArkEmbeddingModel(t *testing.T) {
	t.Run("with_api_key", func(t *testing.T) {
		embedder, err := NewArkEmbeddingModel(context.Background(), "embedding-model", &ArkEmbeddingConfig{
			APIKey:  "test-key",
			BaseURL: "https://ark.example.com/api/v3/",
		})
		assert.NoError(t, err)
		assert.NotNil(t, embedder)
	})

	t.Run("with_ak_sk", func(t *testing.T) {
		embedder, err := NewArkEmbeddingModel(context.Background(), "embedding-model", &ArkEmbeddingConfig{
			AK: "test-ak",
			SK: "test-sk",
		})
		assert.NoError(t, err)
		assert.NotNil(t, embedder)
	})

	t.Run("no_auth", func(t *testing.T) {
		_, err := NewArkEmbeddingModel(context.Background(), "embedding-model", &ArkEmbeddingConfig{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API key or AK/SK pair is required")
	})

	t.Run("nil_config", func(t *testing.T) {
		_, err := NewArkEmbeddingModel(context.Background(), "embedding-model", nil)
		assert.Error(t, err)
	})
}

func TestArkEmbeddingModel_EmbedTexts(t *testing.T) {
	mockey.PatchConvey("single text embedding", t, func() {
		client := arkruntime.NewClientWithApiKey("test-key")
		em := &arkEmbeddingModel{
			name:   "embedding-model",
			config: &ArkEmbeddingConfig{APIKey: "test-key", Dimensions: 3},
			client: client,
		}

		mockey.Mock((*arkruntime.Client).CreateEmbeddings).Return(
			arkmodel.EmbeddingResponse{
				Model: "embedding-model",
				Data: []arkmodel.Embedding{
					{Index: 0, Embedding: []float32{0.1, 0.2, 0.3}},
				},
				Usage: arkmodel.Usage{
					PromptTokens: 5,
					TotalTokens:  5,
				},
			}, nil,
		).Build()

		resp, err := em.EmbedTexts(context.Background(), &EmbeddingRequest{
			Texts: []string{"hello world"},
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Len(t, resp.Embeddings, 1)
		assert.Equal(t, []float32{0.1, 0.2, 0.3}, resp.Embeddings[0])
		assert.Equal(t, "embedding-model", resp.Model)
		assert.Equal(t, 5, resp.Usage.PromptTokens)
		assert.Equal(t, 5, resp.Usage.TotalTokens)
	})

	mockey.PatchConvey("batch text embedding", t, func() {
		client := arkruntime.NewClientWithApiKey("test-key")
		em := &arkEmbeddingModel{
			name:   "embedding-model",
			config: &ArkEmbeddingConfig{APIKey: "test-key"},
			client: client,
		}

		mockey.Mock((*arkruntime.Client).CreateEmbeddings).Return(
			arkmodel.EmbeddingResponse{
				Model: "embedding-model",
				Data: []arkmodel.Embedding{
					{Index: 0, Embedding: []float32{0.1, 0.2, 0.3}},
					{Index: 1, Embedding: []float32{0.4, 0.5, 0.6}},
					{Index: 2, Embedding: []float32{0.7, 0.8, 0.9}},
				},
				Usage: arkmodel.Usage{
					PromptTokens: 15,
					TotalTokens:  15,
				},
			}, nil,
		).Build()

		resp, err := em.EmbedTexts(context.Background(), &EmbeddingRequest{
			Texts: []string{"one", "two", "three"},
		})
		assert.NoError(t, err)
		assert.Len(t, resp.Embeddings, 3)
		assert.Equal(t, []float32{0.4, 0.5, 0.6}, resp.Embeddings[1])
	})

	mockey.PatchConvey("embedding with dimensions override", t, func() {
		client := arkruntime.NewClientWithApiKey("test-key")
		em := &arkEmbeddingModel{
			name:   "embedding-model",
			config: &ArkEmbeddingConfig{APIKey: "test-key", Dimensions: 1024},
			client: client,
		}

		mockey.Mock((*arkruntime.Client).CreateEmbeddings).Return(
			arkmodel.EmbeddingResponse{
				Model: "embedding-model",
				Data: []arkmodel.Embedding{
					{Index: 0, Embedding: make([]float32, 512)},
				},
				Usage: arkmodel.Usage{PromptTokens: 5, TotalTokens: 5},
			}, nil,
		).Build()

		// Request-level dimensions override config dimensions
		resp, err := em.EmbedTexts(context.Background(), &EmbeddingRequest{
			Texts:      []string{"hello"},
			Dimensions: 512,
		})
		assert.NoError(t, err)
		assert.Len(t, resp.Embeddings[0], 512)
	})

	mockey.PatchConvey("embedding error", t, func() {
		client := arkruntime.NewClientWithApiKey("test-key")
		em := &arkEmbeddingModel{
			name:   "embedding-model",
			config: &ArkEmbeddingConfig{APIKey: "test-key"},
			client: client,
		}

		mockey.Mock((*arkruntime.Client).CreateEmbeddings).Return(
			arkmodel.EmbeddingResponse{}, fmt.Errorf("rate limit exceeded"),
		).Build()

		_, err := em.EmbedTexts(context.Background(), &EmbeddingRequest{
			Texts: []string{"hello"},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "rate limit exceeded")
	})
}

func TestArkEmbeddingModel_EmptyInput(t *testing.T) {
	em := &arkEmbeddingModel{
		name:   "embedding-model",
		config: &ArkEmbeddingConfig{APIKey: "test-key"},
	}

	_, err := em.EmbedTexts(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one text input is required")

	_, err = em.EmbedTexts(context.Background(), &EmbeddingRequest{Texts: []string{}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one text input is required")
}
