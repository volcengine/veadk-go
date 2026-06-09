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
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const webFetchTestURL = "https://93.184.216.34/article"

func newWebFetchResponse(status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{contentType},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func TestNewWebFetchTool(t *testing.T) {
	cfg := &WebFetchConfig{}

	webFetchTool, err := NewWebFetchTool(cfg)

	assert.NoError(t, err)
	assert.NotNil(t, webFetchTool)
	assert.Equal(t, defaultWebFetchTimeout, cfg.Timeout)
	assert.NotNil(t, cfg.HTTPClient)
}

func TestWebFetchHandlerExtractsAndTruncates(t *testing.T) {
	mockey.PatchConvey("extract HTML and truncate by characters", t, func() {
		var requests atomic.Int32
		mockey.Mock(doWebFetchRequest).To(func(_ *http.Client, req *http.Request) (*http.Response, error) {
			requests.Add(1)
			assert.Equal(t, http.MethodGet, req.Method)
			assert.Equal(t, webFetchTestURL, req.URL.String())
			assert.Equal(t, userAgent, req.Header.Get("User-Agent"))
			assert.Equal(t, acceptLanguage, req.Header.Get("Accept-Language"))
			assert.Contains(t, req.Header.Get("Accept"), "text/html")

			return newWebFetchResponse(http.StatusOK, "text/html; charset=utf-8", `
				<html>
					<head>
						<title>Example &amp; Docs</title>
						<style>.hidden { display: none; }</style>
					</head>
					<body>
						<script>ignore()</script>
						<h1>Readable heading</h1>
						<p>Useful body with <a href="https://example.com/docs">the docs</a>.</p>
					</body>
				</html>
			`), nil
		}).Build()

		cfg := &WebFetchConfig{HTTPClient: http.DefaultClient}
		cfg.applyDefaults()
		handler := cfg.webFetchHandler(&webFetchCache{m: make(map[string]webFetchCacheEntry)})

		result, err := handler(nil, WebFetchArgs{URL: webFetchTestURL})
		require.NoError(t, err)
		assert.Equal(t, webFetchTestURL, result.URL)
		assert.Equal(t, "Example & Docs", result.Title)
		assert.Contains(t, result.Content, "# Readable heading")
		assert.Contains(t, result.Content, "Useful body")
		assert.Contains(t, result.Content, "[the docs](https://example.com/docs)")
		assert.NotContains(t, result.Content, "ignore")
		assert.False(t, result.Truncated)

		truncated, err := handler(nil, WebFetchArgs{
			URL:      webFetchTestURL,
			MaxChars: 12,
		})
		require.NoError(t, err)
		assert.Len(t, []rune(truncated.Content), 12)
		assert.True(t, truncated.Truncated)

		plainText, err := handler(nil, WebFetchArgs{
			URL:         webFetchTestURL,
			ExtractMode: "text",
		})
		require.NoError(t, err)
		assert.Contains(t, plainText.Content, "Readable heading")
		assert.Contains(t, plainText.Content, "the docs")
		assert.NotContains(t, plainText.Content, "# Readable heading")
		assert.NotContains(t, plainText.Content, "[the docs]")
		assert.Equal(t, int32(3), requests.Load())
	})
}

func TestWebFetchHandlerBlocksUnsafeURLs(t *testing.T) {
	mockey.PatchConvey("reject unsafe URLs before HTTP request", t, func() {
		var requests atomic.Int32
		mockey.Mock(doWebFetchRequest).To(func(_ *http.Client, _ *http.Request) (*http.Response, error) {
			requests.Add(1)
			return newWebFetchResponse(http.StatusOK, "text/plain", "unexpected"), nil
		}).Build()

		cfg := &WebFetchConfig{HTTPClient: http.DefaultClient}
		cfg.applyDefaults()
		handler := cfg.webFetchHandler(&webFetchCache{m: make(map[string]webFetchCacheEntry)})

		_, err := handler(nil, WebFetchArgs{URL: "http://127.0.0.1/"})
		assert.ErrorIs(t, err, ErrWebFetch)
		assert.ErrorContains(t, err, "blocked non-public address")

		_, err = handler(nil, WebFetchArgs{URL: "ftp://x"})
		assert.ErrorIs(t, err, ErrWebFetch)
		assert.ErrorContains(t, err, "only http(s)")
		assert.Equal(t, int32(0), requests.Load())
	})
}

func TestWebFetchHandlerCachesResults(t *testing.T) {
	mockey.PatchConvey("cache repeated fetches", t, func() {
		var requests atomic.Int32
		mockey.Mock(doWebFetchRequest).To(func(_ *http.Client, _ *http.Request) (*http.Response, error) {
			requests.Add(1)
			return newWebFetchResponse(
				http.StatusOK,
				"text/html",
				"<html><body><p>cached body</p></body></html>",
			), nil
		}).Build()

		cfg := &WebFetchConfig{HTTPClient: http.DefaultClient}
		cfg.applyDefaults()
		handler := cfg.webFetchHandler(&webFetchCache{m: make(map[string]webFetchCacheEntry)})
		args := WebFetchArgs{URL: webFetchTestURL}

		first, err := handler(nil, args)
		require.NoError(t, err)
		second, err := handler(nil, args)
		require.NoError(t, err)

		assert.Equal(t, first, second)
		assert.Equal(t, int32(1), requests.Load())
	})
}
