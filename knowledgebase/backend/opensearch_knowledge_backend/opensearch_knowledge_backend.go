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
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/volcengine/veadk-go/configs"
	_interface "github.com/volcengine/veadk-go/knowledgebase/interface"
	"github.com/volcengine/veadk-go/knowledgebase/ktypes"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/model"
)

const (
	DefaultOpenSearchIndex = "veadk_knowledge"
	DefaultOpenSearchPort  = 9200
	DefaultTopK            = 5
)

var (
	ErrOpenSearchKnowledgeBackend = errors.New("opensearch knowledge backend error")
	ErrInvalidEmbedding           = errors.New("invalid embedding response")
	ErrInvalidIndexName           = errors.New("invalid OpenSearch index name")
	indexNameRegexp               = regexp.MustCompile(`^[a-z0-9][a-z0-9_\-.]*$`)
)

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	UseSSL   bool
	CertPath string
	Index    string
	TopK     int

	EmbeddingModel   string
	EmbeddingAPIKey  string
	EmbeddingBaseURL string
	EmbeddingDim     int
}

type OpenSearchKnowledgeBackend struct {
	config     *Config
	httpClient *http.Client
	baseURL    string
	embedder   model.Embedder
}

func NewOpenSearchKnowledgeBackend(cfg *Config) (_interface.KnowledgeBackend, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultOpenSearchPort
	}
	if cfg.Index == "" {
		cfg.Index = DefaultOpenSearchIndex
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
		return nil, fmt.Errorf("%w: create embedder: %w", ErrOpenSearchKnowledgeBackend, err)
	}

	httpClient, err := buildHTTPClient(cfg)
	if err != nil {
		return nil, err
	}

	scheme := "http"
	if cfg.UseSSL {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, cfg.Port)

	return &OpenSearchKnowledgeBackend{
		config:     cfg,
		httpClient: httpClient,
		baseURL:    baseURL,
		embedder:   embedder,
	}, nil
}

func (o *OpenSearchKnowledgeBackend) Index() string {
	return o.config.Index
}

func (o *OpenSearchKnowledgeBackend) AddFromText(text []string, opts ...map[string]any) error {
	contents := make([]string, 0, len(text))
	metadatas := make([][]map[string]any, 0, len(text))
	for _, t := range text {
		if strings.TrimSpace(t) == "" {
			continue
		}
		contents = append(contents, t)
		metadatas = append(metadatas, metadataWithSource("text", "", opts...))
	}
	return o.addEntries(context.Background(), contents, metadatas)
}

func (o *OpenSearchKnowledgeBackend) AddFromFiles(files []string, opts ...map[string]any) error {
	contents := make([]string, 0, len(files))
	metadatas := make([][]map[string]any, 0, len(files))
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("%w: read file %q: %w", ErrOpenSearchKnowledgeBackend, file, err)
		}
		if strings.TrimSpace(string(data)) == "" {
			continue
		}
		contents = append(contents, string(data))
		metadatas = append(metadatas, metadataWithSource("file", file, opts...))
	}
	return o.addEntries(context.Background(), contents, metadatas)
}

func (o *OpenSearchKnowledgeBackend) AddFromDirectory(directory string, opts ...map[string]any) error {
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("%w: stat directory %q: %w", ErrOpenSearchKnowledgeBackend, directory, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %q is not a directory", ErrOpenSearchKnowledgeBackend, directory)
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
		return fmt.Errorf("%w: walk directory %q: %w", ErrOpenSearchKnowledgeBackend, directory, err)
	}
	sort.Strings(files)
	return o.AddFromFiles(files, opts...)
}

func (o *OpenSearchKnowledgeBackend) Search(query string, opts ...map[string]any) ([]ktypes.KnowledgeEntry, error) {
	if strings.TrimSpace(query) == "" {
		return []ktypes.KnowledgeEntry{}, nil
	}

	topK := extractIntOpt("topK", o.config.TopK, opts...)
	topK = extractIntOpt("top_k", topK, opts...)
	if topK <= 0 {
		topK = o.config.TopK
	}

	if err := validateIndexName(o.config.Index); err != nil {
		return nil, err
	}

	ctx := context.Background()
	resp, err := o.embedder.EmbedTexts(ctx, &model.EmbeddingRequest{Texts: []string{query}})
	if err != nil {
		return nil, fmt.Errorf("%w: embed query: %w", ErrOpenSearchKnowledgeBackend, err)
	}
	if len(resp.Embeddings) != 1 {
		return nil, fmt.Errorf("%w: got invalid query embedding response", ErrInvalidEmbedding)
	}

	searchBody := map[string]any{
		"size": topK,
		"query": map[string]any{
			"knn": map[string]any{
				"vector": map[string]any{
					"vector": resp.Embeddings[0],
					"k":      topK,
				},
			},
		},
		"_source": []string{"text", "metadata"},
	}

	body, _ := json.Marshal(searchBody)
	searchResp, err := o.doRequest(ctx, http.MethodPost, "/"+o.config.Index+"/_search", body)
	if err != nil {
		return nil, fmt.Errorf("%w: search opensearch: %w", ErrOpenSearchKnowledgeBackend, err)
	}
	defer searchResp.Body.Close()

	respBody, err := io.ReadAll(searchResp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read opensearch response: %w", ErrOpenSearchKnowledgeBackend, err)
	}
	if searchResp.StatusCode != http.StatusOK {
		if strings.Contains(string(respBody), "index_not_found_exception") {
			return []ktypes.KnowledgeEntry{}, nil
		}
		return nil, fmt.Errorf("%w: search failed: status=%d, body=%s", ErrOpenSearchKnowledgeBackend, searchResp.StatusCode, string(respBody))
	}

	return parseOpenSearchResults(respBody)
}

func (o *OpenSearchKnowledgeBackend) addEntries(ctx context.Context, contents []string, metadatas [][]map[string]any) error {
	if len(contents) == 0 {
		return nil
	}
	if err := validateIndexName(o.config.Index); err != nil {
		return err
	}
	if err := o.ensureIndex(ctx); err != nil {
		return err
	}

	resp, err := o.embedder.EmbedTexts(ctx, &model.EmbeddingRequest{Texts: contents})
	if err != nil {
		return fmt.Errorf("%w: embed documents: %w", ErrOpenSearchKnowledgeBackend, err)
	}
	if len(resp.Embeddings) != len(contents) {
		return fmt.Errorf("%w: got %d embeddings for %d documents", ErrInvalidEmbedding, len(resp.Embeddings), len(contents))
	}

	var buf bytes.Buffer
	for i, content := range contents {
		action := map[string]any{
			"index": map[string]any{
				"_index": o.config.Index,
			},
		}
		doc := map[string]any{
			"text":     content,
			"metadata": cloneMetadata(metadatas[i]),
			"vector":   resp.Embeddings[i],
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
		return fmt.Errorf("%w: bulk index to opensearch: %w", ErrOpenSearchKnowledgeBackend, err)
	}
	defer bulkResp.Body.Close()

	if bulkResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(bulkResp.Body)
		return fmt.Errorf("%w: bulk index failed: status=%d, body=%s", ErrOpenSearchKnowledgeBackend, bulkResp.StatusCode, string(respBody))
	}
	return nil
}

func (o *OpenSearchKnowledgeBackend) ensureIndex(ctx context.Context) error {
	mapping := map[string]any{
		"settings": map[string]any{
			"index": map[string]any{
				"knn": true,
			},
		},
		"mappings": map[string]any{
			"properties": map[string]any{
				"text": map[string]any{
					"type": "text",
				},
				"metadata": map[string]any{
					"type": "object",
				},
				"vector": map[string]any{
					"type":      "knn_vector",
					"dimension": o.config.EmbeddingDim,
					"method": map[string]any{
						"name":       "hnsw",
						"space_type": "cosinesimil",
						"engine":     "nmslib",
					},
				},
			},
		},
	}

	body, _ := json.Marshal(mapping)
	resp, err := o.doRequest(ctx, http.MethodPut, "/"+o.config.Index, body)
	if err != nil {
		return fmt.Errorf("%w: create index %q: %w", ErrOpenSearchKnowledgeBackend, o.config.Index, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		log.Infof("Created OpenSearch knowledge index %q with dimension %d", o.config.Index, o.config.EmbeddingDim)
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(respBody), "resource_already_exists_exception") {
		return nil
	}

	return fmt.Errorf("%w: create index %q failed: status=%d, body=%s", ErrOpenSearchKnowledgeBackend, o.config.Index, resp.StatusCode, string(respBody))
}

func parseOpenSearchResults(respBody []byte) ([]ktypes.KnowledgeEntry, error) {
	var result struct {
		Hits struct {
			Hits []struct {
				Source struct {
					Text     string           `json:"text"`
					Metadata []map[string]any `json:"metadata"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("%w: parse opensearch response: %w", ErrOpenSearchKnowledgeBackend, err)
	}

	entries := make([]ktypes.KnowledgeEntry, 0, len(result.Hits.Hits))
	for _, hit := range result.Hits.Hits {
		if hit.Source.Text == "" {
			continue
		}
		entries = append(entries, ktypes.KnowledgeEntry{
			Content:  hit.Source.Text,
			Metadata: cloneMetadata(hit.Source.Metadata),
		})
	}
	return entries, nil
}

func buildHTTPClient(config *Config) (*http.Client, error) {
	transport := &http.Transport{}

	if config.UseSSL {
		tlsCfg := &tls.Config{} //nolint:gosec // user can configure cert verification
		if config.CertPath != "" {
			caCert, err := os.ReadFile(config.CertPath)
			if err != nil {
				return nil, fmt.Errorf("%w: read cert file %q: %w", ErrOpenSearchKnowledgeBackend, config.CertPath, err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("%w: parse cert file %q", ErrOpenSearchKnowledgeBackend, config.CertPath)
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

func validateIndexName(name string) error {
	if !indexNameRegexp.MatchString(name) {
		return fmt.Errorf("%w: %q (must be lowercase, start with alphanumeric, contain only [a-z0-9_\\-.])", ErrInvalidIndexName, name)
	}
	return nil
}

func (o *OpenSearchKnowledgeBackend) doRequest(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
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
