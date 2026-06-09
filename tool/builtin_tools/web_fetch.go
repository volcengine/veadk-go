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
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const (
	defaultWebFetchTimeout       = 30 * time.Second
	defaultWebFetchMaxChars      = 50_000
	maxWebFetchChars             = 200_000
	defaultWebFetchResponseBytes = 2_000_000
	defaultWebFetchRedirects     = 3
	defaultWebFetchCacheTTL      = 15 * time.Minute
	maxWebFetchCacheEntries      = 128
)

const webFetchUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

var (
	webFetchSpacePattern      = regexp.MustCompile(`[ \t\f\v]+`)
	webFetchLineSpacePattern  = regexp.MustCompile(`[ \t]+\n`)
	webFetchBlankLinePattern  = regexp.MustCompile(`\n{3,}`)
	webFetchMetaURLPattern    = regexp.MustCompile(`(?i)\burl\s*=\s*['"]?([^'";]+)`)
	webFetchBlockedIPPrefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("100::/64"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
)

var webFetchToolDescription = `
	Fetch a public HTTP(S) URL without executing JavaScript and return readable content.
	HTML is converted to markdown or plain text. Redirects and resolved IP addresses are
	validated to block requests to private and internal networks.

	Args:
		url (string): The public HTTP(S) URL to fetch.
		extract_mode (string): "markdown" (default) or "text".
		max_chars (int): Maximum returned characters, up to 200000.

	Returns:
		The final URL, page title, extracted content, and whether it was truncated.
`

type WebFetchResolver func(ctx context.Context, host string) ([]netip.Addr, error)

type WebFetchConfig struct {
	Timeout          time.Duration
	MaxResponseBytes int64
	MaxRedirects     int
	CacheTTL         time.Duration
	HTTPClient       *http.Client
	ResolveHost      WebFetchResolver

	cacheMu sync.Mutex
	cache   map[webFetchCacheKey]webFetchCacheEntry
}

type WebFetchArgs struct {
	URL         string `json:"url" jsonschema:"The public HTTP(S) URL to fetch"`
	ExtractMode string `json:"extract_mode,omitempty" jsonschema:"Extraction mode: markdown or text"`
	MaxChars    int    `json:"max_chars,omitempty" jsonschema:"Maximum number of returned characters"`
}

type WebFetchResult struct {
	URL       string `json:"url"`
	Title     string `json:"title,omitempty"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

type webFetchCacheKey struct {
	URL      string
	Mode     string
	MaxChars int
}

type webFetchCacheEntry struct {
	ExpiresAt time.Time
	Result    WebFetchResult
}

func NewWebFetchTool(cfg *WebFetchConfig) (tool.Tool, error) {
	if cfg == nil {
		cfg = &WebFetchConfig{}
	}
	cfg.applyWebFetchDefaults()

	return functiontool.New(
		functiontool.Config{
			Name:        "web_fetch",
			Description: webFetchToolDescription,
		},
		cfg.webFetchHandler,
	)
}

func (c *WebFetchConfig) applyWebFetchDefaults() {
	if c.Timeout <= 0 {
		c.Timeout = defaultWebFetchTimeout
	}
	if c.MaxResponseBytes <= 0 {
		c.MaxResponseBytes = defaultWebFetchResponseBytes
	}
	if c.MaxRedirects <= 0 {
		c.MaxRedirects = defaultWebFetchRedirects
	}
	if c.CacheTTL <= 0 {
		c.CacheTTL = defaultWebFetchCacheTTL
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: c.Timeout}
	}
	if c.ResolveHost == nil {
		c.ResolveHost = resolveWebFetchHost
	}
	if c.cache == nil {
		c.cache = make(map[webFetchCacheKey]webFetchCacheEntry)
	}
}

func (c *WebFetchConfig) webFetchHandler(ctx tool.Context, args WebFetchArgs) (WebFetchResult, error) {
	c.applyWebFetchDefaults()

	rawURL := strings.TrimSpace(args.URL)
	if rawURL == "" {
		return WebFetchResult{}, fmt.Errorf("web_fetch URL is empty")
	}
	mode := strings.ToLower(strings.TrimSpace(args.ExtractMode))
	if mode == "" {
		mode = "markdown"
	}
	if mode != "markdown" && mode != "text" {
		return WebFetchResult{}, fmt.Errorf("web_fetch extract_mode must be markdown or text")
	}
	maxChars := args.MaxChars
	if maxChars <= 0 {
		maxChars = defaultWebFetchMaxChars
	}
	if maxChars > maxWebFetchChars {
		maxChars = maxWebFetchChars
	}

	key := webFetchCacheKey{URL: rawURL, Mode: mode, MaxChars: maxChars}
	if result, ok := c.loadWebFetchCache(key); ok {
		return result, nil
	}

	executeCtx := context.Background()
	if ctx != nil {
		executeCtx = ctx
	}
	result, err := c.fetchAndExtract(executeCtx, rawURL, mode, maxChars)
	if err != nil {
		return WebFetchResult{}, err
	}
	c.storeWebFetchCache(key, result)
	return result, nil
}

func (c *WebFetchConfig) fetchAndExtract(ctx context.Context, rawURL, mode string, maxChars int) (WebFetchResult, error) {
	currentURL := rawURL
	client := c.webFetchHTTPClient()

	for redirects := 0; ; redirects++ {
		parsedURL, err := c.validateWebFetchURL(ctx, currentURL)
		if err != nil {
			return WebFetchResult{}, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
		if err != nil {
			return WebFetchResult{}, fmt.Errorf("create web_fetch request: %w", err)
		}
		req.Header.Set("User-Agent", webFetchUserAgent)
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/plain;q=0.8,*/*;q=0.5")

		resp, err := client.Do(req)
		if err != nil {
			return WebFetchResult{}, fmt.Errorf("web_fetch request failed: %w", err)
		}

		if isWebFetchRedirect(resp.StatusCode) {
			location := resp.Header.Get("Location")
			_ = resp.Body.Close()
			if location == "" {
				return WebFetchResult{}, fmt.Errorf("web_fetch redirect missing Location header")
			}
			if redirects >= c.MaxRedirects {
				return WebFetchResult{}, fmt.Errorf("web_fetch exceeded %d redirects", c.MaxRedirects)
			}
			nextURL, err := parsedURL.Parse(location)
			if err != nil {
				return WebFetchResult{}, fmt.Errorf("parse web_fetch redirect: %w", err)
			}
			currentURL = nextURL.String()
			continue
		}

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			_ = resp.Body.Close()
			return WebFetchResult{}, fmt.Errorf("web_fetch HTTP error: status=%d", resp.StatusCode)
		}

		raw, bodyTruncated, err := readWebFetchBody(resp.Body, c.MaxResponseBytes)
		_ = resp.Body.Close()
		if err != nil {
			return WebFetchResult{}, fmt.Errorf("read web_fetch response: %w", err)
		}

		contentType := resp.Header.Get("Content-Type")
		if isWebFetchPDF(contentType, raw) {
			return truncateWebFetchResult(parsedURL.String(), "", "[PDF detected; text extraction is not supported]", maxChars, bodyTruncated), nil
		}

		decoded, err := decodeWebFetchBody(raw, contentType)
		if err != nil {
			return WebFetchResult{}, fmt.Errorf("decode web_fetch response: %w", err)
		}

		if !isWebFetchHTML(contentType, raw) {
			return truncateWebFetchResult(parsedURL.String(), "", normalizeWebFetchWhitespace(decoded), maxChars, bodyTruncated), nil
		}

		doc, err := html.Parse(strings.NewReader(decoded))
		if err != nil {
			return WebFetchResult{}, fmt.Errorf("parse web_fetch HTML: %w", err)
		}
		if next := webFetchMetaRefreshURL(doc, parsedURL); next != "" {
			if redirects >= c.MaxRedirects {
				return WebFetchResult{}, fmt.Errorf("web_fetch exceeded %d redirects", c.MaxRedirects)
			}
			currentURL = next
			continue
		}

		title, content := extractWebFetchHTML(doc, parsedURL, mode)
		return truncateWebFetchResult(parsedURL.String(), title, content, maxChars, bodyTruncated), nil
	}
}

func (c *WebFetchConfig) webFetchHTTPClient() *http.Client {
	client := *c.HTTPClient
	if client.Timeout <= 0 {
		client.Timeout = c.Timeout
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &client
}

func (c *WebFetchConfig) validateWebFetchURL(ctx context.Context, rawURL string) (*url.URL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid web_fetch URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("web_fetch only supports http(s) URLs")
	}
	host := parsedURL.Hostname()
	if host == "" {
		return nil, fmt.Errorf("web_fetch URL has no host")
	}

	addresses, err := c.ResolveHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve web_fetch host %q: %w", host, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("resolve web_fetch host %q: no addresses", host)
	}
	for _, address := range addresses {
		if !isPublicWebFetchAddress(address) {
			return nil, fmt.Errorf("web_fetch blocked non-public address for host %q: %s", host, address)
		}
	}
	return parsedURL, nil
}

func resolveWebFetchHost(ctx context.Context, host string) ([]netip.Addr, error) {
	if address, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{address}, nil
	}
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func isPublicWebFetchAddress(address netip.Addr) bool {
	if address.Is4In6() {
		address = address.Unmap()
	}
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() ||
		address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
		return false
	}
	for _, prefix := range webFetchBlockedIPPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func isWebFetchRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func readWebFetchBody(body io.Reader, limit int64) ([]byte, bool, error) {
	raw, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(raw)) > limit {
		return raw[:limit], true, nil
	}
	return raw, false, nil
}

func decodeWebFetchBody(raw []byte, contentType string) (string, error) {
	reader, err := charset.NewReader(bytes.NewReader(raw), contentType)
	if err != nil {
		return "", err
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func isWebFetchHTML(contentType string, raw []byte) bool {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if strings.Contains(mediaType, "html") || strings.Contains(mediaType, "xml") {
		return true
	}
	if mediaType == "" || mediaType == "application/octet-stream" {
		detected := http.DetectContentType(raw)
		return strings.Contains(detected, "html") || strings.Contains(detected, "xml")
	}
	return false
}

func isWebFetchPDF(contentType string, raw []byte) bool {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	return mediaType == "application/pdf" || bytes.HasPrefix(raw, []byte("%PDF-"))
}

func webFetchMetaRefreshURL(doc *html.Node, baseURL *url.URL) string {
	var target string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if target != "" {
			return
		}
		if node.Type == html.ElementNode && node.Data == "meta" {
			var httpEquiv, content string
			for _, attr := range node.Attr {
				switch strings.ToLower(attr.Key) {
				case "http-equiv":
					httpEquiv = attr.Val
				case "content":
					content = attr.Val
				}
			}
			if strings.EqualFold(strings.TrimSpace(httpEquiv), "refresh") {
				match := webFetchMetaURLPattern.FindStringSubmatch(content)
				if len(match) == 2 {
					if nextURL, err := baseURL.Parse(strings.TrimSpace(match[1])); err == nil {
						target = nextURL.String()
						return
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return target
}

func extractWebFetchHTML(doc *html.Node, baseURL *url.URL, mode string) (string, string) {
	title := ""
	if titleNode := findWebFetchElement(doc, "title"); titleNode != nil {
		title = normalizeWebFetchInline(webFetchNodeText(titleNode))
	}

	root := findWebFetchElement(doc, "body")
	if root == nil {
		root = doc
	}
	var builder strings.Builder
	renderWebFetchNode(&builder, root, baseURL, mode)
	return title, normalizeWebFetchWhitespace(builder.String())
}

func renderWebFetchNode(builder *strings.Builder, node *html.Node, baseURL *url.URL, mode string) {
	if node.Type == html.ElementNode {
		switch node.Data {
		case "script", "style", "noscript", "svg", "head":
			return
		case "br", "hr":
			builder.WriteByte('\n')
			return
		case "a":
			label := normalizeWebFetchInline(webFetchNodeText(node))
			href := webFetchAttribute(node, "href")
			if mode == "markdown" && label != "" && href != "" {
				if parsed, err := baseURL.Parse(href); err == nil {
					href = parsed.String()
				}
				fmt.Fprintf(builder, "[%s](%s)", label, href)
			} else {
				builder.WriteString(label)
			}
			return
		case "li":
			builder.WriteByte('\n')
			if mode == "markdown" {
				builder.WriteString("- ")
			}
		case "h1", "h2", "h3", "h4", "h5", "h6":
			builder.WriteByte('\n')
			if mode == "markdown" {
				level := int(node.Data[1] - '0')
				builder.WriteString(strings.Repeat("#", level))
				builder.WriteByte(' ')
			}
		case "p", "div", "section", "article", "header", "footer", "main",
			"nav", "aside", "table", "tr", "ul", "ol", "pre", "blockquote":
			builder.WriteByte('\n')
		}
	}

	if node.Type == html.TextNode {
		builder.WriteString(node.Data)
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		renderWebFetchNode(builder, child, baseURL, mode)
	}

	if node.Type == html.ElementNode {
		switch node.Data {
		case "p", "div", "section", "article", "header", "footer", "main",
			"nav", "aside", "table", "tr", "ul", "ol", "pre", "blockquote",
			"h1", "h2", "h3", "h4", "h5", "h6":
			builder.WriteByte('\n')
		}
	}
}

func findWebFetchElement(node *html.Node, name string) *html.Node {
	if node.Type == html.ElementNode && node.Data == name {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findWebFetchElement(child, name); found != nil {
			return found
		}
	}
	return nil
}

func webFetchNodeText(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func webFetchAttribute(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func normalizeWebFetchInline(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func normalizeWebFetchWhitespace(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = webFetchSpacePattern.ReplaceAllString(value, " ")
	value = webFetchLineSpacePattern.ReplaceAllString(value, "\n")
	value = webFetchBlankLinePattern.ReplaceAllString(value, "\n\n")
	return strings.TrimSpace(value)
}

func truncateWebFetchResult(rawURL, title, content string, maxChars int, alreadyTruncated bool) WebFetchResult {
	runes := []rune(content)
	truncated := alreadyTruncated || len(runes) > maxChars
	if len(runes) > maxChars {
		runes = runes[:maxChars]
	}
	return WebFetchResult{
		URL:       rawURL,
		Title:     title,
		Content:   string(runes),
		Truncated: truncated,
	}
}

func (c *WebFetchConfig) loadWebFetchCache(key webFetchCacheKey) (WebFetchResult, bool) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	entry, ok := c.cache[key]
	if !ok {
		return WebFetchResult{}, false
	}
	if time.Now().After(entry.ExpiresAt) {
		delete(c.cache, key)
		return WebFetchResult{}, false
	}
	return entry.Result, true
}

func (c *WebFetchConfig) storeWebFetchCache(key webFetchCacheKey, result WebFetchResult) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	if len(c.cache) >= maxWebFetchCacheEntries {
		clear(c.cache)
	}
	c.cache[key] = webFetchCacheEntry{
		ExpiresAt: time.Now().Add(c.CacheTTL),
		Result:    result,
	}
}
