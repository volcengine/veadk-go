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
	"errors"
	"github.com/volcengine/veadk-go/log"
	"sync"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/integrations/ve_sign"
	"github.com/volcengine/veadk-go/utils"
)

func mockWebSearchRequest(t *testing.T, fn func(client *ve_sign.VeRequest) ([]byte, error)) {
	t.Helper()
	old := doWebSearchRequest
	doWebSearchRequest = fn
	t.Cleanup(func() {
		doWebSearchRequest = old
	})
}

func setWebSearchCredentialEnv(t *testing.T) {
	t.Helper()
	t.Setenv(common.VOLCENGINE_ACCESS_KEY, "test-ak")
	t.Setenv(common.VOLCENGINE_SECRET_KEY, "test-sk")
}

func TestWebSearchHandler(t *testing.T) {
	setWebSearchCredentialEnv(t)
	mockWebSearchRequest(t, func(client *ve_sign.VeRequest) ([]byte, error) {
		body := client.Body.(map[string]any)
		assert.Equal(t, "golang", body["Query"])
		assert.Equal(t, "web", body["SearchType"])
		assert.Equal(t, DefaultTopK, body["Count"])
		assert.Equal(t, true, body["NeedSummary"])
		assert.Equal(t, "test-ak", client.AK)
		assert.Equal(t, "test-sk", client.SK)
		return []byte(`{"Result":{"WebResults":[{"Summary":"summary one"},{"Summary":"summary two"}]}}`), nil
	})

	result, err := Config{}.webSearchHandler(nil, WebSearchArgs{Query: "golang"})

	assert.NoError(t, err)
	assert.Equal(t, []string{"summary one", "summary two"}, result.Result)
}

func TestWebSearchHandlerCustomTopK(t *testing.T) {
	setWebSearchCredentialEnv(t)
	mockWebSearchRequest(t, func(client *ve_sign.VeRequest) ([]byte, error) {
		body := client.Body.(map[string]any)
		assert.Equal(t, 3, body["Count"])
		return []byte(`{"Result":{"WebResults":[{"Summary":"summary"}]}}`), nil
	})

	result, err := Config{TopK: 3}.webSearchHandler(nil, WebSearchArgs{Query: "golang"})

	assert.NoError(t, err)
	assert.Equal(t, []string{"summary"}, result.Result)
}

func TestWebSearchHandlerEmptyResult(t *testing.T) {
	setWebSearchCredentialEnv(t)
	mockWebSearchRequest(t, func(client *ve_sign.VeRequest) ([]byte, error) {
		return []byte(`{"Result":{"WebResults":[]}}`), nil
	})

	result, err := Config{}.webSearchHandler(nil, WebSearchArgs{Query: "golang"})

	assert.Error(t, err)
	assert.Empty(t, result.Result)
}

func TestParallelWebSearchHandler(t *testing.T) {
	setWebSearchCredentialEnv(t)
	var mu sync.Mutex
	queries := make(map[string]bool)
	mockWebSearchRequest(t, func(client *ve_sign.VeRequest) ([]byte, error) {
		body := client.Body.(map[string]any)
		query := body["Query"].(string)
		mu.Lock()
		queries[query] = true
		mu.Unlock()

		if query == "bad" {
			return nil, errors.New("search failed")
		}
		return []byte(`{"Result":{"WebResults":[{"Summary":"summary for ` + query + `"}]}}`), nil
	})

	result, err := Config{TopK: 2}.parallelWebSearchHandler(nil, ParallelWebSearchArgs{
		Queries: []string{"alpha", "bad", " ", "beta"},
	})

	assert.NoError(t, err)
	assert.Equal(t, []string{"summary for alpha"}, result.Result["alpha"])
	assert.Equal(t, []string{"summary for beta"}, result.Result["beta"])
	assert.Contains(t, result.Errors["bad"], "search failed")
	assert.NotContains(t, result.Result, "")
	assert.True(t, queries["alpha"])
	assert.True(t, queries["beta"])
	assert.True(t, queries["bad"])
	assert.False(t, queries[""])
}

func TestParallelWebSearchHandlerEmptyQueries(t *testing.T) {
	result, err := Config{}.parallelWebSearchHandler(nil, ParallelWebSearchArgs{})

	assert.NoError(t, err)
	assert.Empty(t, result.Result)
	assert.Empty(t, result.Errors)
}

func TestNewWebSearchTools(t *testing.T) {
	webSearchTool, err := NewWebSearchTool(nil)
	assert.NoError(t, err)
	assert.NotNil(t, webSearchTool)

	parallelTool, err := NewParallelWebSearchTool(nil)
	assert.NoError(t, err)
	assert.NotNil(t, parallelTool)
}

func TestClient_DoRequest(t *testing.T) {

	ak := utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, "")
	sk := utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, "")
	if ak == "" || sk == "" {
		t.Skip()
	}

	cfg := Config{}
	result, err := cfg.webSearchHandler(nil, WebSearchArgs{
		Query: "How to create a LLMAgent?",
	})
	if err != nil {
		log.Errorf("web search client DoRequest error: %v", err)
		return
	}

	log.Infof("response: %+v", result.Result)

}
