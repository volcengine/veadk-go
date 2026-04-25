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
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	_interface "github.com/volcengine/veadk-go/knowledgebase/interface"
	"github.com/volcengine/veadk-go/knowledgebase/ktypes"
)

const (
	DefaultIndex = "local_knowledge_base"
	DefaultTopK  = 5
)

var (
	ErrLocalKnowledgeBackend = errors.New("local knowledge backend error")
	ErrInvalidEmbedding      = errors.New("invalid embedding response")
)

type Config struct {
	Index    string
	TopK     int
	Embedder Embedder
}

type Embedder interface {
	EmbedTexts(ctx context.Context, texts []string) ([][]float32, error)
}

type LocalKnowledgeBackend struct {
	index    string
	topK     int
	embedder Embedder

	mu      sync.RWMutex
	nextID  int
	entries []entry
}

type entry struct {
	id       int
	content  string
	metadata []map[string]any
	vector   []float32
}

type scoredEntry struct {
	entry entry
	score float64
}

func NewLocalKnowledgeBackend(cfg *Config) (_interface.KnowledgeBackend, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	index := strings.TrimSpace(cfg.Index)
	if index == "" {
		index = DefaultIndex
	}
	topK := cfg.TopK
	if topK <= 0 {
		topK = DefaultTopK
	}
	return &LocalKnowledgeBackend{
		index:    index,
		topK:     topK,
		embedder: cfg.Embedder,
	}, nil
}

func (l *LocalKnowledgeBackend) Index() string {
	return l.index
}

func (l *LocalKnowledgeBackend) AddFromText(text []string, opts ...map[string]any) error {
	contents := make([]string, 0, len(text))
	metadatas := make([][]map[string]any, 0, len(text))
	for _, t := range text {
		if strings.TrimSpace(t) == "" {
			continue
		}
		contents = append(contents, t)
		metadatas = append(metadatas, metadataWithSource("text", "", opts...))
	}
	return l.addEntries(contents, metadatas)
}

func (l *LocalKnowledgeBackend) AddFromFiles(files []string, opts ...map[string]any) error {
	contents := make([]string, 0, len(files))
	metadatas := make([][]map[string]any, 0, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("%w: read file %q: %w", ErrLocalKnowledgeBackend, file, err)
		}
		if strings.TrimSpace(string(data)) == "" {
			continue
		}
		contents = append(contents, string(data))
		metadatas = append(metadatas, metadataWithSource("file", file, opts...))
	}
	return l.addEntries(contents, metadatas)
}

func (l *LocalKnowledgeBackend) AddFromDirectory(directory string, opts ...map[string]any) error {
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("%w: stat directory %q: %w", ErrLocalKnowledgeBackend, directory, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %q is not a directory", ErrLocalKnowledgeBackend, directory)
	}

	files := make([]string, 0)
	err = filepath.WalkDir(directory, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("%w: walk directory %q: %w", ErrLocalKnowledgeBackend, directory, err)
	}
	sort.Strings(files)
	return l.AddFromFiles(files, opts...)
}

func (l *LocalKnowledgeBackend) Search(query string, opts ...map[string]any) ([]ktypes.KnowledgeEntry, error) {
	if strings.TrimSpace(query) == "" {
		return []ktypes.KnowledgeEntry{}, nil
	}

	topK := extractIntOpt("topK", l.topK, opts...)
	topK = extractIntOpt("top_k", topK, opts...)
	if topK <= 0 {
		topK = l.topK
	}

	l.mu.RLock()
	entries := make([]entry, len(l.entries))
	copy(entries, l.entries)
	embedder := l.embedder
	l.mu.RUnlock()

	if len(entries) == 0 {
		return []ktypes.KnowledgeEntry{}, nil
	}

	var scored []scoredEntry
	if embedder != nil && hasVectors(entries) {
		queryVector, err := embedQuery(context.Background(), embedder, query)
		if err != nil {
			return nil, err
		}
		scored = scoreByVector(entries, queryVector)
	} else {
		scored = scoreByText(entries, query)
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].entry.id < scored[j].entry.id
		}
		return scored[i].score > scored[j].score
	})

	if topK > len(scored) {
		topK = len(scored)
	}

	results := make([]ktypes.KnowledgeEntry, 0, topK)
	for _, item := range scored[:topK] {
		results = append(results, ktypes.KnowledgeEntry{
			Content:  item.entry.content,
			Metadata: cloneMetadata(item.entry.metadata),
		})
	}
	return results, nil
}

func (l *LocalKnowledgeBackend) addEntries(contents []string, metadatas [][]map[string]any) error {
	if len(contents) == 0 {
		return nil
	}

	vectors := make([][]float32, len(contents))
	if l.embedder != nil {
		embedded, err := l.embedder.EmbedTexts(context.Background(), contents)
		if err != nil {
			return fmt.Errorf("%w: embed documents: %w", ErrLocalKnowledgeBackend, err)
		}
		if len(embedded) != len(contents) {
			return fmt.Errorf("%w: got %d embeddings for %d documents", ErrInvalidEmbedding, len(embedded), len(contents))
		}
		vectors = embedded
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	for i, content := range contents {
		l.entries = append(l.entries, entry{
			id:       l.nextID,
			content:  content,
			metadata: cloneMetadata(metadatas[i]),
			vector:   append([]float32(nil), vectors[i]...),
		})
		l.nextID++
	}
	return nil
}

func embedQuery(ctx context.Context, embedder Embedder, query string) ([]float32, error) {
	vectors, err := embedder.EmbedTexts(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("%w: embed query: %w", ErrLocalKnowledgeBackend, err)
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("%w: got invalid query embedding response", ErrInvalidEmbedding)
	}
	return vectors[0], nil
}

func hasVectors(entries []entry) bool {
	for _, item := range entries {
		if len(item.vector) == 0 {
			return false
		}
	}
	return true
}

func scoreByVector(entries []entry, queryVector []float32) []scoredEntry {
	scored := make([]scoredEntry, 0, len(entries))
	for _, item := range entries {
		scored = append(scored, scoredEntry{
			entry: item,
			score: cosineSimilarity(queryVector, item.vector),
		})
	}
	return scored
}

func scoreByText(entries []entry, query string) []scoredEntry {
	queryTerms := tokenize(query)
	scored := make([]scoredEntry, 0, len(entries))
	for _, item := range entries {
		score := lexicalScore(query, queryTerms, item.content)
		if score > 0 {
			scored = append(scored, scoredEntry{
				entry: item,
				score: score,
			})
		}
	}
	return scored
}

func lexicalScore(query string, queryTerms []string, content string) float64 {
	lowerContent := strings.ToLower(content)
	score := 0.0
	for _, term := range queryTerms {
		score += float64(strings.Count(lowerContent, term))
	}
	if strings.Contains(lowerContent, strings.ToLower(strings.TrimSpace(query))) {
		score += 0.5
	}
	return score
}

func tokenize(text string) []string {
	seen := make(map[string]struct{})
	terms := make([]string, 0)
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		terms = append(terms, token)
	}
	return terms
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return math.Inf(-1)
	}
	var dot, normA, normB float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	if normA == 0 || normB == 0 {
		return math.Inf(-1)
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func metadataWithSource(source, filePath string, opts ...map[string]any) []map[string]any {
	metadata := extractMetadata(opts...)
	sourceMetadata := map[string]any{"source": source}
	if filePath != "" {
		sourceMetadata["file_path"] = filePath
	}
	return append(metadata, sourceMetadata)
}

func extractMetadata(opts ...map[string]any) []map[string]any {
	for _, opt := range opts {
		val, ok := opt["metadata"]
		if !ok {
			continue
		}
		switch metadata := val.(type) {
		case map[string]any:
			return []map[string]any{cloneMap(metadata)}
		case []map[string]any:
			return cloneMetadata(metadata)
		}
	}
	return nil
}

func extractIntOpt(key string, defaultVal int, opts ...map[string]any) int {
	for _, opt := range opts {
		if val, ok := opt[key]; ok {
			if intVal, ok := val.(int); ok {
				return intVal
			}
		}
	}
	return defaultVal
}

func cloneMetadata(metadata []map[string]any) []map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(metadata))
	for _, item := range metadata {
		out = append(out, cloneMap(item))
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
