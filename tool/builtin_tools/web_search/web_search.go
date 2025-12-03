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
	"fmt"
	"os"
	"strings"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/log"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

//The document of this tool see: https://www.volcengine.com/docs/85508/1650263

// WebSearchTool is a built-in tool that is automatically invoked by Agents
// models to retrieve search results from websites.
var WebSearchTool tool.Tool

func init() {
	var err error
	WebSearchTool, err = functiontool.New(
		functiontool.Config{
			Name: "web_search",
			Description: `Search a query in websites.
Args:
	query: The query to search.
Returns:
	A list of result documents.`,
		},
		WebSearch)
	if err != nil {
		panic(err)
	}
	log.Info("init WebSearchTool successful")
}

type webSearchArgs struct {
	Query string `json:"query" jsonschema:"The query to search"`
}

type webSearchResult struct {
	Result []string `json:"result,omitempty"`
}

func WebSearch(ctx tool.Context, args webSearchArgs) (webSearchResult, error) {
	// Search a query in websites.
	// Args:
	// 		query: The query to search.
	// Returns:
	//		A list of result documents.
	var ak string
	var sk string
	//var sessionToken string
	var out = webSearchResult{Result: make([]string, 0)}

	if ctx != nil {
		ak = getStringFromToolContext(ctx, common.VOLCENGINE_ACCESS_KEY)
		sk = getStringFromToolContext(ctx, common.VOLCENGINE_SECRET_KEY)
	}

	if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
		ak = getEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, "")
		sk = getEnvWithDefault(common.VOLCENGINE_SECRET_KEY, "")
	}

	if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
		cred, err := veauth.GetCredentialFromVeFaaSIAM()
		if err != nil {
			return out, err
		}
		ak = cred.AccessKeyID
		sk = cred.SecretAccessKey
		//sessionToken = cred.SessionToken
	}
	//header := map[string]string{"X-Security-Token": sessionToken}
	body := map[string]any{
		"Query":       args.Query,
		"SearchType":  "web",
		"Count":       5,
		"NeedSummary": true,
	}

	bodyBytes, _ := json.Marshal(body)

	webSearchClient := NewClient()
	resp, err := webSearchClient.DoRequest(ak, sk, nil, bodyBytes)
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

func getEnvWithDefault(key, defaultValue string) string {
	val, exists := os.LookupEnv(key)
	if !exists || strings.TrimSpace(val) == "" {
		return defaultValue
	}
	return val
}
