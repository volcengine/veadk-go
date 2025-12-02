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
	"veadk-go/auth/veauth"
	"veadk-go/consts"

	"google.golang.org/adk/tool"
)

//The document of this tool see: https://www.volcengine.com/docs/85508/1650263

func WebSearch(query string, toolContext tool.Context) ([]string, error) {
	// Search a query in websites.
	// Args:
	// 		query: The query to search.
	// Returns:
	//		A list of result documents.
	var ak string
	var sk string
	//var sessionToken string
	var out []string

	if toolContext != nil {
		ak = getStringFromToolContext(toolContext, consts.VOLCENGINE_ACCESS_KEY)
		sk = getStringFromToolContext(toolContext, consts.VOLCENGINE_SECRET_KEY)
	}

	if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
		ak = getEnvWithDefault(consts.VOLCENGINE_ACCESS_KEY, "")
		sk = getEnvWithDefault(consts.VOLCENGINE_SECRET_KEY, "")
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
		"Query":       query,
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
		return nil, fmt.Errorf("web search result is empty")
	}
	for _, item := range resp.Result.WebResults {
		out = append(out, item.Summary)
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
