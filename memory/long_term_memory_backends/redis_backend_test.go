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

	"github.com/bytedance/mockey"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/model"
)

func TestNewRedisMemoryBackend(t *testing.T) {
	mockey.PatchConvey("TestNewRedisMemoryBackend", t, func() {
		mockey.PatchConvey("embedder creation failed", func() {
			mockey.Mock((*EmbeddingConfig).CreateEmbedder).Return(nil, errors.New("embedder error")).Build()
			backend, err := NewRedisMemoryBackend(&RedisMemoryConfig{
				Host: "localhost",
			})
			assert.Nil(t, backend)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "failed to create embedder")
		})

		mockey.PatchConvey("defaults applied", func() {
			config := &RedisMemoryConfig{Host: "localhost"}
			mockey.Mock((*EmbeddingConfig).CreateEmbedder).Return(nil, errors.New("stop")).Build()
			_, _ = NewRedisMemoryBackend(config)

			assert.Equal(t, DefaultRedisPort, config.Port)
			assert.Equal(t, DefaultRedisIndex, config.Index)
			assert.NotNil(t, config.EmbeddingConfig)
		})

		mockey.PatchConvey("ensureIndex failed", func() {
			mockEmbedder := &mockEmbedder{}
			mockey.Mock((*EmbeddingConfig).CreateEmbedder).Return(mockEmbedder, nil).Build()
			mockey.Mock((*RedisMemoryBackend).ensureIndex).Return(errors.New("index error")).Build()

			backend, err := NewRedisMemoryBackend(&RedisMemoryConfig{
				Host:            "localhost",
				EmbeddingConfig: &EmbeddingConfig{Dimensions: 1024},
			})
			assert.Nil(t, backend)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "index error")
		})

		mockey.PatchConvey("success", func() {
			mockEmbedder := &mockEmbedder{}
			mockey.Mock((*EmbeddingConfig).CreateEmbedder).Return(mockEmbedder, nil).Build()
			mockey.Mock((*RedisMemoryBackend).ensureIndex).Return(nil).Build()

			backend, err := NewRedisMemoryBackend(&RedisMemoryConfig{
				Host:            "localhost",
				EmbeddingConfig: &EmbeddingConfig{Dimensions: 1024},
			})
			assert.NotNil(t, backend)
			assert.Nil(t, err)
		})
	})
}

func TestRedisMemoryBackend_SaveMemory(t *testing.T) {
	mockey.PatchConvey("TestRedisMemoryBackend_SaveMemory", t, func() {
		mockEmb := &mockEmbedder{
			embedFunc: func(ctx context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
				embeddings := make([][]float32, len(req.Texts))
				for i := range embeddings {
					embeddings[i] = []float32{0.1, 0.2, 0.3}
				}
				return &model.EmbeddingResponse{Embeddings: embeddings}, nil
			},
		}
		backend := &RedisMemoryBackend{
			config:   &RedisMemoryConfig{Index: DefaultRedisIndex},
			client:   redis.NewClient(&redis.Options{Addr: "localhost:6379"}),
			embedder: mockEmb,
		}

		mockey.PatchConvey("empty event list", func() {
			err := backend.SaveMemory(context.Background(), "user1", []string{})
			assert.Nil(t, err)
		})

		mockey.PatchConvey("embed failed", func() {
			backend.embedder = &mockEmbedder{
				embedFunc: func(ctx context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
					return nil, errors.New("embed error")
				},
			}
			err := backend.SaveMemory(context.Background(), "user1", []string{"event1"})
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "failed to embed texts")
		})

		mockey.PatchConvey("success", func() {
			mockey.Mock((*redis.Pipeline).Exec).Return(nil, nil).Build()
			mockey.Mock((*redis.Pipeline).HSet).Return(redis.NewIntCmd(context.Background())).Build()
			err := backend.SaveMemory(context.Background(), "user1", []string{"event1", "event2"})
			assert.Nil(t, err)
		})
	})
}

func TestRedisMemoryBackend_SearchMemory(t *testing.T) {
	mockey.PatchConvey("TestRedisMemoryBackend_SearchMemory", t, func() {
		mockEmb := &mockEmbedder{
			embedFunc: func(ctx context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
				return &model.EmbeddingResponse{
					Embeddings: [][]float32{{0.1, 0.2, 0.3}},
				}, nil
			},
		}
		backend := &RedisMemoryBackend{
			config:   &RedisMemoryConfig{Index: DefaultRedisIndex},
			client:   redis.NewClient(&redis.Options{Addr: "localhost:6379"}),
			embedder: mockEmb,
		}

		mockey.PatchConvey("embed failed", func() {
			backend.embedder = &mockEmbedder{
				embedFunc: func(ctx context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
					return nil, errors.New("embed error")
				},
			}
			results, err := backend.SearchMemory(context.Background(), "user1", "query", 5)
			assert.Nil(t, results)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "failed to embed query")
		})
	})
}

func TestParseRedisSearchResults(t *testing.T) {
	t.Run("empty results", func(t *testing.T) {
		items := parseRedisSearchResults([]interface{}{})
		assert.Nil(t, items)
	})

	t.Run("valid results", func(t *testing.T) {
		results := []interface{}{
			int64(2), // total count
			"key1",
			[]interface{}{"text", "hello world", "timestamp", "1700000000000"},
			"key2",
			[]interface{}{"text", "second item", "timestamp", "1700000001000"},
		}
		items := parseRedisSearchResults(results)
		assert.Equal(t, 2, len(items))
		assert.Equal(t, "hello world", items[0].Content)
		assert.Equal(t, "second item", items[1].Content)
	})

	t.Run("missing text field", func(t *testing.T) {
		results := []interface{}{
			int64(1),
			"key1",
			[]interface{}{"timestamp", "1700000000000"},
		}
		items := parseRedisSearchResults(results)
		assert.Equal(t, 0, len(items))
	})
}

func TestFloat32SliceToBytes(t *testing.T) {
	vec := []float32{1.0, 2.0, 3.0}
	b := float32SliceToBytes(vec)
	assert.Equal(t, 12, len(b)) // 3 floats * 4 bytes
}

func TestEscapeRedisTag(t *testing.T) {
	assert.Equal(t, `hello`, escapeRedisTag("hello"))
	assert.Equal(t, `veadk\-ltm`, escapeRedisTag("veadk-ltm"))
	assert.Equal(t, `user\.name`, escapeRedisTag("user.name"))
	assert.Equal(t, `a\:b\/c`, escapeRedisTag("a:b/c"))
}

// mockEmbedder is a test helper implementing model.Embedder.
type mockEmbedder struct {
	embedFunc func(ctx context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error)
}

func (m *mockEmbedder) EmbedTexts(ctx context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, req)
	}
	return &model.EmbeddingResponse{}, nil
}
