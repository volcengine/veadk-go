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
	"errors"
	"net/http"
	"testing"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
)

func TestNewVeSearchTool(t *testing.T) {
	tool, err := NewVeSearchTool(&VeSearchConfig{
		APIKey:   "test-api-key",
		Endpoint: "test-bot",
	})

	assert.NoError(t, err)
	assert.NotNil(t, tool)
}

func TestVeSearchConfigPriority(t *testing.T) {
	t.Setenv(common.TOOL_VESEARCH_ENDPOINT, "env-bot")

	cfg := &VeSearchConfig{
		APIKey:   "test-api-key",
		Endpoint: "cfg-bot",
	}
	tool, err := NewVeSearchTool(cfg)

	assert.NoError(t, err)
	assert.NotNil(t, tool)
	assert.Equal(t, "cfg-bot", cfg.Endpoint)
}

func TestVeSearchHandler(t *testing.T) {
	mockey.PatchConvey("success", t, func() {
		mockey.Mock(doVeSearchRequest).To(func(_ *http.Client, r *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, veSearchCompletionURL, r.URL.String())
			assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
			assert.Contains(t, r.Header.Get("Content-Type"), "application/json")

			var body veSearchChatCompletionRequest
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)
			assert.Equal(t, "test-bot", body.BotID)
			require.Len(t, body.Messages, 1)
			assert.Equal(t, "user", body.Messages[0].Role)
			assert.Equal(t, "test query", body.Messages[0].Content)

			return newJSONResponse(http.StatusOK, `{
				"choices": [
					{"message": {"role": "assistant", "content": "search result"}}
				]
			}`), nil
		}).Build()

		cfg := &VeSearchConfig{
			APIKey:     "test-api-key",
			Endpoint:   "test-bot",
			HTTPClient: http.DefaultClient,
		}

		result, err := cfg.veSearchHandler(nil, VeSearchArgs{Query: " test query "})

		assert.NoError(t, err)
		assert.Equal(t, "search result", result.Result)
	})
}

func TestVeSearchHandlerHTTPError(t *testing.T) {
	mockey.PatchConvey("non-200 response", t, func() {
		mockey.Mock(doVeSearchRequest).Return(newJSONResponse(http.StatusBadRequest, `bad request`), nil).Build()

		cfg := &VeSearchConfig{
			APIKey:     "test-api-key",
			Endpoint:   "test-bot",
			HTTPClient: http.DefaultClient,
		}

		result, err := cfg.veSearchHandler(nil, VeSearchArgs{Query: "test query"})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "bad request")
		assert.Empty(t, result.Result)
	})
}

func TestNewVeSearchToolConfigError(t *testing.T) {
	t.Run("missing endpoint", func(t *testing.T) {
		t.Setenv(common.TOOL_VESEARCH_ENDPOINT, "")

		tool, err := NewVeSearchTool(&VeSearchConfig{APIKey: "test-api-key"})

		assert.ErrorIs(t, err, ErrVeSearchConfig)
		assert.Contains(t, err.Error(), common.TOOL_VESEARCH_ENDPOINT)
		assert.Nil(t, tool)
	})

	t.Run("missing api key", func(t *testing.T) {
		t.Setenv(common.TOOL_VESEARCH_API_KEY, "")

		mockey.PatchConvey("empty token fallback", t, func() {
			mockey.Mock(veauth.GetVeSearchToken).Return("", nil).Build()

			tool, err := NewVeSearchTool(&VeSearchConfig{Endpoint: "test-bot"})

			assert.ErrorIs(t, err, ErrVeSearchConfig)
			assert.Contains(t, err.Error(), common.TOOL_VESEARCH_API_KEY)
			assert.Nil(t, tool)
		})
	})

	t.Run("token fallback error", func(t *testing.T) {
		t.Setenv(common.TOOL_VESEARCH_API_KEY, "")

		mockey.PatchConvey("token error", t, func() {
			mockey.Mock(veauth.GetVeSearchToken).Return("", errors.New("token error")).Build()

			tool, err := NewVeSearchTool(&VeSearchConfig{Endpoint: "test-bot"})

			assert.ErrorIs(t, err, ErrVeSearchConfig)
			assert.Contains(t, err.Error(), "token error")
			assert.Nil(t, tool)
		})
	})
}

func TestVeSearchHandlerEmptyQuery(t *testing.T) {
	cfg := &VeSearchConfig{
		APIKey:     "test-api-key",
		Endpoint:   "test-bot",
		HTTPClient: http.DefaultClient,
	}

	result, err := cfg.veSearchHandler(nil, VeSearchArgs{Query: " "})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "query is empty")
	assert.Empty(t, result.Result)
}
