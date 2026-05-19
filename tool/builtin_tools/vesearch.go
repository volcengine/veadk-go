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

package builtin_tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const (
	defaultVeSearchTimeout = 60 * time.Second
	veSearchCompletionURL  = "https://open.feedcoopapi.com/agent_api/agent/chat/completion"
)

var ErrVeSearchConfig = errors.New("vesearch config error")

//go:noinline
func doVeSearchRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	return client.Do(req)
}

var veSearchToolDescription = `
	Search information from the Internet, social media, news sites, and other sources with Volcano Engine VeSearch.

	Args:
		query (str): The query string to search.

	Returns:
		Summarized search results returned by VeSearch.
`

type VeSearchConfig struct {
	APIKey     string
	Endpoint   string
	Region     string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type VeSearchArgs struct {
	Query string `json:"query" jsonschema:"The query string to search with VeSearch"`
}

type VeSearchResult struct {
	Result string `json:"result,omitempty"`
}

type veSearchChatCompletionRequest struct {
	BotID    string            `json:"bot_id"`
	Messages []veSearchMessage `json:"messages"`
}

type veSearchMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type veSearchChatCompletionResponse struct {
	Choices []struct {
		Message veSearchMessage `json:"message"`
	} `json:"choices"`
}

func NewVeSearchTool(cfg *VeSearchConfig) (tool.Tool, error) {
	if cfg == nil {
		cfg = &VeSearchConfig{}
	}
	if err := cfg.applyDefaults(); err != nil {
		return nil, err
	}
	return functiontool.New(
		functiontool.Config{
			Name:        "vesearch",
			Description: veSearchToolDescription,
		},
		cfg.veSearchHandler)
}

func (c *VeSearchConfig) veSearchHandler(ctx tool.Context, args VeSearchArgs) (VeSearchResult, error) {
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return VeSearchResult{}, fmt.Errorf("vesearch query is empty")
	}

	executeCtx := context.Background()
	if ctx != nil {
		executeCtx = ctx
	}
	result, err := c.execute(executeCtx, query)
	if err != nil {
		return VeSearchResult{}, err
	}
	return VeSearchResult{Result: result}, nil
}

func (c *VeSearchConfig) applyDefaults() error {
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	if c.Endpoint == "" {
		c.Endpoint = strings.TrimSpace(utils.GetEnvWithDefault(common.TOOL_VESEARCH_ENDPOINT))
	}
	if c.Endpoint == "" {
		return fmt.Errorf("%w: %s is required", ErrVeSearchConfig, common.TOOL_VESEARCH_ENDPOINT)
	}

	c.APIKey = strings.TrimSpace(c.APIKey)
	if c.APIKey == "" {
		apiKey, err := resolveVeSearchAPIKey(c.Region)
		if err != nil {
			return err
		}
		c.APIKey = strings.TrimSpace(apiKey)
	}
	if c.APIKey == "" {
		return fmt.Errorf("%w: %s is required or Volcano Engine credentials must be available", ErrVeSearchConfig, common.TOOL_VESEARCH_API_KEY)
	}

	if c.Timeout <= 0 {
		c.Timeout = defaultVeSearchTimeout
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: c.Timeout}
	}
	return nil
}

func resolveVeSearchAPIKey(region string) (string, error) {
	if key := strings.TrimSpace(utils.GetEnvWithDefault(common.TOOL_VESEARCH_API_KEY)); key != "" {
		return key, nil
	}
	key, err := veauth.GetVeSearchToken(region)
	if err != nil {
		return "", fmt.Errorf("%w: failed to resolve API key: %v", ErrVeSearchConfig, err)
	}
	return key, nil
}

func (c *VeSearchConfig) execute(ctx context.Context, query string) (string, error) {
	body := veSearchChatCompletionRequest{
		BotID: c.Endpoint,
		Messages: []veSearchMessage{
			{
				Role:    "user",
				Content: query,
			},
		},
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, veSearchCompletionURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := doVeSearchRequest(httpClient, httpReq)
	if err != nil {
		return "", fmt.Errorf("vesearch request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read vesearch response failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if len(respBody) == 0 {
			return "", fmt.Errorf("vesearch HTTP error: status=%d", resp.StatusCode)
		}
		return "", fmt.Errorf("vesearch HTTP error: %s", string(respBody))
	}

	var parsed veSearchChatCompletionResponse
	if err = json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("unmarshal vesearch response failed: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("vesearch response choices is empty")
	}
	return parsed.Choices[0].Message.Content, nil
}
