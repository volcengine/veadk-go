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
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func newMockHTTPClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func newJSONResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestNewLinkReaderTool(t *testing.T) {
	tool, err := NewLinkReaderTool(&LinkReaderConfig{
		APIKey:  "test-api-key",
		BaseURL: "https://ark.example.com/api/v3",
	})

	assert.NoError(t, err)
	assert.NotNil(t, tool)
}

func TestLinkReaderHandler(t *testing.T) {
	httpClient := newMockHTTPClient(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "https://ark.example.com/api/v3/tools/execute", r.URL.String())
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		assert.Contains(t, r.Header.Get("Content-Type"), "application/json")

		var body linkReaderExecuteRequest
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "LinkReader", body.ActionName)
		assert.Equal(t, "LinkReader", body.ToolName)
		assert.Equal(t, []any{"https://example.com/a", "https://example.com/b"}, body.Parameters["url_list"])

		return newJSONResponse(http.StatusOK, `{
			"status_code": 200,
			"data": {
				"ark_web_data_list": [
					{"title": "A", "content": "content A"},
					{"title": "B", "content": "content B"}
				]
			}
		}`), nil
	})

	cfg := &LinkReaderConfig{
		APIKey:     "test-api-key",
		BaseURL:    "https://ark.example.com/api/v3/",
		HTTPClient: httpClient,
	}

	result, err := cfg.linkReaderHandler(nil, LinkReaderRequest{
		URLList: []string{" https://example.com/a ", "", "https://example.com/b"},
	})

	assert.NoError(t, err)
	require.Len(t, result.Result, 2)
	assert.Equal(t, "A", result.Result[0]["title"])
	assert.Equal(t, "content B", result.Result[1]["content"])
}

func TestLinkReaderHandlerEmptyURLs(t *testing.T) {
	cfg := &LinkReaderConfig{HTTPClient: http.DefaultClient}

	result, err := cfg.linkReaderHandler(nil, LinkReaderRequest{URLList: []string{" ", ""}})

	assert.NoError(t, err)
	assert.Empty(t, result.Result)
}

func TestLinkReaderExecuteAPIError(t *testing.T) {
	cfg := &LinkReaderConfig{
		APIKey:  "test-api-key",
		BaseURL: "https://ark.example.com/api/v3",
		HTTPClient: newMockHTTPClient(func(r *http.Request) (*http.Response, error) {
			return newJSONResponse(http.StatusOK, `{"status_code": 400, "message": "bad url"}`), nil
		}),
	}

	result, err := cfg.linkReaderHandler(nil, LinkReaderRequest{URLList: []string{"https://example.com"}})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status_code=400")
	assert.Empty(t, result.Result)
}

func TestLinkReaderExecuteHTTPError(t *testing.T) {
	cfg := &LinkReaderConfig{
		APIKey:  "test-api-key",
		BaseURL: "https://ark.example.com/api/v3",
		HTTPClient: newMockHTTPClient(func(r *http.Request) (*http.Response, error) {
			return newJSONResponse(http.StatusInternalServerError, `internal error`), nil
		}),
	}

	_, err := cfg.linkReaderHandler(nil, LinkReaderRequest{URLList: []string{"https://example.com"}})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP error")
}

func TestNormalizeLinkReaderURLs(t *testing.T) {
	urls, err := normalizeLinkReaderURLs([]string{" a ", "", "b"})
	assert.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, urls)

	_, err = normalizeLinkReaderURLs([]string{"a", "b", "c", "d"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at most 3 URLs")
}

func TestBuildLinkReaderURL(t *testing.T) {
	assert.Equal(t, "https://ark.example.com/api/v3/tools/execute", buildLinkReaderURL("https://ark.example.com/api/v3"))
	assert.Equal(t, "https://ark.example.com/api/v3/tools/execute", buildLinkReaderURL("https://ark.example.com/api/v3/"))
}
