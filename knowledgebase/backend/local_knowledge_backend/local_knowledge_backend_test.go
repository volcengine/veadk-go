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

package local_knowledge_backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewLocalKnowledgeBackendDefaults(t *testing.T) {
	backend, err := NewLocalKnowledgeBackend(nil)
	assert.Nil(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, DefaultIndex, backend.Index())
}

func TestLocalKnowledgeBackendAddFromTextAndSearch(t *testing.T) {
	backend, err := NewLocalKnowledgeBackend(&Config{Index: "test-index", TopK: 1})
	assert.Nil(t, err)

	err = backend.AddFromText([]string{
		"Banana bread recipe with cinnamon.",
		"The Volcengine Agent Development Kit builds agents.",
	}, map[string]any{"metadata": map[string]any{"tenant": "test"}})
	assert.Nil(t, err)

	results, err := backend.Search("agent kit")
	assert.Nil(t, err)
	assert.Len(t, results, 1)
	assert.Contains(t, results[0].Content, "Agent Development Kit")
	assert.Equal(t, "test", results[0].Metadata[0]["tenant"])
	assert.Equal(t, "text", results[0].Metadata[1]["source"])
}

func TestLocalKnowledgeBackendSearchTopK(t *testing.T) {
	backend, err := NewLocalKnowledgeBackend(&Config{TopK: 3})
	assert.Nil(t, err)

	err = backend.AddFromText([]string{
		"agent alpha",
		"agent beta",
		"agent gamma",
	})
	assert.Nil(t, err)

	results, err := backend.Search("agent", map[string]any{"top_k": 2})
	assert.Nil(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "agent alpha", results[0].Content)
	assert.Equal(t, "agent beta", results[1].Content)
}

func TestLocalKnowledgeBackendAddFromFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "agent.txt")
	err := os.WriteFile(file, []byte("local knowledge file for agent tools"), 0600)
	assert.Nil(t, err)

	backend, err := NewLocalKnowledgeBackend(nil)
	assert.Nil(t, err)

	err = backend.AddFromFiles([]string{file})
	assert.Nil(t, err)

	results, err := backend.Search("agent tools")
	assert.Nil(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "local knowledge file for agent tools", results[0].Content)
	assert.Equal(t, "file", results[0].Metadata[0]["source"])
	assert.Equal(t, file, results[0].Metadata[0]["file_path"])
}

func TestLocalKnowledgeBackendAddFromDirectory(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("alpha knowledge"), 0600)
	assert.Nil(t, err)
	nested := filepath.Join(dir, "nested")
	err = os.Mkdir(nested, 0700)
	assert.Nil(t, err)
	err = os.WriteFile(filepath.Join(nested, "beta.txt"), []byte("beta knowledge"), 0600)
	assert.Nil(t, err)

	backend, err := NewLocalKnowledgeBackend(nil)
	assert.Nil(t, err)

	err = backend.AddFromDirectory(dir)
	assert.Nil(t, err)

	results, err := backend.Search("beta")
	assert.Nil(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "beta knowledge", results[0].Content)
}

func TestLocalKnowledgeBackendAddErrors(t *testing.T) {
	backend, err := NewLocalKnowledgeBackend(nil)
	assert.Nil(t, err)

	err = backend.AddFromFiles([]string{filepath.Join(t.TempDir(), "missing.txt")})
	assert.NotNil(t, err)
	assert.True(t, errors.Is(err, ErrLocalKnowledgeBackend))

	file := filepath.Join(t.TempDir(), "not-dir.txt")
	err = os.WriteFile(file, []byte("content"), 0600)
	assert.Nil(t, err)
	err = backend.AddFromDirectory(file)
	assert.NotNil(t, err)
	assert.True(t, errors.Is(err, ErrLocalKnowledgeBackend))
}

func TestLocalKnowledgeBackendEmptySearch(t *testing.T) {
	backend, err := NewLocalKnowledgeBackend(nil)
	assert.Nil(t, err)

	results, err := backend.Search("   ")
	assert.Nil(t, err)
	assert.Empty(t, results)
}

func TestLocalKnowledgeBackendSearchWithEmbedder(t *testing.T) {
	embedder := &mockEmbedder{
		vectors: map[string][]float32{
			"cat document": {1, 0},
			"dog document": {0, 1},
			"bark":         {0, 1},
		},
	}
	backend, err := NewLocalKnowledgeBackend(&Config{Embedder: embedder})
	assert.Nil(t, err)

	err = backend.AddFromText([]string{"cat document", "dog document"})
	assert.Nil(t, err)

	results, err := backend.Search("bark", map[string]any{"topK": 1})
	assert.Nil(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "dog document", results[0].Content)
}

func TestLocalKnowledgeBackendEmbedderError(t *testing.T) {
	backend, err := NewLocalKnowledgeBackend(&Config{
		Embedder: &mockEmbedder{err: errors.New("embed failed")},
	})
	assert.Nil(t, err)

	err = backend.AddFromText([]string{"agent document"})
	assert.NotNil(t, err)
	assert.True(t, errors.Is(err, ErrLocalKnowledgeBackend))
}

func TestLocalKnowledgeBackendInvalidEmbeddingResponse(t *testing.T) {
	backend, err := NewLocalKnowledgeBackend(&Config{
		Embedder: &mockEmbedder{vectors: map[string][]float32{}},
	})
	assert.Nil(t, err)

	err = backend.AddFromText([]string{"agent document"})
	assert.NotNil(t, err)
	assert.True(t, errors.Is(err, ErrInvalidEmbedding))
}

type mockEmbedder struct {
	vectors map[string][]float32
	err     error
}

func (m *mockEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	_ = ctx
	if m.err != nil {
		return nil, m.err
	}
	embeddings := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vector, ok := m.vectors[text]
		if !ok {
			continue
		}
		embeddings = append(embeddings, vector)
	}
	return embeddings, nil
}
