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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/model"
)

const (
	DefaultOpenSearchIndex = "veadk_ltm"
	DefaultOpenSearchPort  = 9200
)

var (
	ErrInvalidIndexName = errors.New("invalid OpenSearch index name")
	indexNameRegexp      = regexp.MustCompile(`^[a-z0-9][a-z0-9_\-.]*$`)
)

// OpenSearchMemoryConfig holds configuration for the OpenSearch long-term memory backend.
type OpenSearchMemoryConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	UseSSL   bool
	CertPath string
	Index    string

	// EmbeddingConfig configures the embedding model. If nil, uses global config defaults.
	EmbeddingConfig *EmbeddingConfig
}

// OpenSearchMemoryBackend implements LongTermMemoryBackend using OpenSearch with vector search.
type OpenSearchMemoryBackend struct {
	config     *OpenSearchMemoryConfig
	httpClient *http.Client
	baseURL    string
	embedder   model.Embedder
}

// NewOpenSearchMemoryBackend creates a new OpenSearch-backed long-term memory backend.
func NewOpenSearchMemoryBackend(config *OpenSearchMemoryConfig) (LongTermMemoryBackend, error) {
	if config.Port == 0 {
		config.Port = DefaultOpenSearchPort
	}
	if config.Index == "" {
		config.Index = DefaultOpenSearchIndex
	}
	if config.EmbeddingConfig == nil {
		config.EmbeddingConfig = NewDefaultEmbeddingConfig()
	}

	embedder, err := config.EmbeddingConfig.CreateEmbedder(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder for opensearch backend: %w", err)
	}

	httpClient, err := buildHTTPClient(config)
	if err != nil {
		return nil, err
	}

	scheme := "http"
	if config.UseSSL {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", scheme, config.Host, config.Port)

	backend := &OpenSearchMemoryBackend{
		config:     config,
		httpClient: httpClient,
		baseURL:    baseURL,
		embedder:   embedder,
	}

	return backend, nil
}

func buildHTTPClient(config *OpenSearchMemoryConfig) (*http.Client, error) {
	transport := &http.Transport{}

	if config.UseSSL {
		tlsCfg := &tls.Config{} //nolint:gosec // user can configure cert verification
		if config.CertPath != "" {
			caCert, err := os.ReadFile(config.CertPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read OpenSearch cert file %q: %w", config.CertPath, err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to parse OpenSearch cert file %q", config.CertPath)
			}
			tlsCfg.RootCAs = pool
		} else {
			log.Warn("OpenSearch cert_path is not set, which may lead to security risks")
			tlsCfg.InsecureSkipVerify = true //nolint:gosec // user chose not to provide cert
		}
		transport.TLSClientConfig = tlsCfg
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}

// validateIndexName checks that the index name conforms to OpenSearch naming rules.
func validateIndexName(name string) error {
	if !indexNameRegexp.MatchString(name) {
		return fmt.Errorf("%w: %q (must be lowercase, start with alphanumeric, contain only [a-z0-9_\\-.])", ErrInvalidIndexName, name)
	}
	return nil
}

// ensureIndex creates an OpenSearch index with the appropriate mapping if it doesn't exist.
func (o *OpenSearchMemoryBackend) ensureIndex(ctx context.Context, indexName string) error {
	dim := o.config.EmbeddingConfig.Dimensions

	mapping := map[string]interface{}{
		"settings": map[string]interface{}{
			"index": map[string]interface{}{
				"knn": true,
			},
		},
		"mappings": map[string]interface{}{
			"properties": map[string]interface{}{
				"text": map[string]interface{}{
					"type": "text",
				},
				"timestamp": map[string]interface{}{
					"type": "long",
				},
				"vector": map[string]interface{}{
					"type":      "knn_vector",
					"dimension": dim,
					"method": map[string]interface{}{
						"name":       "hnsw",
						"space_type": "cosinesimil",
						"engine":     "nmslib",
					},
				},
			},
		},
	}

	body, _ := json.Marshal(mapping)
	resp, err := o.doRequest(ctx, http.MethodPut, "/"+indexName, body)
	if err != nil {
		return fmt.Errorf("failed to create opensearch index %q: %w", indexName, err)
	}
	defer resp.Body.Close()

	// 200 = created, 400 with "resource_already_exists_exception" = already exists (both OK)
	if resp.StatusCode == http.StatusOK {
		log.Infof("Created OpenSearch index %q with dimension %d", indexName, dim)
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(respBody), "resource_already_exists_exception") {
		return nil
	}

	return fmt.Errorf("failed to create opensearch index %q: status=%d, body=%s", indexName, resp.StatusCode, string(respBody))
}

func (o *OpenSearchMemoryBackend) SaveMemory(ctx context.Context, userId string, eventList []string) error {
	if len(eventList) == 0 {
		return nil
	}

	indexName := fmt.Sprintf("%s_%s", o.config.Index, userId)
	if err := validateIndexName(indexName); err != nil {
		return err
	}

	if err := o.ensureIndex(ctx, indexName); err != nil {
		return err
	}

	resp, err := o.embedder.EmbedTexts(ctx, &model.EmbeddingRequest{Texts: eventList})
	if err != nil {
		return fmt.Errorf("failed to embed texts for opensearch: %w", err)
	}

	// Build bulk request body
	var buf bytes.Buffer
	for i, event := range eventList {
		id, err := uuid.NewUUID()
		if err != nil {
			return fmt.Errorf("generate uuid failed: %w", err)
		}

		action := map[string]interface{}{
			"index": map[string]interface{}{
				"_index": indexName,
				"_id":    id.String(),
			},
		}
		doc := map[string]interface{}{
			"text":      event,
			"timestamp": time.Now().UnixMilli(),
			"vector":    resp.Embeddings[i],
		}

		actionLine, _ := json.Marshal(action)
		docLine, _ := json.Marshal(doc)
		buf.Write(actionLine)
		buf.WriteByte('\n')
		buf.Write(docLine)
		buf.WriteByte('\n')
	}

	bulkResp, err := o.doRequest(ctx, http.MethodPost, "/_bulk", buf.Bytes())
	if err != nil {
		return fmt.Errorf("failed to bulk index to opensearch: %w", err)
	}
	defer bulkResp.Body.Close()

	if bulkResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(bulkResp.Body)
		return fmt.Errorf("opensearch bulk index failed: status=%d, body=%s", bulkResp.StatusCode, string(respBody))
	}

	log.Infof("Successfully saved user %s %d events to OpenSearch", userId, len(eventList))
	return nil
}

func (o *OpenSearchMemoryBackend) SearchMemory(ctx context.Context, userId, query string, topK int) ([]*MemItem, error) {
	log.Infof("Searching OpenSearch for query: %s, user: %s, top_k: %d", query, userId, topK)

	indexName := fmt.Sprintf("%s_%s", o.config.Index, userId)
	if err := validateIndexName(indexName); err != nil {
		return nil, err
	}

	resp, err := o.embedder.EmbedTexts(ctx, &model.EmbeddingRequest{Texts: []string{query}})
	if err != nil {
		return nil, fmt.Errorf("failed to embed query for opensearch: %w", err)
	}

	searchBody := map[string]interface{}{
		"size": topK,
		"query": map[string]interface{}{
			"knn": map[string]interface{}{
				"vector": map[string]interface{}{
					"vector": resp.Embeddings[0],
					"k":      topK,
				},
			},
		},
		"_source": []string{"text", "timestamp"},
	}

	body, _ := json.Marshal(searchBody)
	searchResp, err := o.doRequest(ctx, http.MethodPost, "/"+indexName+"/_search", body)
	if err != nil {
		return nil, fmt.Errorf("failed to search opensearch: %w", err)
	}
	defer searchResp.Body.Close()

	respBody, err := io.ReadAll(searchResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read opensearch response: %w", err)
	}

	if searchResp.StatusCode != http.StatusOK {
		// Index not found means no memories yet
		if strings.Contains(string(respBody), "index_not_found_exception") {
			return nil, nil
		}
		return nil, fmt.Errorf("opensearch search failed: status=%d, body=%s", searchResp.StatusCode, string(respBody))
	}

	return parseOpenSearchResults(respBody)
}

func parseOpenSearchResults(respBody []byte) ([]*MemItem, error) {
	var result struct {
		Hits struct {
			Hits []struct {
				Source struct {
					Text      string `json:"text"`
					Timestamp int64  `json:"timestamp"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse opensearch response: %w", err)
	}

	var items []*MemItem
	for _, hit := range result.Hits.Hits {
		if hit.Source.Text != "" {
			items = append(items, &MemItem{
				Content:   hit.Source.Text,
				Timestamp: time.UnixMilli(hit.Source.Timestamp),
			})
		}
	}
	return items, nil
}

func (o *OpenSearchMemoryBackend) doRequest(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	url := o.baseURL + path
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if o.config.Username != "" || o.config.Password != "" {
		req.SetBasicAuth(o.config.Username, o.config.Password)
	}

	return o.httpClient.Do(req)
}
