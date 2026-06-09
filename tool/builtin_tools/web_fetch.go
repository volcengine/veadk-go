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
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const (
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	acceptLanguage         = "en-US,en;q=0.9"
	defaultWebFetchTimeout = 30 * time.Second
	maxRedirects           = 3
	maxResponseBytes       = 2_000_000
	maxCharsCap            = 200_000
	defaultMaxChars        = 50_000
	cacheTTL               = 15 * time.Minute
	cacheMaxEntries        = 128
)

var ErrWebFetch = errors.New("web fetch error")

var (
	titlePattern           = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title\s*>`)
	scriptPattern          = regexp.MustCompile(`(?is)<script[^>]*>.*?</script(\s+[^>]*)?>`)
	stylePattern           = regexp.MustCompile(`(?is)<style[^>]*>.*?</style(\s+[^>]*)?>`)
	noscriptPattern        = regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript(\s+[^>]*)?>`)
	linkPattern            = regexp.MustCompile(`(?is)<a\s+[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a\s*>`)
	listItemPattern        = regexp.MustCompile(`(?is)<li[^>]*>(.*?)</li\s*>`)
	breakPattern           = regexp.MustCompile(`(?i)<(br|hr)\s*/?>`)
	blockClosePattern      = regexp.MustCompile(`(?i)</(p|div|section|article|header|footer|table|tr|ul|ol)\s*>`)
	tagPattern             = regexp.MustCompile(`<[^>]+>`)
	lineEndSpacePattern    = regexp.MustCompile(`[ \t]+\n`)
	blankLinesPattern      = regexp.MustCompile(`\n{3,}`)
	multipleSpacePattern   = regexp.MustCompile(`[ \t]{2,}`)
	imagePattern           = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)
	markdownLinkPattern    = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	inlineCodePattern      = regexp.MustCompile("`([^`]+)`")
	markdownHeadingPattern = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	markdownBulletPattern  = regexp.MustCompile(`(?m)^\s*[-*+]\s+`)
	markdownNumberPattern  = regexp.MustCompile(`(?m)^\s*\d+\.\s+`)
	metaRefreshPattern     = regexp.MustCompile(
		`(?i)<meta[^>]+http-equiv=["']?refresh["']?[^>]*content=["']?[^"'>]*?url=([^"'>\s]+)`,
	)
	headingPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1(\s+[^>]*)?>`),
		regexp.MustCompile(`(?is)<h2[^>]*>(.*?)</h2(\s+[^>]*)?>`),
		regexp.MustCompile(`(?is)<h3[^>]*>(.*?)</h3(\s+[^>]*)?>`),
		regexp.MustCompile(`(?is)<h4[^>]*>(.*?)</h4(\s+[^>]*)?>`),
		regexp.MustCompile(`(?is)<h5[^>]*>(.*?)</h5(\s+[^>]*)?>`),
		regexp.MustCompile(`(?is)<h6[^>]*>(.*?)</h6(\s+[^>]*)?>`),
	}
)

var webFetchToolDescription = `
	Fetch a web page over HTTP(S) and return its readable main content.

	Performs a plain HTTP GET without JavaScript execution and extracts readable
	text as markdown or plain text. It follows HTTP and meta-refresh redirects.
	Pages that require login or render entirely with JavaScript may be incomplete.
`

//go:noinline
func doWebFetchRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	return client.Do(req)
}

type WebFetchConfig struct {
	Timeout    time.Duration
	HTTPClient *http.Client
}

type WebFetchArgs struct {
	URL         string `json:"url" jsonschema:"The http(s) URL to fetch"`
	ExtractMode string `json:"extract_mode,omitempty" jsonschema:"markdown (default) or text"`
	MaxChars    int    `json:"max_chars,omitempty" jsonschema:"Truncate extracted content to at most this many characters"`
}

type WebFetchResult struct {
	URL       string `json:"url"`
	Title     string `json:"title,omitempty"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

type webFetchCache struct {
	mu sync.Mutex
	m  map[string]webFetchCacheEntry
}

type webFetchCacheEntry struct {
	expiresAt time.Time
	result    WebFetchResult
}

func NewWebFetchTool(cfg *WebFetchConfig) (tool.Tool, error) {
	if cfg == nil {
		cfg = &WebFetchConfig{}
	}
	cfg.applyDefaults()
	cache := &webFetchCache{m: make(map[string]webFetchCacheEntry)}

	return functiontool.New(
		functiontool.Config{
			Name:        "web_fetch",
			Description: webFetchToolDescription,
		},
		cfg.webFetchHandler(cache),
	)
}

func (c *WebFetchConfig) applyDefaults() {
	if c.Timeout <= 0 {
		c.Timeout = defaultWebFetchTimeout
	}

	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: c.Timeout}
	} else {
		client := *c.HTTPClient
		if client.Timeout <= 0 {
			client.Timeout = c.Timeout
		}
		c.HTTPClient = &client
	}
	c.HTTPClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
}

func (c *WebFetchConfig) webFetchHandler(
	cache *webFetchCache,
) func(tool.Context, WebFetchArgs) (WebFetchResult, error) {
	return func(ctx tool.Context, args WebFetchArgs) (WebFetchResult, error) {
		mode := "markdown"
		if args.ExtractMode == "text" {
			mode = "text"
		}

		maxChars := args.MaxChars
		if maxChars <= 0 {
			maxChars = defaultMaxChars
		}
		if maxChars < 1 {
			maxChars = 1
		}
		if maxChars > maxCharsCap {
			maxChars = maxCharsCap
		}

		key := args.URL + "\x00" + mode + "\x00" + strconv.Itoa(maxChars)
		if result, ok := cache.get(key); ok {
			return result, nil
		}

		executeCtx := context.Background()
		if ctx != nil {
			executeCtx = ctx
		}
		result, err := c.execute(executeCtx, args.URL, mode, maxChars)
		if err != nil {
			return WebFetchResult{}, fmt.Errorf("%w: %v", ErrWebFetch, err)
		}
		cache.set(key, result)
		return result, nil
	}
}

func (c *WebFetchConfig) execute(
	ctx context.Context,
	rawURL string,
	mode string,
	maxChars int,
) (WebFetchResult, error) {
	return fetchAndExtract(ctx, c.HTTPClient, rawURL, mode, maxChars)
}

func (c *webFetchCache) get(key string) (WebFetchResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.m[key]
	if !ok {
		return WebFetchResult{}, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.m, key)
		return WebFetchResult{}, false
	}
	return entry.result, true
}

func (c *webFetchCache) set(key string, result WebFetchResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.m == nil {
		c.m = make(map[string]webFetchCacheEntry)
	}
	if len(c.m) >= cacheMaxEntries {
		clear(c.m)
	}
	c.m[key] = webFetchCacheEntry{
		expiresAt: time.Now().Add(cacheTTL),
		result:    result,
	}
}

func assertPublicHost(host string) error {
	if host == "" {
		return fmt.Errorf("missing host")
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("DNS resolution failed for %q: %w", host, err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("blocked non-public address for host %q: %s", host, ip)
		}
	}
	return nil
}

func checkURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("only http(s) URLs are supported")
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("URL has no host")
	}
	if err = assertPublicHost(parsed.Hostname()); err != nil {
		return nil, err
	}
	return parsed, nil
}

func fetchAndExtract(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	mode string,
	maxChars int,
) (WebFetchResult, error) {
	current := rawURL
	hops := 0

	for {
		parsed, err := checkURL(current)
		if err != nil {
			return WebFetchResult{}, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return WebFetchResult{}, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept-Language", acceptLanguage)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		resp, err := doWebFetchRequest(client, req)
		if err != nil {
			return WebFetchResult{}, fmt.Errorf("request failed: %w", err)
		}

		location := resp.Header.Get("Location")
		if resp.StatusCode >= http.StatusMultipleChoices &&
			resp.StatusCode < http.StatusBadRequest && location != "" {
			closeWebFetchBody(resp.Body)
			hops++
			if hops > maxRedirects {
				return WebFetchResult{}, fmt.Errorf("too many redirects (>%d)", maxRedirects)
			}
			reference, parseErr := url.Parse(location)
			if parseErr != nil {
				return WebFetchResult{}, fmt.Errorf("invalid redirect URL: %w", parseErr)
			}
			current = parsed.ResolveReference(reference).String()
			continue
		}

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			closeWebFetchBody(resp.Body)
			return WebFetchResult{}, fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		closeWebFetchBody(resp.Body)
		if readErr != nil {
			return WebFetchResult{}, fmt.Errorf("read response: %w", readErr)
		}

		contentType := resp.Header.Get("Content-Type")
		if strings.Contains(strings.ToLower(contentType), "pdf") ||
			(len(raw) >= 5 && string(raw[:5]) == "%PDF-") {
			return WebFetchResult{
				URL:       current,
				Content:   "[PDF detected — extraction not supported in Go port]",
				Truncated: false,
			}, nil
		}

		body := decodeWebFetchBody(raw, contentType)
		isHTML := strings.Contains(strings.ToLower(contentType), "html") ||
			strings.Contains(strings.ToLower(contentType), "xml")

		if isHTML && hops < maxRedirects {
			if target := metaRefreshURL(body); target != "" {
				reference, parseErr := url.Parse(target)
				if parseErr != nil {
					return WebFetchResult{}, fmt.Errorf("invalid meta refresh URL: %w", parseErr)
				}
				next := parsed.ResolveReference(reference).String()
				if next != current {
					hops++
					current = next
					continue
				}
			}
		}

		if !isHTML {
			return buildResult(current, "", normalizeWhitespace(body), maxChars), nil
		}

		markdown, title := htmlToMarkdown(body)
		content := markdown
		if mode == "text" {
			content = markdownToText(markdown)
		}
		if content == "" {
			content = normalizeWhitespace(stripTags(body))
		}
		return buildResult(current, title, content, maxChars), nil
	}
}

func closeWebFetchBody(body io.ReadCloser) {
	if body != nil {
		_ = body.Close()
	}
}

func decodeWebFetchBody(raw []byte, contentType string) string {
	charset := "utf-8"
	if _, params, err := mime.ParseMediaType(contentType); err == nil {
		if value := strings.TrimSpace(params["charset"]); value != "" {
			charset = strings.ToLower(value)
		}
	}

	switch charset {
	case "iso-8859-1", "iso8859-1", "latin-1", "latin1":
		runes := make([]rune, len(raw))
		for i, value := range raw {
			runes[i] = rune(value)
		}
		return string(runes)
	default:
		return strings.ToValidUTF8(string(raw), "\uFFFD")
	}
}

func htmlToMarkdown(htmlText string) (string, string) {
	title := ""
	if match := titlePattern.FindStringSubmatch(htmlText); len(match) > 1 {
		title = normalizeWhitespace(stripTags(match[1]))
	}

	text := scriptPattern.ReplaceAllString(htmlText, "")
	text = stylePattern.ReplaceAllString(text, "")
	text = noscriptPattern.ReplaceAllString(text, "")

	text = linkPattern.ReplaceAllStringFunc(text, func(value string) string {
		match := linkPattern.FindStringSubmatch(value)
		if len(match) < 3 {
			return value
		}
		href := match[1]
		label := normalizeWhitespace(stripTags(match[2]))
		if label == "" {
			return href
		}
		return "[" + label + "](" + href + ")"
	})

	for index, pattern := range headingPatterns {
		level := index + 1
		text = pattern.ReplaceAllStringFunc(text, func(value string) string {
			match := pattern.FindStringSubmatch(value)
			if len(match) < 2 {
				return value
			}
			label := normalizeWhitespace(stripTags(match[1]))
			return "\n" + strings.Repeat("#", level) + " " + label + "\n"
		})
	}

	text = listItemPattern.ReplaceAllStringFunc(text, func(value string) string {
		match := listItemPattern.FindStringSubmatch(value)
		if len(match) < 2 {
			return value
		}
		label := normalizeWhitespace(stripTags(match[1]))
		if label == "" {
			return ""
		}
		return "\n- " + label
	})
	text = breakPattern.ReplaceAllString(text, "\n")
	text = blockClosePattern.ReplaceAllString(text, "\n")
	text = stripTags(text)
	return normalizeWhitespace(text), title
}

func markdownToText(markdown string) string {
	text := imagePattern.ReplaceAllString(markdown, "")
	text = markdownLinkPattern.ReplaceAllString(text, "$1")
	text = inlineCodePattern.ReplaceAllString(text, "$1")
	text = markdownHeadingPattern.ReplaceAllString(text, "")
	text = markdownBulletPattern.ReplaceAllString(text, "")
	text = markdownNumberPattern.ReplaceAllString(text, "")
	return normalizeWhitespace(text)
}

func normalizeWhitespace(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = lineEndSpacePattern.ReplaceAllString(value, "\n")
	value = blankLinesPattern.ReplaceAllString(value, "\n\n")
	value = multipleSpacePattern.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func stripTags(value string) string {
	return html.UnescapeString(tagPattern.ReplaceAllString(value, ""))
}

func metaRefreshURL(htmlText string) string {
	if len(htmlText) > 4096 {
		htmlText = htmlText[:4096]
	}
	match := metaRefreshPattern.FindStringSubmatch(htmlText)
	if len(match) < 2 {
		return ""
	}
	return html.UnescapeString(match[1])
}

func buildResult(rawURL, title, content string, maxChars int) WebFetchResult {
	runes := []rune(content)
	truncated := len(runes) > maxChars
	if truncated {
		runes = runes[:maxChars]
	}
	return WebFetchResult{
		URL:       rawURL,
		Title:     title,
		Content:   string(runes),
		Truncated: truncated,
	}
}
