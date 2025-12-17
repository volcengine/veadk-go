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
	"strings"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

//The document of this tools see: https://www.volcengine.com/docs/85508/1650263

// WebSearchTool is a built-in tools that is automatically invoked by Agents
// models to retrieve search results from websites.

var ErrWebSearchConfig = errors.New("web search config error")

type Config struct {
	AK           string
	SK           string
	SessionToken string
	Region       string
}

type WebSearchArgs struct {
	Query string `json:"query" jsonschema:"The query to search"`
}

type WebSearchResult struct {
	Result []string `json:"result,omitempty"`
}

func NewWebSearchTool(cfg *Config) (tool.Tool, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.AK == "" {
		cfg.AK = utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
	}
	if cfg.SK == "" {
		cfg.SK = utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	}
	if cfg.AK == "" || cfg.SK == "" {
		iam, err := veauth.GetCredentialFromVeFaaSIAM()
		if err != nil {
			log.Warn(fmt.Sprintf("%s : GetCredential error: %s", ErrWebSearchConfig.Error(), err.Error()))
		} else {
			cfg.AK = iam.AccessKeyID
			cfg.SK = iam.SecretAccessKey
			cfg.SessionToken = iam.SessionToken
		}
	}
	if cfg.Region == "" {
		cfg.Region = common.DEFAULT_WEB_SEARCH_REGION
	}

	handler := func(ctx tool.Context, args WebSearchArgs) (WebSearchResult, error) {
		var ak string
		var sk string
		var header map[string]string
		//var sessionToken string
		var out = WebSearchResult{Result: make([]string, 0)}

		if ctx != nil {
			ak = getStringFromToolContext(ctx, common.VOLCENGINE_ACCESS_KEY)
			sk = getStringFromToolContext(ctx, common.VOLCENGINE_SECRET_KEY)
		}

		if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
			ak = cfg.AK
			sk = cfg.SK
		}

		if cfg.SessionToken != "" {
			header = map[string]string{"X-Security-Token": cfg.SessionToken}
		}

		body := map[string]any{
			"Query":       args.Query,
			"SearchType":  "web",
			"Count":       5,
			"NeedSummary": true,
		}

		bodyBytes, _ := json.Marshal(body)

		webSearchClient := NewClient(cfg.Region)
		resp, err := webSearchClient.DoRequest(ak, sk, header, bodyBytes)
		if err != nil {
			return out, err
		}
		if len(resp.Result.WebResults) <= 0 {
			return out, fmt.Errorf("web search result is empty")
		}
		for _, item := range resp.Result.WebResults {
			out.Result = append(out.Result, item.Summary)
		}

		return out, nil
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
		handler)
}

func getStringFromToolContext(toolContext tool.Context, key string) string {
	var value string
	tmp, err := toolContext.State().Get(key)
	if err != nil {
		return value
	}
	value, ok := tmp.(string)
	if !ok {
		return value
	}
	return value
}
