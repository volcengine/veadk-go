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

package redis_knowledge_backend

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bytedance/mockey"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/model"
)

func TestRedisKnowledgeBackend_New(t *testing.T) {
	mockey.PatchConvey("TestRedisKnowledgeBackend_New", t, func() {
		mockey.PatchConvey("embedder creation failed", func() {
			mockey.Mock(model.NewArkEmbeddingModel).Return(nil, errors.New("embedder error")).Build()
			backend, err := NewRedisKnowledgeBackend(&Config{Host: "localhost"})
			assert.Nil(t, backend)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "create embedder")
		})

		mockey.PatchConvey("defaults applied", func() {
			cfg := &Config{Host: "localhost"}
			mockey.Mock(model.NewArkEmbeddingModel).Return(nil, errors.New("stop")).Build()
			_, _ = NewRedisKnowledgeBackend(cfg)

			assert.Equal(t, DefaultRedisPort, cfg.Port)
			assert.Equal(t, DefaultRedisIndex, cfg.Index)
			assert.Equal(t, DefaultTopK, cfg.TopK)
			assert.NotZero(t, cfg.EmbeddingDim)
		})

		mockey.PatchConvey("invalid index name", func() {
			backend, err := NewRedisKnowledgeBackend(&Config{
				Host:            "localhost",
				Index:           "Invalid",
				EmbeddingAPIKey: "key",
				EmbeddingDim:    3,
			})
			assert.Nil(t, backend)
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrInvalidIndexName)
		})
	})
}

func TestRedisKnowledgeBackend_AddFromText(t *testing.T) {
	mockey.PatchConvey("TestRedisKnowledgeBackend_AddFromText", t, func() {
		backend := newTestRedisBackend()
		calls := make([][]any, 0)
		mockey.Mock((*RedisKnowledgeBackend).do).To(func(ctx context.Context, args ...any) *redis.Cmd {
			_ = ctx
			calls = append(calls, append([]any(nil), args...))
			return redisCmd("OK", nil)
		}).Build()

		err := backend.AddFromText([]string{"agent knowledge", "   "})
		assert.Nil(t, err)
		assert.Len(t, calls, 2)

		createArgs := calls[0]
		assert.Equal(t, "FT.CREATE", createArgs[0])
		assert.Equal(t, "test_knowledge", createArgs[1])
		assert.Contains(t, createArgs, "HNSW")
		assert.Contains(t, createArgs, "DIM")
		assert.Contains(t, createArgs, 3)

		hsetArgs := calls[1]
		assert.Equal(t, "HSET", hsetArgs[0])
		key, ok := hsetArgs[1].(string)
		assert.True(t, ok)
		assert.True(t, strings.HasPrefix(key, "test_knowledge:"))
		assert.Equal(t, "content", hsetArgs[2])
		assert.Equal(t, "agent knowledge", hsetArgs[3])
		assert.Equal(t, "vector", hsetArgs[4])
		vectorBytes, ok := hsetArgs[5].([]byte)
		assert.True(t, ok)
		assert.Len(t, vectorBytes, 12)
	})
}

func TestRedisKnowledgeBackend_Search(t *testing.T) {
	mockey.PatchConvey("TestRedisKnowledgeBackend_Search", t, func() {
		backend := newTestRedisBackend()
		var searchArgs []any
		mockey.Mock((*RedisKnowledgeBackend).do).To(func(ctx context.Context, args ...any) *redis.Cmd {
			_ = ctx
			searchArgs = append([]any(nil), args...)
			return redisCmd([]interface{}{
				int64(2),
				"test_knowledge:1",
				[]interface{}{"content", "knowledge 1", "score", "0.1"},
				"test_knowledge:2",
				[]interface{}{"content", "knowledge 2", "score", "0.2"},
			}, nil)
		}).Build()

		results, err := backend.Search("agent", map[string]any{"top_k": 2})
		assert.Nil(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "knowledge 1", results[0].Content)
		assert.Equal(t, "knowledge 2", results[1].Content)

		assert.Equal(t, "FT.SEARCH", searchArgs[0])
		assert.Equal(t, "test_knowledge", searchArgs[1])
		assert.Equal(t, "*=>[KNN 2 @vector $vec AS score]", searchArgs[2])
		assert.Equal(t, "PARAMS", searchArgs[3])
		assert.Equal(t, "2", searchArgs[4])
		assert.Equal(t, "vec", searchArgs[5])
		vectorBytes, ok := searchArgs[6].([]byte)
		assert.True(t, ok)
		assert.Len(t, vectorBytes, 12)
		assert.Contains(t, searchArgs, "RETURN")
		assert.Contains(t, searchArgs, "content")
		assert.Contains(t, searchArgs, "score")
	})
}

func TestRedisKnowledgeBackend_ErrorPaths(t *testing.T) {
	mockey.PatchConvey("TestRedisKnowledgeBackend_ErrorPaths", t, func() {
		mockey.PatchConvey("invalid index", func() {
			backend := newTestRedisBackend()
			backend.config.Index = "Invalid"
			err := backend.AddFromText([]string{"agent"})
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrInvalidIndexName)
		})

		mockey.PatchConvey("embedding dimension mismatch", func() {
			backend := newTestRedisBackend()
			backend.embedder = &mockRedisEmbedder{
				embeddings: map[string][]float32{
					"agent": {0.1, 0.2},
				},
			}
			mockey.Mock((*RedisKnowledgeBackend).do).Return(redisCmd("OK", nil)).Build()

			err := backend.AddFromText([]string{"agent"})
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrInvalidEmbedding)
		})
	})
}

type mockRedisEmbedder struct {
	embeddings map[string][]float32
	err        error
}

func (m *mockRedisEmbedder) EmbedTexts(ctx context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
	_ = ctx
	if m.err != nil {
		return nil, m.err
	}
	embeddings := make([][]float32, 0, len(req.Texts))
	for i, text := range req.Texts {
		if m.embeddings != nil {
			vector, ok := m.embeddings[text]
			if !ok {
				continue
			}
			embeddings = append(embeddings, vector)
			continue
		}
		embeddings = append(embeddings, []float32{float32(i) + 0.1, 0.2, 0.3})
	}
	return &model.EmbeddingResponse{Embeddings: embeddings}, nil
}

func newTestRedisBackend() *RedisKnowledgeBackend {
	return &RedisKnowledgeBackend{
		config: &Config{
			Host:         "localhost",
			Port:         DefaultRedisPort,
			Index:        "test_knowledge",
			TopK:         DefaultTopK,
			EmbeddingDim: 3,
		},
		embedder: &mockRedisEmbedder{},
	}
}

func redisCmd(val any, err error) *redis.Cmd {
	cmd := redis.NewCmd(context.Background())
	if val != nil {
		cmd.SetVal(val)
	}
	if err != nil {
		cmd.SetErr(err)
	}
	return cmd
}
