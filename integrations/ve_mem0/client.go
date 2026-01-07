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

package ve_mem0

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Mem0Client is the client for Mem0 API
type Mem0Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// ClientOption is a function to configure the client
type ClientOption func(*Mem0Client)

// NewMem0Client creates a new Mem0 client
func NewMem0Client(server string, apiKey string, opts ...ClientOption) *Mem0Client {
	c := &Mem0Client{
		baseURL:    server,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// AddMemoriesRequest represents the request body for adding memories
type AddMemoriesRequest struct {
	Messages  []Message `json:"messages"`
	UserId    *string   `json:"user_id,omitempty"`
	AsyncMode *bool     `json:"async_mode,omitempty"`
}

// Message represents a message in the memory
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AddMemoriesResponse represents the response for adding memories
type AddMemoriesResponse struct {
	Results []AddMemoryResult `json:"results"`
}

type AddMemoryResult struct {
	Message string `json:"message"`
	Status  string `json:"status"`
	EventId string `json:"event_id"`
	// For compatibility if needed
	Id string `json:"id,omitempty"`
}

// MemoryItem represents a memory item (used in Search)
type MemoryItem struct {
	Id        string                 `json:"id"`
	Memory    string                 `json:"memory"`
	Hash      string                 `json:"hash,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	UserId    *string                `json:"user_id,omitempty"`
	AgentId   *string                `json:"agent_id,omitempty"`
	RunId     *string                `json:"run_id,omitempty"`
	Score     float32                `json:"score,omitempty"`
	CreatedAt time.Time              `json:"created_at,omitempty"`
	UpdatedAt *time.Time             `json:"updated_at,omitempty"`
}

// SearchMemoriesRequest represents the request body for searching memories
type SearchMemoriesRequest struct {
	Query       string                 `json:"query"`
	UserId      *string                `json:"user_id,omitempty"`
	RunId       *string                `json:"run_id,omitempty"`
	AgentId     *string                `json:"agent_id,omitempty"`
	TopK        *int                   `json:"top_k,omitempty"`
	EnableGraph *bool                  `json:"enable_graph,omitempty"`
	Filters     map[string]interface{} `json:"filters,omitempty"`
	ProjectId   *string                `json:"project_id,omitempty"`
}

// SearchMemoriesResponse represents the response for searching memories
type SearchMemoriesResponse struct {
	Results []MemoryItem `json:"results"`
}

// Add adds memories
func (c *Mem0Client) Add(ctx context.Context, req AddMemoriesRequest) (AddMemoriesResponse, error) {
	var response AddMemoriesResponse
	url := c.baseURL + "/v1/memories"

	body, err := c.doRequest(ctx, http.MethodPost, url, req)
	if err != nil {
		return response, fmt.Errorf("mem0 add memories error: %w", err)
	}

	err = json.Unmarshal(body, &response)
	if err != nil {
		return response, fmt.Errorf("mem0 add memories unmarshale body rror: %w", err)
	}
	return response, nil
}

// Search searches memories
func (c *Mem0Client) Search(ctx context.Context, req SearchMemoriesRequest) (SearchMemoriesResponse, error) {
	var response SearchMemoriesResponse
	url := c.baseURL + "/v1/search"
	body, err := c.doRequest(ctx, http.MethodPost, url, req)
	if err != nil {
		return response, fmt.Errorf("mem0 add memories error: %w", err)
	}

	err = json.Unmarshal(body, &response)
	if err != nil {
		return response, fmt.Errorf("mem0 add memories unmarshale body rror: %w", err)
	}
	return response, nil
}

func (c *Mem0Client) doRequest(ctx context.Context, method, url string, body interface{}) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("build request error: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("do request error status=%d body=%s", resp.StatusCode, string(respBody))
	}

	return io.ReadAll(resp.Body)
}

func (c *Mem0Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", c.apiKey)
	}
}
