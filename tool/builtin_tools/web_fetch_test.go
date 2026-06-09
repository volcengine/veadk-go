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
	"context"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func publicWebFetchResolver(_ context.Context, _ string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
}

func TestNewWebFetchTool(t *testing.T) {
	webFetchTool, err := NewWebFetchTool(&WebFetchConfig{})

	assert.NoError(t, err)
	assert.NotNil(t, webFetchTool)
}

func TestWebFetchHandlerExtractsMarkdownAndCaches(t *testing.T) {
	var requests atomic.Int32
	client := newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		assert.Equal(t, http.MethodGet, req.Method)
		assert.Contains(t, req.Header.Get("User-Agent"), "Chrome")

		resp := newJSONResponse(http.StatusOK, `<!doctype html>
			<html><head><title> Example Page </title></head>
			<body><script>ignore()</script><h1>Hello</h1>
			<p>Read <a href="/docs">the docs</a>.</p></body></html>`)
		resp.Header.Set("Content-Type", "text/html; charset=utf-8")
		return resp, nil
	})
	cfg := &WebFetchConfig{
		HTTPClient:  client,
		ResolveHost: publicWebFetchResolver,
	}

	result, err := cfg.webFetchHandler(nil, WebFetchArgs{URL: "https://example.com/page"})
	require.NoError(t, err)
	assert.Equal(t, "Example Page", result.Title)
	assert.Contains(t, result.Content, "# Hello")
	assert.Contains(t, result.Content, "[the docs](https://example.com/docs)")
	assert.NotContains(t, result.Content, "ignore")
	assert.False(t, result.Truncated)

	cached, err := cfg.webFetchHandler(nil, WebFetchArgs{URL: "https://example.com/page"})
	require.NoError(t, err)
	assert.Equal(t, result, cached)
	assert.Equal(t, int32(1), requests.Load())
}

func TestWebFetchHandlerTextAndTruncation(t *testing.T) {
	resp := newJSONResponse(http.StatusOK, "<html><body><h2>Title</h2><p>abcdef</p></body></html>")
	resp.Header.Set("Content-Type", "text/html")
	cfg := &WebFetchConfig{
		HTTPClient: newMockHTTPClient(func(_ *http.Request) (*http.Response, error) {
			return resp, nil
		}),
		ResolveHost: publicWebFetchResolver,
	}

	result, err := cfg.webFetchHandler(nil, WebFetchArgs{
		URL:         "https://example.com",
		ExtractMode: "text",
		MaxChars:    7,
	})
	require.NoError(t, err)
	assert.NotContains(t, result.Content, "#")
	assert.Len(t, []rune(result.Content), 7)
	assert.True(t, result.Truncated)
}

func TestWebFetchBlocksUnsafeTargets(t *testing.T) {
	cfg := &WebFetchConfig{}
	cfg.applyWebFetchDefaults()

	_, err := cfg.webFetchHandler(nil, WebFetchArgs{URL: "file:///etc/passwd"})
	assert.ErrorContains(t, err, "only supports http(s)")

	_, err = cfg.webFetchHandler(nil, WebFetchArgs{URL: "http://127.0.0.1/admin"})
	assert.ErrorContains(t, err, "blocked non-public address")

	_, err = cfg.webFetchHandler(nil, WebFetchArgs{URL: "http://169.254.169.254/latest/meta-data"})
	assert.ErrorContains(t, err, "blocked non-public address")
}

func TestWebFetchRevalidatesRedirects(t *testing.T) {
	var requests atomic.Int32
	cfg := &WebFetchConfig{
		HTTPClient: newMockHTTPClient(func(_ *http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"http://127.0.0.1/admin"}},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
		ResolveHost: func(_ context.Context, host string) ([]netip.Addr, error) {
			if host == "example.com" {
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			}
			return resolveWebFetchHost(context.Background(), host)
		},
	}

	_, err := cfg.webFetchHandler(nil, WebFetchArgs{URL: "https://example.com"})
	assert.ErrorContains(t, err, "blocked non-public address")
	assert.Equal(t, int32(1), requests.Load())
}

func TestWebFetchFollowsMetaRefresh(t *testing.T) {
	var requests atomic.Int32
	cfg := &WebFetchConfig{
		HTTPClient: newMockHTTPClient(func(req *http.Request) (*http.Response, error) {
			requests.Add(1)
			if req.URL.Path == "/" {
				resp := newJSONResponse(http.StatusOK, `<html><head>
					<meta http-equiv="refresh" content="0; url=/article"></head></html>`)
				resp.Header.Set("Content-Type", "text/html")
				return resp, nil
			}
			resp := newJSONResponse(http.StatusOK, "<html><body><p>Article body</p></body></html>")
			resp.Header.Set("Content-Type", "text/html")
			return resp, nil
		}),
		ResolveHost: publicWebFetchResolver,
	}

	result, err := cfg.webFetchHandler(nil, WebFetchArgs{URL: "https://example.com/"})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/article", result.URL)
	assert.Contains(t, result.Content, "Article body")
	assert.Equal(t, int32(2), requests.Load())
}

func TestIsPublicWebFetchAddress(t *testing.T) {
	assert.True(t, isPublicWebFetchAddress(netip.MustParseAddr("8.8.8.8")))
	assert.False(t, isPublicWebFetchAddress(netip.MustParseAddr("10.0.0.1")))
	assert.False(t, isPublicWebFetchAddress(netip.MustParseAddr("100.64.0.1")))
	assert.False(t, isPublicWebFetchAddress(netip.MustParseAddr("2001:db8::1")))
}
