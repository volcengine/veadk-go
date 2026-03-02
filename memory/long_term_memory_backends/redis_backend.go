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
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/model"
)

const (
	DefaultRedisIndex = "veadk-ltm"
	DefaultRedisPort  = 6379
)

// RedisMemoryConfig holds configuration for the Redis long-term memory backend.
type RedisMemoryConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	DB       int
	Index    string

	// EmbeddingConfig configures the embedding model. If nil, uses global config defaults.
	EmbeddingConfig *EmbeddingConfig
}

// RedisMemoryBackend implements LongTermMemoryBackend using Redis with vector search.
type RedisMemoryBackend struct {
	config   *RedisMemoryConfig
	client   *redis.Client
	embedder model.Embedder
}

// NewRedisMemoryBackend creates a new Redis-backed long-term memory backend.
func NewRedisMemoryBackend(config *RedisMemoryConfig) (LongTermMemoryBackend, error) {
	if config.Port == 0 {
		config.Port = DefaultRedisPort
	}
	if config.Index == "" {
		config.Index = DefaultRedisIndex
	}
	if config.EmbeddingConfig == nil {
		config.EmbeddingConfig = NewDefaultEmbeddingConfig()
	}

	embedder, err := config.EmbeddingConfig.CreateEmbedder(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder for redis backend: %w", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		Username: config.Username,
		Password: config.Password,
		DB:       config.DB,
	})

	backend := &RedisMemoryBackend{
		config:   config,
		client:   client,
		embedder: embedder,
	}

	if err := backend.ensureIndex(context.Background()); err != nil {
		return nil, err
	}

	return backend, nil
}

// ensureIndex creates the RediSearch vector index if it does not already exist.
func (r *RedisMemoryBackend) ensureIndex(ctx context.Context) error {
	indexName := r.config.Index
	dim := r.config.EmbeddingConfig.Dimensions

	// Check if the index already exists
	_, err := r.client.Do(ctx, "FT.INFO", indexName).Result()
	if err == nil {
		return nil
	}

	// Create the vector index
	err = r.client.Do(ctx, "FT.CREATE", indexName,
		"ON", "HASH",
		"PREFIX", "1", indexName+":",
		"SCHEMA",
		"text", "TEXT", "WEIGHT", "1.0",
		"timestamp", "NUMERIC", "SORTABLE",
		"vector", "VECTOR", "FLAT", "6",
		"TYPE", "FLOAT32",
		"DIM", dim,
		"DISTANCE_METRIC", "COSINE",
	).Err()
	if err != nil {
		return fmt.Errorf("failed to create redis vector index %q: %w", indexName, err)
	}

	log.Infof("Created Redis vector index %q with dimension %d", indexName, dim)
	return nil
}

func (r *RedisMemoryBackend) SaveMemory(ctx context.Context, userId string, eventList []string) error {
	if len(eventList) == 0 {
		return nil
	}

	resp, err := r.embedder.EmbedTexts(ctx, &model.EmbeddingRequest{Texts: eventList})
	if err != nil {
		return fmt.Errorf("failed to embed texts for redis: %w", err)
	}

	pipe := r.client.Pipeline()
	for i, event := range eventList {
		id, err := uuid.NewUUID()
		if err != nil {
			return fmt.Errorf("generate uuid failed: %w", err)
		}
		key := fmt.Sprintf("%s:%s:%s", r.config.Index, userId, id.String())
		vectorBytes := float32SliceToBytes(resp.Embeddings[i])

		pipe.HSet(ctx, key, map[string]interface{}{
			"text":      event,
			"timestamp": time.Now().UnixMilli(),
			"vector":    vectorBytes,
		})
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to save memories to redis: %w", err)
	}

	log.Infof("Successfully saved user %s %d events to Redis", userId, len(eventList))
	return nil
}

func (r *RedisMemoryBackend) SearchMemory(ctx context.Context, userId, query string, topK int) ([]*MemItem, error) {
	log.Infof("Searching Redis for query: %s, user: %s, top_k: %d", query, userId, topK)

	resp, err := r.embedder.EmbedTexts(ctx, &model.EmbeddingRequest{Texts: []string{query}})
	if err != nil {
		return nil, fmt.Errorf("failed to embed query for redis: %w", err)
	}

	queryVector := float32SliceToBytes(resp.Embeddings[0])

	// FT.SEARCH with KNN and prefix filter for user isolation
	searchQuery := fmt.Sprintf("@__key:{%s\\:%s\\:*}=>[KNN %d @vector $BLOB AS score]",
		escapeRedisTag(r.config.Index), escapeRedisTag(userId), topK)

	cmd := r.client.Do(ctx, "FT.SEARCH", r.config.Index,
		searchQuery,
		"PARAMS", "2", "BLOB", queryVector,
		"SORTBY", "score",
		"RETURN", "2", "text", "timestamp",
		"LIMIT", "0", strconv.Itoa(topK),
		"DIALECT", "2",
	)
	if err := cmd.Err(); err != nil {
		return nil, fmt.Errorf("failed to search redis: %w", err)
	}

	results, err := cmd.Slice()
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis search results: %w", err)
	}

	return parseRedisSearchResults(results), nil
}

// parseRedisSearchResults parses FT.SEARCH results into MemItem slice.
// FT.SEARCH returns: [total_count, key1, [field1, val1, field2, val2, ...], key2, [...], ...]
func parseRedisSearchResults(results []interface{}) []*MemItem {
	if len(results) < 1 {
		return nil
	}

	var items []*MemItem
	// results[0] is the total count, then pairs of (key, fields)
	for i := 2; i < len(results); i += 2 {
		fields, ok := results[i].([]interface{})
		if !ok {
			continue
		}

		item := &MemItem{}
		for j := 0; j+1 < len(fields); j += 2 {
			fieldName, _ := fields[j].(string)
			fieldVal, _ := fields[j+1].(string)
			switch fieldName {
			case "text":
				item.Content = fieldVal
			case "timestamp":
				if ts, err := strconv.ParseInt(fieldVal, 10, 64); err == nil {
					item.Timestamp = time.UnixMilli(ts)
				}
			}
		}
		if item.Content != "" {
			items = append(items, item)
		}
	}
	return items
}

// float32SliceToBytes converts a []float32 to a little-endian byte slice for Redis vector storage.
func float32SliceToBytes(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// escapeRedisTag escapes special characters in Redis tag query values.
func escapeRedisTag(s string) string {
	var result []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '-', '.', ':', '/':
			result = append(result, '\\', s[i])
		default:
			result = append(result, s[i])
		}
	}
	return string(result)
}
