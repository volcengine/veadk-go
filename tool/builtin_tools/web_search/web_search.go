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

type Config struct {
	TopK int
}

func (c Config) webSearchHandler(ctx tool.Context, args WebSearchArgs) (WebSearchResult, error) {
	var ak string
	var sk string
	var header map[string]string
	var result *WebSearchResponse
	var out = WebSearchResult{Result: make([]string, 0)}

	client := NewClient()
	if ctx != nil {
		client.AK = utils.GetStringFromToolContext(ctx, common.VOLCENGINE_ACCESS_KEY)
		client.SK = utils.GetStringFromToolContext(ctx, common.VOLCENGINE_SECRET_KEY)
	}

	if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
		client.AK = utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
		client.SK = utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	}

	if strings.TrimSpace(client.AK) == "" || strings.TrimSpace(client.SK) == "" {
		iam, err := veauth.GetCredentialFromVeFaaSIAM()
		if err != nil {
			log.Warn(fmt.Sprintf("%s : GetCredential error: %s", ErrWebSearchConfig.Error(), err.Error()))
		} else {
			client.AK = iam.AccessKeyID
			client.SK = iam.SecretAccessKey
			if iam.SessionToken != "" {
				header = map[string]string{"X-Security-Token": iam.SessionToken}
			}
		}
	}

	client.Header = header

	if c.TopK <= 0 {
		c.TopK = DefaultTopK
	}

	body := map[string]any{
		"Query":       args.Query,
		"SearchType":  "web",
		"Count":       c.TopK,
		"NeedSummary": true,
	}
	client.Body = body

	resp, err := client.DoRequest()
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
		out.Result = append(out.Result, item.Summary)
	}

	return out, nil
}

func NewWebSearchTool(cfg *Config) (tool.Tool, error) {
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
