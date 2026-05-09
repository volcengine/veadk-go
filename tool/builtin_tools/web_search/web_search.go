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

package web_search

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/integrations/ve_sign"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

//The document of this tools see: https://www.volcengine.com/docs/85508/1650263
// WebSearchTool is a built-in tools that is automatically invoked by Agents
// models to retrieve search results from websites.

const (
	DefaultTopK = 5
)

var ErrWebSearchConfig = errors.New("web search config error")

var doWebSearchRequest = func(client *ve_sign.VeRequest) ([]byte, error) {
	return client.DoRequest()
}

func NewClient() *ve_sign.VeRequest {
	return &ve_sign.VeRequest{
		Method:  http.MethodPost,
		Scheme:  "https",
		Host:    "mercury.volcengineapi.com",
		Path:    "/",
		Service: "volc_torchlight_api",
		Region:  common.DEFAULT_WEB_SEARCH_REGION,
		Action:  "WebSearch",
		Version: "2025-01-01",
	}
}

type WebSearchArgs struct {
	Query string `json:"query" jsonschema:"The query to search"`
}

type WebSearchResult struct {
	Result []string `json:"result,omitempty"`
}

type ParallelWebSearchArgs struct {
	Queries []string `json:"queries" jsonschema:"The queries to search in parallel"`
}

type ParallelWebSearchResult struct {
	Result map[string][]string `json:"result,omitempty"`
	Errors map[string]string   `json:"errors,omitempty"`
}

type Config struct {
	TopK int
}

type webSearchCredential struct {
	AK     string
	SK     string
	Header map[string]string
}

func resolveWebSearchCredential(ctx tool.Context) webSearchCredential {
	var header map[string]string
	credential := webSearchCredential{}
	if ctx != nil {
		credential.AK = utils.GetStringFromToolContext(ctx, common.VOLCENGINE_ACCESS_KEY)
		credential.SK = utils.GetStringFromToolContext(ctx, common.VOLCENGINE_SECRET_KEY)
	}

	if strings.TrimSpace(credential.AK) == "" {
		credential.AK = utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
	}
	if strings.TrimSpace(credential.SK) == "" {
		credential.SK = utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	}

	if strings.TrimSpace(credential.AK) == "" || strings.TrimSpace(credential.SK) == "" {
		iam, err := veauth.GetCredentialFromVeFaaSIAM()
		if err != nil {
			log.Warn(fmt.Sprintf("%s : GetCredential error: %s", ErrWebSearchConfig.Error(), err.Error()))
		} else {
			credential.AK = iam.AccessKeyID
			credential.SK = iam.SecretAccessKey
			if iam.SessionToken != "" {
				header = map[string]string{"X-Security-Token": iam.SessionToken}
			}
		}
	}
	credential.Header = header
	return credential
}

func (c Config) topK() int {
	if c.TopK <= 0 {
		return DefaultTopK
	}
	return c.TopK
}

func (c Config) search(query string, credential webSearchCredential) ([]string, error) {
	var result *WebSearchResponse
	out := make([]string, 0)

	client := NewClient()
	client.AK = credential.AK
	client.SK = credential.SK
	client.Header = credential.Header

	body := map[string]any{
		"Query":       query,
		"SearchType":  "web",
		"Count":       c.topK(),
		"NeedSummary": true,
	}
	client.Body = body

	resp, err := doWebSearchRequest(client)
	if err != nil {
		return out, err
	}

	if err = json.Unmarshal(resp, &result); err != nil {
		return out, fmt.Errorf("web search unmarshal response err: %w", err)
	}

	if len(result.Result.WebResults) <= 0 {
		return out, fmt.Errorf("web search result is empty")
	}
	for _, item := range result.Result.WebResults {
		out = append(out, item.Summary)
	}

	return out, nil
}

func (c Config) webSearchHandler(ctx tool.Context, args WebSearchArgs) (WebSearchResult, error) {
	result, err := c.search(args.Query, resolveWebSearchCredential(ctx))
	if err != nil {
		return WebSearchResult{Result: make([]string, 0)}, err
	}
	return WebSearchResult{Result: result}, nil
}

func (c Config) parallelWebSearchHandler(ctx tool.Context, args ParallelWebSearchArgs) (ParallelWebSearchResult, error) {
	out := ParallelWebSearchResult{
		Result: make(map[string][]string),
		Errors: make(map[string]string),
	}
	if len(args.Queries) == 0 {
		return out, nil
	}

	credential := resolveWebSearchCredential(ctx)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, query := range args.Queries {
		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}

		wg.Add(1)
		go func(q string) {
			defer wg.Done()
			result, err := c.search(q, credential)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out.Errors[q] = err.Error()
				return
			}
			out.Result[q] = result
		}(query)
	}
	wg.Wait()

	if len(out.Errors) == 0 {
		out.Errors = nil
	}
	return out, nil
}

func NewWebSearchTool(cfg *Config) (tool.Tool, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	return functiontool.New(
		functiontool.Config{
			Name: "web_search",
			Description: `A tools to retrieve information from the websites.
Args:
	query: The query to search.
Returns:
	A list of result documents.`,
		},
		cfg.webSearchHandler)
}

func NewParallelWebSearchTool(cfg *Config) (tool.Tool, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	return functiontool.New(
		functiontool.Config{
			Name: "parallel_web_search",
			Description: `Search multiple queries from websites in parallel.
Args:
queries: The queries to search. Each query will be searched in parallel.
Returns:
A map of query to result documents, plus per-query errors if any.`,
		},
		cfg.parallelWebSearchHandler)
}
