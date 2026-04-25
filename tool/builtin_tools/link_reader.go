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
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const (
	defaultLinkReaderTimeout = 60 * time.Second
	maxLinkReaderURLs        = 3
	linkReaderPath           = "/tools/execute"
)

var linkReaderToolDescription = `
	Use this tool when you need to fetch content from web pages, PDFs, or Douyin videos.
	It retrieves the title and main content from the provided URLs.

	Args:
		url_list (list[str]): A list of URLs to parse, maximum 3.

	Returns:
		A list of parsed link content records returned by Ark LinkReader.
`

type LinkReaderConfig struct {
	APIKey     string
	BaseURL    string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type LinkReaderRequest struct {
	URLList []string `json:"url_list"`
}

type LinkReaderResult struct {
	Result []map[string]any `json:"result,omitempty"`
}

type linkReaderExecuteRequest struct {
	ActionName string         `json:"action_name"`
	ToolName   string         `json:"tool_name"`
	Parameters map[string]any `json:"parameters"`
}

type linkReaderExecuteResponse struct {
	StatusCode int    `json:"status_code"`
	Message    string `json:"message,omitempty"`
	Data       struct {
		ArkWebDataList []map[string]any `json:"ark_web_data_list"`
	} `json:"data"`
}

func NewLinkReaderTool(cfg *LinkReaderConfig) (tool.Tool, error) {
	if cfg == nil {
		cfg = &LinkReaderConfig{}
	}
	if cfg.APIKey == "" {
		cfg.APIKey = resolveLinkReaderAPIKey()
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = utils.GetEnvWithDefault(common.MODEL_AGENT_API_BASE, configs.GetGlobalConfig().Model.Agent.ApiBase, common.DEFAULT_MODEL_AGENT_API_BASE)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultLinkReaderTimeout
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	}

	log.Debug("Initializing link reader tool", "base_url", cfg.BaseURL)

	return functiontool.New(
		functiontool.Config{
			Name:        "link_reader",
			Description: linkReaderToolDescription,
		},
		cfg.linkReaderHandler)
}

func (c *LinkReaderConfig) linkReaderHandler(ctx tool.Context, req LinkReaderRequest) (LinkReaderResult, error) {
	urls, err := normalizeLinkReaderURLs(req.URLList)
	if err != nil {
		return LinkReaderResult{}, err
	}
	if len(urls) == 0 {
		return LinkReaderResult{Result: []map[string]any{}}, nil
	}

	executeCtx := context.Background()
	if ctx != nil {
		executeCtx = ctx
	}
	result, err := c.execute(executeCtx, urls)
	if err != nil {
		return LinkReaderResult{}, err
	}
	return LinkReaderResult{Result: result}, nil
}

func (c *LinkReaderConfig) execute(ctx context.Context, urls []string) ([]map[string]any, error) {
	body := linkReaderExecuteRequest{
		ActionName: "LinkReader",
		ToolName:   "LinkReader",
		Parameters: map[string]any{"url_list": urls},
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, buildLinkReaderURL(c.BaseURL), bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("link reader request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read link reader response failed: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("link reader HTTP error: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var parsed linkReaderExecuteResponse
	if err = json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal link reader response failed: %w", err)
	}
	if parsed.StatusCode != http.StatusOK {
		if parsed.Message != "" {
			return nil, fmt.Errorf("link reader failed: status_code=%d message=%s", parsed.StatusCode, parsed.Message)
		}
		return nil, fmt.Errorf("link reader failed: status_code=%d", parsed.StatusCode)
	}
	return parsed.Data.ArkWebDataList, nil
}

func resolveLinkReaderAPIKey() string {
	if key := utils.GetEnvWithDefault(common.MODEL_AGENT_API_KEY, configs.GetGlobalConfig().Model.Agent.ApiKey); key != "" {
		return key
	}
	return utils.Must(veauth.GetArkToken(common.DEFAULT_MODEL_REGION))
}

func normalizeLinkReaderURLs(urls []string) ([]string, error) {
	out := make([]string, 0, len(urls))
	for _, item := range urls {
		url := strings.TrimSpace(item)
		if url == "" {
			continue
		}
		out = append(out, url)
	}
	if len(out) > maxLinkReaderURLs {
		return nil, fmt.Errorf("link_reader supports at most %d URLs, got %d", maxLinkReaderURLs, len(out))
	}
	return out, nil
}

func buildLinkReaderURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + linkReaderPath
}
