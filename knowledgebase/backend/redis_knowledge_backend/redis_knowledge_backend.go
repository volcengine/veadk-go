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
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/volcengine/veadk-go/configs"
	_interface "github.com/volcengine/veadk-go/knowledgebase/interface"
	"github.com/volcengine/veadk-go/knowledgebase/ktypes"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/model"
)

const (
	DefaultRedisIndex = "veadk_knowledge"
	DefaultRedisPort  = 6379
	DefaultTopK       = 5
)

var (
	ErrRedisKnowledgeBackend = errors.New("redis knowledge backend error")
	ErrInvalidEmbedding      = errors.New("invalid embedding response")
	ErrInvalidIndexName      = errors.New("invalid Redis index name")
	indexNameRegexp          = regexp.MustCompile(`^[a-z0-9][a-z0-9_\-.]*$`)
)

type Config struct {
	Host     string
	Username string
	Password string
	Port     int
	DB       int
	Index    string
	TopK     int

	EmbeddingModel   string
	EmbeddingAPIKey  string
	EmbeddingBaseURL string
	EmbeddingDim     int
}

type RedisKnowledgeBackend struct {
	config   *Config
	client   *redis.Client
	embedder model.Embedder
}

func NewRedisKnowledgeBackend(cfg *Config) (_interface.KnowledgeBackend, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	applyRedisDefaults(cfg)
	if cfg.Index == "" {
		cfg.Index = DefaultRedisIndex
	}
	if cfg.TopK <= 0 {
		cfg.TopK = DefaultTopK
	}
	if err := validateIndexName(cfg.Index); err != nil {
		return nil, err
	}
	applyEmbeddingDefaults(cfg)

	embedder, err := model.NewArkEmbeddingModel(context.Background(), cfg.EmbeddingModel, &model.ArkEmbeddingConfig{
		APIKey:     cfg.EmbeddingAPIKey,
		BaseURL:    cfg.EmbeddingBaseURL,
		Dimensions: cfg.EmbeddingDim,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: create embedder: %w", ErrRedisKnowledgeBackend, err)
	}

	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Username: cfg.Username,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	return &RedisKnowledgeBackend{
		config:   cfg,
		client:   client,
		embedder: embedder,
	}, nil
}

func (r *RedisKnowledgeBackend) Index() string {
	return r.config.Index
}

func (r *RedisKnowledgeBackend) AddFromText(text []string, opts ...map[string]any) error {
	_ = opts
	contents := make([]string, 0, len(text))
	for _, t := range text {
		if strings.TrimSpace(t) == "" {
			continue
		}
		contents = append(contents, t)
	}
	return r.addEntries(context.Background(), contents)
}

func (r *RedisKnowledgeBackend) AddFromFiles(files []string, opts ...map[string]any) error {
	contents := make([]string, 0, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("%w: read file %q: %w", ErrRedisKnowledgeBackend, file, err)
		}
		if strings.TrimSpace(string(data)) == "" {
			continue
		}
		contents = append(contents, string(data))
	}
	return r.AddFromText(contents, opts...)
}

func (r *RedisKnowledgeBackend) AddFromDirectory(directory string, opts ...map[string]any) error {
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("%w: stat directory %q: %w", ErrRedisKnowledgeBackend, directory, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %q is not a directory", ErrRedisKnowledgeBackend, directory)
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
		return fmt.Errorf("%w: walk directory %q: %w", ErrRedisKnowledgeBackend, directory, err)
	}
	sort.Strings(files)
	return r.AddFromFiles(files, opts...)
}

func (r *RedisKnowledgeBackend) Search(query string, opts ...map[string]any) ([]ktypes.KnowledgeEntry, error) {
	if strings.TrimSpace(query) == "" {
		return []ktypes.KnowledgeEntry{}, nil
	}

	topK := extractIntOpt("topK", r.config.TopK, opts...)
	topK = extractIntOpt("top_k", topK, opts...)
	if topK <= 0 {
		topK = r.config.TopK
	}

	if err := validateIndexName(r.config.Index); err != nil {
		return nil, err
	}

	ctx := context.Background()
	resp, err := r.embedder.EmbedTexts(ctx, &model.EmbeddingRequest{Texts: []string{query}})
	if err != nil {
		return nil, fmt.Errorf("%w: embed query: %w", ErrRedisKnowledgeBackend, err)
	}
	if len(resp.Embeddings) != 1 {
		return nil, fmt.Errorf("%w: got invalid query embedding response", ErrInvalidEmbedding)
	}
	if err := validateEmbeddingDimension(resp.Embeddings[0], r.config.EmbeddingDim); err != nil {
		return nil, err
	}

	queryVector := float32SliceToBytes(resp.Embeddings[0])
	cmd := r.do(ctx,
		"FT.SEARCH", r.config.Index,
		fmt.Sprintf("*=>[KNN %d @vector $vec AS score]", topK),
		"PARAMS", "2", "vec", queryVector,
		"SORTBY", "score",
		"RETURN", "2", "content", "score",
		"DIALECT", "2",
	)
	if err := cmd.Err(); err != nil {
		if isRedisIndexMissing(err) {
			return []ktypes.KnowledgeEntry{}, nil
		}
		return nil, fmt.Errorf("%w: search redis: %w", ErrRedisKnowledgeBackend, err)
	}

	results, err := cmd.Slice()
	if err != nil {
		return nil, fmt.Errorf("%w: parse redis search results: %w", ErrRedisKnowledgeBackend, err)
	}

	return parseRedisSearchResults(results), nil
}

func (r *RedisKnowledgeBackend) addEntries(ctx context.Context, contents []string) error {
	if len(contents) == 0 {
		return nil
	}
	if err := validateIndexName(r.config.Index); err != nil {
		return err
	}
	if err := r.ensureIndex(ctx); err != nil {
		return err
	}

	resp, err := r.embedder.EmbedTexts(ctx, &model.EmbeddingRequest{Texts: contents})
	if err != nil {
		return fmt.Errorf("%w: embed documents: %w", ErrRedisKnowledgeBackend, err)
	}
	if len(resp.Embeddings) != len(contents) {
		return fmt.Errorf("%w: got %d embeddings for %d documents", ErrInvalidEmbedding, len(resp.Embeddings), len(contents))
	}

	for i, content := range contents {
		if err := validateEmbeddingDimension(resp.Embeddings[i], r.config.EmbeddingDim); err != nil {
			return err
		}
		key := fmt.Sprintf("%s:%s", r.config.Index, uuid.NewString())
		vector := float32SliceToBytes(resp.Embeddings[i])
		if err := r.do(ctx, "HSET", key, "content", content, "vector", vector).Err(); err != nil {
			return fmt.Errorf("%w: write redis hash %q: %w", ErrRedisKnowledgeBackend, key, err)
		}
	}
	return nil
}

func (r *RedisKnowledgeBackend) ensureIndex(ctx context.Context) error {
	cmd := r.do(ctx,
		"FT.CREATE", r.config.Index,
		"ON", "HASH",
		"PREFIX", "1", r.config.Index+":",
		"SCHEMA",
		"content", "TEXT",
		"vector", "VECTOR", "HNSW", "6",
		"TYPE", "FLOAT32",
		"DIM", r.config.EmbeddingDim,
		"DISTANCE_METRIC", "COSINE",
	)
	if err := cmd.Err(); err != nil {
		if isRedisIndexExists(err) {
			return nil
		}
		return fmt.Errorf("%w: create index %q: %w", ErrRedisKnowledgeBackend, r.config.Index, err)
	}
	log.Infof("Created Redis knowledge index %q with dimension %d", r.config.Index, r.config.EmbeddingDim)
	return nil
}

func parseRedisSearchResults(results []interface{}) []ktypes.KnowledgeEntry {
	if len(results) < 3 {
		return []ktypes.KnowledgeEntry{}
	}

	entries := make([]ktypes.KnowledgeEntry, 0, (len(results)-1)/2)
	for i := 2; i < len(results); i += 2 {
		fields, ok := toInterfaceSlice(results[i])
		if !ok {
			continue
		}

		var content string
		for j := 0; j+1 < len(fields); j += 2 {
			fieldName := redisValueToString(fields[j])
			if fieldName == "content" {
				content = redisValueToString(fields[j+1])
				break
			}
		}
		if content == "" {
			continue
		}
		entries = append(entries, ktypes.KnowledgeEntry{Content: content})
	}
	return entries
}

func validateIndexName(name string) error {
	if !indexNameRegexp.MatchString(name) {
		return fmt.Errorf("%w: %q (must be lowercase, start with alphanumeric, contain only [a-z0-9_\\-.])", ErrInvalidIndexName, name)
	}
	return nil
}

func validateEmbeddingDimension(vec []float32, dim int) error {
	if len(vec) != dim {
		return fmt.Errorf("%w: got dimension %d, want %d", ErrInvalidEmbedding, len(vec), dim)
	}
	return nil
}

func float32SliceToBytes(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

//go:noinline
func (r *RedisKnowledgeBackend) do(ctx context.Context, args ...any) *redis.Cmd {
	return r.client.Do(ctx, args...)
}

func applyRedisDefaults(cfg *Config) {
	global := configs.GetGlobalConfig()
	if global == nil || global.Database == nil || global.Database.Redis == nil {
		if cfg.Port == 0 {
			cfg.Port = DefaultRedisPort
		}
		return
	}

	redisCfg := global.Database.Redis
	if cfg.Host == "" {
		cfg.Host = redisCfg.Host
	}
	if cfg.Port == 0 {
		cfg.Port = redisCfg.Port
	}
	if cfg.Username == "" {
		cfg.Username = redisCfg.Username
	}
	if cfg.Password == "" {
		cfg.Password = redisCfg.Password
	}
	if cfg.DB == 0 {
		cfg.DB = redisCfg.DB
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultRedisPort
	}
}

func applyEmbeddingDefaults(cfg *Config) {
	global := configs.GetGlobalConfig()
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = global.Model.Embedding.Name
	}
	if cfg.EmbeddingAPIKey == "" {
		cfg.EmbeddingAPIKey = global.Model.Embedding.ApiKey
	}
	if cfg.EmbeddingBaseURL == "" {
		cfg.EmbeddingBaseURL = global.Model.Embedding.ApiBase
	}
	if cfg.EmbeddingDim == 0 {
		cfg.EmbeddingDim = global.Model.Embedding.Dim
	}
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

func isRedisIndexExists(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "index already exists")
}

func isRedisIndexMissing(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown index name") || strings.Contains(msg, "no such index")
}

func toInterfaceSlice(val any) ([]interface{}, bool) {
	switch v := val.(type) {
	case []interface{}:
		return v, true
	default:
		return nil, false
	}
}

func redisValueToString(val any) string {
	switch v := val.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}
