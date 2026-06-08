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
	"encoding/json"
	"net/http"
	"testing"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volcengine/veadk-go/common"
)

func TestNewWebScraperTool(t *testing.T) {
	tool, err := NewWebScraperTool(&WebScraperConfig{
		APIKey:   "test-api-key",
		Endpoint: "scraper.example.com",
	})

	assert.NoError(t, err)
	assert.NotNil(t, tool)
}

func TestWebScraperHandler(t *testing.T) {
	mockey.PatchConvey("success", t, func() {
		mockey.Mock(doWebScraperRequest).To(func(_ *http.Client, r *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "https://scraper.example.com/v1/queries", r.URL.String())
			assert.Contains(t, r.Header.Get("Content-Type"), "application/json")
			assert.Equal(t, "google_search", r.Header.Get("X-VE-Source"))
			assert.Equal(t, "test-api-key", r.Header.Get("X-VE-API-Key"))

			var body webScraperRequest
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)
			assert.Equal(t, "test query", body.Query)
			assert.Equal(t, "google_search", body.Source)
			assert.True(t, body.Parse)
			assert.Equal(t, "10", body.Limit)
			assert.Equal(t, "1", body.StartPage)
			assert.Equal(t, "1", body.Pages)
			require.Len(t, body.Context, 3)
			assert.Equal(t, "nfpr", body.Context[0].Key)
			assert.Equal(t, true, body.Context[0].Value)
			assert.Equal(t, "safe_search", body.Context[1].Key)
			assert.Equal(t, false, body.Context[1].Value)
			assert.Equal(t, "filter", body.Context[2].Key)
			assert.Equal(t, float64(1), body.Context[2].Value)

			return newJSONResponse(http.StatusOK, `{
				"results": [
					{
						"content": {
							"results": {
								"organic": [
									{
										"url": "https://example.com/a",
										"title": "Example A",
										"desc": "Description A"
									}
								]
							}
						}
					}
				]
			}`), nil
		}).Build()

		cfg := &WebScraperConfig{
			APIKey:     "test-api-key",
			Endpoint:   "scraper.example.com",
			HTTPClient: http.DefaultClient,
		}

		result, err := cfg.webScraperHandler(nil, WebScraperArgs{Query: " test query "})

		assert.NoError(t, err)
		assert.Contains(t, result.Result, "https://example.com/a")
		assert.Contains(t, result.Result, "Example A")
		assert.Contains(t, result.Result, "description")
		assert.Contains(t, result.Result, "Description A")
	})
}

func TestNewWebScraperToolConfigError(t *testing.T) {
	t.Run("missing endpoint", func(t *testing.T) {
		t.Setenv(common.TOOL_WEB_SCRAPER_ENDPOINT, "")

		tool, err := NewWebScraperTool(&WebScraperConfig{APIKey: "test-api-key"})

		assert.ErrorIs(t, err, ErrWebScraperConfig)
		assert.Contains(t, err.Error(), common.TOOL_WEB_SCRAPER_ENDPOINT)
		assert.Nil(t, tool)
	})

	t.Run("missing api key", func(t *testing.T) {
		t.Setenv(common.TOOL_WEB_SCRAPER_API_KEY, "")

		tool, err := NewWebScraperTool(&WebScraperConfig{Endpoint: "scraper.example.com"})

		assert.ErrorIs(t, err, ErrWebScraperConfig)
		assert.Contains(t, err.Error(), common.TOOL_WEB_SCRAPER_API_KEY)
		assert.Nil(t, tool)
	})
}
