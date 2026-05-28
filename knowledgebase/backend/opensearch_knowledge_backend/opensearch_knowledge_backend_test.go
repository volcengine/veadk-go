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

package opensearch_knowledge_backend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/model"
)

func TestOpenSearchKnowledgeBackend_New(t *testing.T) {
	mockey.PatchConvey("TestOpenSearchKnowledgeBackend_New", t, func() {
		mockey.PatchConvey("embedder creation failed", func() {
			mockey.Mock(model.NewArkEmbeddingModel).Return(nil, errors.New("embedder error")).Build()
			backend, err := NewOpenSearchKnowledgeBackend(&Config{Host: "localhost"})
			assert.Nil(t, backend)
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), "create embedder")
		})

		mockey.PatchConvey("defaults applied", func() {
			cfg := &Config{Host: "localhost"}
			mockey.Mock(model.NewArkEmbeddingModel).Return(nil, errors.New("stop")).Build()
			_, _ = NewOpenSearchKnowledgeBackend(cfg)

			assert.Equal(t, DefaultOpenSearchPort, cfg.Port)
			assert.Equal(t, DefaultOpenSearchIndex, cfg.Index)
			assert.Equal(t, DefaultTopK, cfg.TopK)
			assert.NotZero(t, cfg.EmbeddingDim)
		})

		mockey.PatchConvey("success with ssl", func() {
			mockey.Mock(model.NewArkEmbeddingModel).Return(&mockOpenSearchEmbedder{}, nil).Build()
			backend, err := NewOpenSearchKnowledgeBackend(&Config{
				Host:             "localhost",
				UseSSL:           true,
				EmbeddingModel:   "embedding",
				EmbeddingAPIKey:  "key",
				EmbeddingBaseURL: "https://ark.example",
				EmbeddingDim:     3,
			})
			assert.Nil(t, err)
			assert.NotNil(t, backend)

			osBackend := backend.(*OpenSearchKnowledgeBackend)
			assert.Equal(t, "https://localhost:9200", osBackend.baseURL)
		})

		mockey.PatchConvey("success without ssl", func() {
			mockey.Mock(model.NewArkEmbeddingModel).Return(&mockOpenSearchEmbedder{}, nil).Build()
			backend, err := NewOpenSearchKnowledgeBackend(&Config{
				Host:            "localhost",
				Port:            9201,
				EmbeddingModel:  "embedding",
				EmbeddingAPIKey: "key",
				EmbeddingDim:    3,
			})
			assert.Nil(t, err)
			assert.NotNil(t, backend)

			osBackend := backend.(*OpenSearchKnowledgeBackend)
			assert.Equal(t, "http://localhost:9201", osBackend.baseURL)
		})

		mockey.PatchConvey("invalid index name", func() {
			backend, err := NewOpenSearchKnowledgeBackend(&Config{
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

func TestOpenSearchKnowledgeBackend_ValidateIndexName(t *testing.T) {
	tests := []struct {
		name    string
		index   string
		wantErr bool
	}{
		{"valid lowercase", "veadk_knowledge", false},
		{"valid with dash", "veadk-knowledge", false},
		{"valid with dot", "veadk.knowledge", false},
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

func TestOpenSearchKnowledgeBackend_AddFromText(t *testing.T) {
	mockey.PatchConvey("TestOpenSearchKnowledgeBackend_AddFromText", t, func() {
		backend := newTestOpenSearchBackend()
		callCount := 0
		var bulkBody []byte
		mockey.Mock((*OpenSearchKnowledgeBackend).doRequest).To(func(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
			_ = ctx
			callCount++
			if callCount == 1 {
				assert.Equal(t, http.MethodPut, method)
				assert.Equal(t, "/test_knowledge", path)
			} else {
				assert.Equal(t, http.MethodPost, method)
				assert.Equal(t, "/_bulk", path)
				bulkBody = append([]byte(nil), body...)
			}
			return okResponse(`{"acknowledged":true}`), nil
		}).Build()

		err := backend.AddFromText([]string{"agent knowledge", "   "}, map[string]any{"metadata": map[string]any{"tenant": "test"}})
		assert.Nil(t, err)
		assert.Equal(t, 2, callCount)
		assert.Contains(t, string(bulkBody), `"text":"agent knowledge"`)
		assert.Contains(t, string(bulkBody), `"tenant":"test"`)
		assert.Contains(t, string(bulkBody), `"source":"text"`)
	})
}

func TestOpenSearchKnowledgeBackend_AddFromFiles(t *testing.T) {
	mockey.PatchConvey("TestOpenSearchKnowledgeBackend_AddFromFiles", t, func() {
		dir := t.TempDir()
		file := filepath.Join(dir, "agent.txt")
		err := os.WriteFile(file, []byte("file knowledge"), 0600)
		assert.Nil(t, err)

		backend := newTestOpenSearchBackend()
		var bulkBody []byte
		mockey.Mock((*OpenSearchKnowledgeBackend).doRequest).To(func(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
			_ = ctx
			if path == "/_bulk" {
				bulkBody = append([]byte(nil), body...)
			}
			return okResponse(`{"acknowledged":true}`), nil
		}).Build()

		err = backend.AddFromFiles([]string{file})
		assert.Nil(t, err)
		assert.Contains(t, string(bulkBody), `"text":"file knowledge"`)
		assert.Contains(t, string(bulkBody), `"source":"file"`)
		assert.Contains(t, string(bulkBody), `"file_path":"`+file+`"`)
	})
}

func TestOpenSearchKnowledgeBackend_AddFromDirectory(t *testing.T) {
	mockey.PatchConvey("TestOpenSearchKnowledgeBackend_AddFromDirectory", t, func() {
		dir := t.TempDir()
		err := os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("alpha knowledge"), 0600)
		assert.Nil(t, err)
		nested := filepath.Join(dir, "nested")
		err = os.Mkdir(nested, 0700)
		assert.Nil(t, err)
		err = os.WriteFile(filepath.Join(nested, "beta.txt"), []byte("beta knowledge"), 0600)
		assert.Nil(t, err)

		backend := newTestOpenSearchBackend()
		var bulkBody []byte
		mockey.Mock((*OpenSearchKnowledgeBackend).doRequest).To(func(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
			_ = ctx
			if path == "/_bulk" {
				bulkBody = append([]byte(nil), body...)
			}
			return okResponse(`{"acknowledged":true}`), nil
		}).Build()

		err = backend.AddFromDirectory(dir)
		assert.Nil(t, err)
		assert.Contains(t, string(bulkBody), `"text":"alpha knowledge"`)
		assert.Contains(t, string(bulkBody), `"text":"beta knowledge"`)
	})
}

func TestOpenSearchKnowledgeBackend_Search(t *testing.T) {
	mockey.PatchConvey("TestOpenSearchKnowledgeBackend_Search", t, func() {
		backend := newTestOpenSearchBackend()
		var searchBody map[string]any
		responseBody := `{
			"hits": {
				"hits": [
					{"_source": {"text": "knowledge 1", "metadata": [{"tenant": "test"}, {"source": "text"}]}},
					{"_source": {"text": "knowledge 2", "metadata": [{"source": "file"}]}}
				]
			}
		}`
		mockey.Mock((*OpenSearchKnowledgeBackend).doRequest).To(func(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
			_ = ctx
			assert.Equal(t, http.MethodPost, method)
			assert.Equal(t, "/test_knowledge/_search", path)
			err := json.Unmarshal(body, &searchBody)
			assert.Nil(t, err)
			return okResponse(responseBody), nil
		}).Build()

		results, err := backend.Search("agent", map[string]any{"top_k": 2})
		assert.Nil(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, float64(2), searchBody["size"])
		assert.Equal(t, "knowledge 1", results[0].Content)
		assert.Equal(t, "test", results[0].Metadata[0]["tenant"])
		assert.Equal(t, "knowledge 2", results[1].Content)
	})
}

func TestOpenSearchKnowledgeBackend_ErrorPaths(t *testing.T) {
	mockey.PatchConvey("TestOpenSearchKnowledgeBackend_ErrorPaths", t, func() {
		mockey.PatchConvey("invalid index", func() {
			backend := newTestOpenSearchBackend()
			backend.config.Index = "Invalid"
			err := backend.AddFromText([]string{"agent"})
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrInvalidIndexName)
		})

		mockey.PatchConvey("missing file", func() {
			backend := newTestOpenSearchBackend()
			err := backend.AddFromFiles([]string{filepath.Join(t.TempDir(), "missing.txt")})
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrOpenSearchKnowledgeBackend)
		})

		mockey.PatchConvey("directory is file", func() {
			file := filepath.Join(t.TempDir(), "not-dir.txt")
			err := os.WriteFile(file, []byte("content"), 0600)
			assert.Nil(t, err)

			backend := newTestOpenSearchBackend()
			err = backend.AddFromDirectory(file)
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrOpenSearchKnowledgeBackend)
		})

		mockey.PatchConvey("ensure index failed", func() {
			backend := newTestOpenSearchBackend()
			mockey.Mock((*OpenSearchKnowledgeBackend).doRequest).Return(&http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader(`{"error":"boom"}`)),
			}, nil).Build()

			err := backend.AddFromText([]string{"agent"})
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrOpenSearchKnowledgeBackend)
		})

		mockey.PatchConvey("embed documents failed", func() {
			backend := newTestOpenSearchBackend()
			backend.embedder = &mockOpenSearchEmbedder{err: errors.New("embed error")}
			mockey.Mock((*OpenSearchKnowledgeBackend).doRequest).Return(okResponse(`{"acknowledged":true}`), nil).Build()

			err := backend.AddFromText([]string{"agent"})
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrOpenSearchKnowledgeBackend)
		})

		mockey.PatchConvey("invalid embedding response", func() {
			backend := newTestOpenSearchBackend()
			backend.embedder = &mockOpenSearchEmbedder{embeddings: map[string][]float32{}}
			mockey.Mock((*OpenSearchKnowledgeBackend).doRequest).Return(okResponse(`{"acknowledged":true}`), nil).Build()

			err := backend.AddFromText([]string{"agent"})
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrInvalidEmbedding)
		})

		mockey.PatchConvey("bulk failed", func() {
			backend := newTestOpenSearchBackend()
			callCount := 0
			mockey.Mock((*OpenSearchKnowledgeBackend).doRequest).To(func(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
				_ = ctx
				_ = method
				_ = path
				_ = body
				callCount++
				if callCount == 1 {
					return okResponse(`{"acknowledged":true}`), nil
				}
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader(`{"error":"bulk"}`)),
				}, nil
			}).Build()

			err := backend.AddFromText([]string{"agent"})
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrOpenSearchKnowledgeBackend)
		})

		mockey.PatchConvey("search index not found", func() {
			backend := newTestOpenSearchBackend()
			mockey.Mock((*OpenSearchKnowledgeBackend).doRequest).Return(&http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"index_not_found_exception"}}`)),
			}, nil).Build()

			results, err := backend.Search("agent")
			assert.Nil(t, err)
			assert.Empty(t, results)
		})

		mockey.PatchConvey("search failed", func() {
			backend := newTestOpenSearchBackend()
			mockey.Mock((*OpenSearchKnowledgeBackend).doRequest).Return(&http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader(`{"error":"search"}`)),
			}, nil).Build()

			results, err := backend.Search("agent")
			assert.Nil(t, results)
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrOpenSearchKnowledgeBackend)
		})

		mockey.PatchConvey("search parse failed", func() {
			backend := newTestOpenSearchBackend()
			mockey.Mock((*OpenSearchKnowledgeBackend).doRequest).Return(okResponse(`invalid`), nil).Build()

			results, err := backend.Search("agent")
			assert.Nil(t, results)
			assert.NotNil(t, err)
			assert.ErrorIs(t, err, ErrOpenSearchKnowledgeBackend)
		})
	})
}

type mockOpenSearchEmbedder struct {
	embeddings map[string][]float32
	err        error
}

func (m *mockOpenSearchEmbedder) EmbedTexts(ctx context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
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

func newTestOpenSearchBackend() *OpenSearchKnowledgeBackend {
	return &OpenSearchKnowledgeBackend{
		config: &Config{
			Host:         "localhost",
			Port:         DefaultOpenSearchPort,
			Index:        "test_knowledge",
			TopK:         DefaultTopK,
			EmbeddingDim: 3,
		},
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    "http://localhost:9200",
		embedder:   &mockOpenSearchEmbedder{},
	}
}

func okResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
