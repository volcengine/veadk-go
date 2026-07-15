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

	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const defaultWebScraperTimeout = 60 * time.Second

var ErrWebScraperConfig = errors.New("web scraper config error")

//go:noinline
func doWebScraperRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	return client.Do(req)
}

type WebScraperConfig struct {
	APIKey     string
	Endpoint   string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type WebScraperArgs struct {
	Query string `json:"query" jsonschema:"The keyword to search"`
}

type WebScraperResult struct {
	Result string `json:"result,omitempty"`
}

type webScraperRequest struct {
	Query     string                     `json:"query"`
	Source    string                     `json:"source"`
	Parse     bool                       `json:"parse"`
	Limit     string                     `json:"limit"`
	StartPage string                     `json:"start_page"`
	Pages     string                     `json:"pages"`
	Context   []webScraperRequestContext `json:"context"`
}

type webScraperRequestContext struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type webScraperResponse struct {
	Results []struct {
		Content struct {
			Results struct {
				Organic []webScraperOrganicResult `json:"organic"`
			} `json:"results"`
		} `json:"content"`
	} `json:"results"`
}

type webScraperOrganicResult struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"desc"`
}

type webScraperRenderedResult struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func NewWebScraperTool(cfg *WebScraperConfig) (tool.Tool, error) {
	if cfg == nil {
		cfg = &WebScraperConfig{}
	}
	if err := cfg.applyDefaults(); err != nil {
		return nil, err
	}
	return functiontool.New(
		functiontool.Config{
			Name:        "web_scraper",
			Description: "通过搜索引擎检索单个关键词并返回结果",
		},
		cfg.webScraperHandler)
}

func (c *WebScraperConfig) webScraperHandler(ctx tool.Context, args WebScraperArgs) (WebScraperResult, error) {
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return WebScraperResult{}, fmt.Errorf("web_scraper query is empty")
	}

	executeCtx := context.Background()
	if ctx != nil {
		executeCtx = ctx
	}
	result, err := c.execute(executeCtx, query)
	if err != nil {
		return WebScraperResult{}, err
	}
	return WebScraperResult{Result: result}, nil
}

func (c *WebScraperConfig) applyDefaults() error {
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	if c.Endpoint == "" {
		c.Endpoint = strings.TrimSpace(utils.GetEnvWithDefault(common.TOOL_WEB_SCRAPER_ENDPOINT))
	}
	if c.Endpoint == "" {
		return fmt.Errorf("%w: %s is required", ErrWebScraperConfig, common.TOOL_WEB_SCRAPER_ENDPOINT)
	}

	c.APIKey = strings.TrimSpace(c.APIKey)
	if c.APIKey == "" {
		c.APIKey = strings.TrimSpace(utils.GetEnvWithDefault(common.TOOL_WEB_SCRAPER_API_KEY))
	}
	if c.APIKey == "" {
		return fmt.Errorf("%w: %s is required", ErrWebScraperConfig, common.TOOL_WEB_SCRAPER_API_KEY)
	}

	if c.Timeout <= 0 {
		c.Timeout = defaultWebScraperTimeout
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: c.Timeout}
	}
	return nil
}

func (c *WebScraperConfig) execute(ctx context.Context, query string) (string, error) {
	body := webScraperRequest{
		Query:     query,
		Source:    "google_search",
		Parse:     true,
		Limit:     "10",
		StartPage: "1",
		Pages:     "1",
		Context: []webScraperRequestContext{
			{
				Key:   "nfpr",
				Value: true,
			},
			{
				Key:   "safe_search",
				Value: false,
			},
			{
				Key:   "filter",
				Value: 1,
			},
		},
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("https://%s/v1/queries", c.Endpoint), bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-VE-Source", "google_search")
	httpReq.Header.Set("X-VE-API-Key", c.APIKey)

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := doWebScraperRequest(httpClient, httpReq)
	if err != nil {
		return "", fmt.Errorf("web_scraper request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read web_scraper response failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if len(respBody) == 0 {
			return "", fmt.Errorf("web_scraper HTTP error: status=%d", resp.StatusCode)
		}
		return "", fmt.Errorf("web_scraper HTTP error: %s", string(respBody))
	}

	var parsed webScraperResponse
	if err = json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("unmarshal web_scraper response failed: %w", err)
	}
	if len(parsed.Results) == 0 {
		return "", fmt.Errorf("web_scraper response results is empty")
	}
	return renderWebScraperResults(parsed.Results[0].Content.Results.Organic)
}

func renderWebScraperResults(results []webScraperOrganicResult) (string, error) {
	var builder strings.Builder
	for _, result := range results {
		rendered := webScraperRenderedResult(result)
		data, err := json.Marshal(rendered)
		if err != nil {
			return "", err
		}
		builder.Write(data)
	}
	return builder.String(), nil
}
