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
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/model"
)

func TestNewOpenSearchMemoryBackend(t *testing.T) {
	mockey.PatchConvey("TestNewOpenSearchMemoryBackend", t, func() {
		mockey.PatchConvey("embedder creation failed", func() {
			mockey.Mock((*EmbeddingConfig).CreateEmbedder).Return(nil, errors.New("embedder error")).Build()
			backend, err := NewOpenSearchMemoryBackend(&OpenSearchMemoryConfig{
				Host: "localhost",
			})
			assert.Nil(t, backend)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "failed to create embedder")
		})

		mockey.PatchConvey("defaults applied", func() {
			config := &OpenSearchMemoryConfig{Host: "localhost"}
			mockey.Mock((*EmbeddingConfig).CreateEmbedder).Return(nil, errors.New("stop")).Build()
			_, _ = NewOpenSearchMemoryBackend(config)

			assert.Equal(t, DefaultOpenSearchPort, config.Port)
			assert.Equal(t, DefaultOpenSearchIndex, config.Index)
			assert.NotNil(t, config.EmbeddingConfig)
		})

		mockey.PatchConvey("success with SSL", func() {
			mockEmb := &mockEmbedder{}
			mockey.Mock((*EmbeddingConfig).CreateEmbedder).Return(mockEmb, nil).Build()

			backend, err := NewOpenSearchMemoryBackend(&OpenSearchMemoryConfig{
				Host:            "localhost",
				UseSSL:          true,
				EmbeddingConfig: &EmbeddingConfig{Dimensions: 1024},
			})
			assert.NotNil(t, backend)
			assert.Nil(t, err)

			osBackend := backend.(*OpenSearchMemoryBackend)
			assert.Equal(t, "https://localhost:9200", osBackend.baseURL)
		})

		mockey.PatchConvey("success without SSL", func() {
			mockEmb := &mockEmbedder{}
			mockey.Mock((*EmbeddingConfig).CreateEmbedder).Return(mockEmb, nil).Build()

			backend, err := NewOpenSearchMemoryBackend(&OpenSearchMemoryConfig{
				Host:            "localhost",
				Port:            9201,
				EmbeddingConfig: &EmbeddingConfig{Dimensions: 1024},
			})
			assert.NotNil(t, backend)
			assert.Nil(t, err)

			osBackend := backend.(*OpenSearchMemoryBackend)
			assert.Equal(t, "http://localhost:9201", osBackend.baseURL)
		})
	})
}

func TestValidateIndexName(t *testing.T) {
	tests := []struct {
		name    string
		index   string
		wantErr bool
	}{
		{"valid lowercase", "veadk_ltm_user1", false},
		{"valid with dash", "veadk-ltm-user1", false},
		{"valid with dot", "veadk.ltm", false},
		{"starts with underscore", "_invalid", true},
		{"starts with dash", "-invalid", true},
		{"uppercase", "INVALID", true},
		{"empty", "", true},
		{"special chars", "invalid@name", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIndexName(tt.index)
			if tt.wantErr {
				assert.NotNil(t, err)
				assert.ErrorIs(t, err, ErrInvalidIndexName)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

func TestOpenSearchMemoryBackend_SaveMemory(t *testing.T) {
	mockey.PatchConvey("TestOpenSearchMemoryBackend_SaveMemory", t, func() {
		mockEmb := &mockEmbedder{
			embedFunc: func(ctx context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
				embeddings := make([][]float32, len(req.Texts))
				for i := range embeddings {
					embeddings[i] = []float32{0.1, 0.2, 0.3}
				}
				return &model.EmbeddingResponse{Embeddings: embeddings}, nil
			},
		}
		backend := &OpenSearchMemoryBackend{
			config: &OpenSearchMemoryConfig{
				Index:           DefaultOpenSearchIndex,
				EmbeddingConfig: &EmbeddingConfig{Dimensions: 3},
			},
			httpClient: &http.Client{Timeout: 5 * time.Second},
			baseURL:    "http://localhost:9200",
			embedder:   mockEmb,
		}

		mockey.PatchConvey("empty event list", func() {
			err := backend.SaveMemory(context.Background(), "user1", []string{})
			assert.Nil(t, err)
		})

		mockey.PatchConvey("invalid index name", func() {
			err := backend.SaveMemory(context.Background(), "USER_UPPER", []string{"event1"})
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrInvalidIndexName)
		})

		mockey.PatchConvey("embed failed", func() {
			backend.embedder = &mockEmbedder{
				embedFunc: func(ctx context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
					return nil, errors.New("embed error")
				},
			}
			mockey.Mock((*OpenSearchMemoryBackend).ensureIndex).Return(nil).Build()
			err := backend.SaveMemory(context.Background(), "user1", []string{"event1"})
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "failed to embed texts")
		})

		mockey.PatchConvey("success", func() {
			mockey.Mock((*OpenSearchMemoryBackend).ensureIndex).Return(nil).Build()
			mockey.Mock((*OpenSearchMemoryBackend).doRequest).Return(&http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"errors":false}`)),
			}, nil).Build()

			err := backend.SaveMemory(context.Background(), "user1", []string{"event1", "event2"})
			assert.Nil(t, err)
		})
	})
}

func TestOpenSearchMemoryBackend_SearchMemory(t *testing.T) {
	mockey.PatchConvey("TestOpenSearchMemoryBackend_SearchMemory", t, func() {
		mockEmb := &mockEmbedder{
			embedFunc: func(ctx context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
				return &model.EmbeddingResponse{
					Embeddings: [][]float32{{0.1, 0.2, 0.3}},
				}, nil
			},
		}
		backend := &OpenSearchMemoryBackend{
			config: &OpenSearchMemoryConfig{
				Index:           DefaultOpenSearchIndex,
				EmbeddingConfig: &EmbeddingConfig{Dimensions: 3},
			},
			httpClient: &http.Client{Timeout: 5 * time.Second},
			baseURL:    "http://localhost:9200",
			embedder:   mockEmb,
		}

		mockey.PatchConvey("invalid index name", func() {
			results, err := backend.SearchMemory(context.Background(), "USER_UPPER", "query", 5)
			assert.Nil(t, results)
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrInvalidIndexName)
		})

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

		mockey.PatchConvey("success", func() {
			responseBody := `{
				"hits": {
					"hits": [
						{"_source": {"text": "memory 1", "timestamp": 1700000000000}},
						{"_source": {"text": "memory 2", "timestamp": 1700000001000}}
					]
				}
			}`
			mockey.Mock((*OpenSearchMemoryBackend).doRequest).Return(&http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(responseBody)),
			}, nil).Build()

			results, err := backend.SearchMemory(context.Background(), "user1", "query", 5)
			assert.Nil(t, err)
			assert.Equal(t, 2, len(results))
			assert.Equal(t, "memory 1", results[0].Content)
			assert.Equal(t, "memory 2", results[1].Content)
		})

		mockey.PatchConvey("index not found", func() {
			mockey.Mock((*OpenSearchMemoryBackend).doRequest).Return(&http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"index_not_found_exception"}}`)),
			}, nil).Build()

			results, err := backend.SearchMemory(context.Background(), "user1", "query", 5)
			assert.Nil(t, err)
			assert.Nil(t, results)
		})
	})
}

func TestParseOpenSearchResults(t *testing.T) {
	t.Run("valid results", func(t *testing.T) {
		body := `{
			"hits": {
				"hits": [
					{"_source": {"text": "hello", "timestamp": 1700000000000}},
					{"_source": {"text": "world", "timestamp": 1700000001000}}
				]
			}
		}`
		items, err := parseOpenSearchResults([]byte(body))
		assert.Nil(t, err)
		assert.Equal(t, 2, len(items))
		assert.Equal(t, "hello", items[0].Content)
		assert.Equal(t, "world", items[1].Content)
	})

	t.Run("empty hits", func(t *testing.T) {
		body := `{"hits": {"hits": []}}`
		items, err := parseOpenSearchResults([]byte(body))
		assert.Nil(t, err)
		assert.Equal(t, 0, len(items))
	})

	t.Run("invalid json", func(t *testing.T) {
		items, err := parseOpenSearchResults([]byte("invalid"))
		assert.NotNil(t, err)
		assert.Nil(t, items)
	})
}
