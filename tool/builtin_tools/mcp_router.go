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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/volcengine/veadk-go/v2/auth/veauth"
	"github.com/volcengine/veadk-go/v2/common"
	"github.com/volcengine/veadk-go/v2/configs"
	"github.com/volcengine/veadk-go/v2/integrations/ve_sign"
	"github.com/volcengine/veadk-go/v2/log"
	"github.com/volcengine/veadk-go/v2/utils"
	"golang.org/x/oauth2"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
)

const (
	defaultAgentKitMCPHost       = "open.volcengineapi.com"
	defaultAgentKitActionVersion = "2025-10-30"
	defaultMCPScheme             = "https"
)

var (
	errMCPRouterURLRequired = errors.New("mcp router url is required")
	mcpRouterDoRequest      = func(req ve_sign.VeRequest) ([]byte, error) {
		return req.DoRequest()
	}
	mcpRouterToolsetNew = mcptoolset.New
)

type MCPRouterConfig struct {
	ID               string
	URL              string
	APIKey           string
	Region           string
	ServiceCode      string
	Host             string
	Scheme           string
	CredentialSource veauth.CredentialSource
}

type MCPRouterOption func(*MCPRouterConfig)

func WithMCPRouterID(id string) MCPRouterOption {
	return func(config *MCPRouterConfig) {
		config.ID = id
	}
}

func WithMCPRouterURL(url string) MCPRouterOption {
	return func(config *MCPRouterConfig) {
		config.URL = url
	}
}

func WithMCPRouterAPIKey(apiKey string) MCPRouterOption {
	return func(config *MCPRouterConfig) {
		config.APIKey = apiKey
	}
}

func WithMCPRouterCredentialSource(source veauth.CredentialSource) MCPRouterOption {
	return func(config *MCPRouterConfig) {
		config.CredentialSource = source
	}
}

func agentkitMCPTransport(ctx context.Context, url string, apiKey string) mcp.Transport {
	var httpClient *http.Client
	if strings.TrimSpace(apiKey) != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: apiKey})
		httpClient = oauth2.NewClient(ctx, ts)
	}
	return &mcp.StreamableClientTransport{
		Endpoint:   url,
		HTTPClient: httpClient,
	}
}

func NewMcpRouter() tool.Toolset {
	mcpRouter, err := NewMcpRouterToolset()
	if err != nil {
		log.Warn(fmt.Sprintf("NewMcpRouter failed: %s", err.Error()))
		return nil
	}
	return mcpRouter
}

func NewMcpRouterToolset(opts ...MCPRouterOption) (tool.Toolset, error) {
	ctx := context.Background()
	config := defaultMCPRouterConfig()
	for _, opt := range opts {
		opt(&config)
	}

	resolved, err := resolveMCPRouterConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(resolved.URL) == "" {
		return nil, fmt.Errorf("%w: set %s, config tools.mcp_router.url, or %s/%s",
			errMCPRouterURLRequired, common.TOOL_MCP_ROUTER_URL, common.MCP_ROUTER_ID, common.MCP_TOOLSET_ID)
	}

	return mcpRouterToolsetNew(mcptoolset.Config{
		Transport: agentkitMCPTransport(ctx, resolved.URL, resolved.APIKey),
	})
}

func defaultMCPRouterConfig() MCPRouterConfig {
	global := configs.GetGlobalConfig()
	router := &configs.MCPRouter{}
	if global != nil && global.Tool != nil && global.Tool.MCPRouter != nil {
		router = global.Tool.MCPRouter
	}
	return MCPRouterConfig{
		ID:          utils.GetEnvWithDefault(common.MCP_ROUTER_ID, utils.GetEnvWithDefault(common.MCP_TOOLSET_ID), router.ID),
		URL:         utils.GetEnvWithDefault(common.TOOL_MCP_ROUTER_URL, router.Url),
		APIKey:      utils.GetEnvWithDefault(common.TOOL_MCP_ROUTER_API_KEY, router.ApiKey),
		Region:      utils.GetEnvWithDefault(common.AGENTKIT_TOOL_REGION, common.DEFAULT_AGENTKIT_TOOL_REGION),
		ServiceCode: utils.GetEnvWithDefault(common.AGENTKIT_TOOL_SERVICE_CODE, common.DEFAULT_AGENTKIT_TOOL_SERVICE_CODE),
		Host:        utils.GetEnvWithDefault(common.AGENTKIT_TOOL_HOST, defaultAgentKitMCPHost),
		Scheme:      defaultMCPScheme,
	}
}

func resolveMCPRouterConfig(ctx context.Context, config MCPRouterConfig) (MCPRouterConfig, error) {
	id := strings.TrimSpace(config.ID)
	if id == "" {
		return config, nil
	}

	resolved, err := resolveAgentKitMCPToolset(ctx, config)
	if err != nil {
		return MCPRouterConfig{}, fmt.Errorf("resolve mcp router id %q: %w", id, err)
	}
	if strings.TrimSpace(config.URL) != "" {
		resolved.URL = config.URL
	}
	if strings.TrimSpace(config.APIKey) != "" {
		resolved.APIKey = config.APIKey
	}
	resolved.ID = id
	resolved.Region = config.Region
	resolved.ServiceCode = config.ServiceCode
	resolved.Host = config.Host
	resolved.Scheme = config.Scheme
	resolved.CredentialSource = config.CredentialSource
	return resolved, nil
}

func resolveAgentKitMCPToolset(ctx context.Context, config MCPRouterConfig) (MCPRouterConfig, error) {
	source := config.CredentialSource
	if source == nil {
		source = defaultMCPRouterCredentialSource()
	}
	credential, err := source.Credential(ctx)
	if err != nil {
		return MCPRouterConfig{}, err
	}

	header := map[string]string(nil)
	if strings.TrimSpace(credential.SessionToken) != "" {
		header = map[string]string{"X-Security-Token": credential.SessionToken}
	}
	body := map[string]any{"MCPToolsetId": config.ID}
	respBody, err := mcpRouterDoRequest(ve_sign.VeRequest{
		AK:      credential.AccessKeyID,
		SK:      credential.SecretAccessKey,
		Method:  http.MethodPost,
		Scheme:  defaultString(config.Scheme, defaultMCPScheme),
		Host:    defaultString(config.Host, defaultAgentKitMCPHost),
		Path:    "/",
		Service: defaultString(config.ServiceCode, common.DEFAULT_AGENTKIT_TOOL_SERVICE_CODE),
		Region:  defaultString(config.Region, common.DEFAULT_AGENTKIT_TOOL_REGION),
		Action:  "GetMCPToolset",
		Version: defaultAgentKitActionVersion,
		Header:  header,
		Body:    body,
	})
	if err != nil {
		return MCPRouterConfig{}, err
	}

	var resp map[string]any
	if err = json.Unmarshal(respBody, &resp); err != nil {
		return MCPRouterConfig{}, fmt.Errorf("unmarshal GetMCPToolset response: %w", err)
	}
	toolset := findMCPToolset(resp)
	if toolset == nil {
		return MCPRouterConfig{}, errors.New("AgentKit MCP toolset was not found in response")
	}
	url, err := mcpToolsetURL(toolset)
	if err != nil {
		return MCPRouterConfig{}, err
	}
	apiKey, err := mcpToolsetAPIKey(toolset)
	if err != nil {
		return MCPRouterConfig{}, err
	}
	return MCPRouterConfig{
		ID:     config.ID,
		URL:    url,
		APIKey: apiKey,
	}, nil
}

func defaultMCPRouterCredentialSource() veauth.CredentialSource {
	prefix := "VOLCENGINE"
	if strings.EqualFold(os.Getenv("CLOUD_PROVIDER"), "byteplus") {
		prefix = "BYTEPLUS"
	}
	global := configs.GetGlobalConfig()
	var ak string
	var sk string
	if global != nil && global.Volcengine != nil {
		ak = global.Volcengine.AK
		sk = global.Volcengine.SK
	}
	return veauth.NewRoleCredentialSource(veauth.RoleCredentialSourceConfig{
		AccessKeyID:       ak,
		SecretAccessKey:   sk,
		EnvironmentPrefix: prefix,
	})
}

func findMCPToolset(resp map[string]any) map[string]any {
	for _, holder := range []map[string]any{resp, mapValue(resp, "Result")} {
		if holder == nil {
			continue
		}
		for _, key := range []string{"MCPToolset", "McpToolset", "mcp_toolset", "mcpToolset"} {
			if value := mapValue(holder, key); value != nil {
				return value
			}
		}
	}
	return nil
}

func mcpToolsetURL(toolset map[string]any) (string, error) {
	path := stringValue(toolset, "Path", "path")
	endpoint := preferredMCPEndpoint(arrayValue(toolset, "NetworkConfigurations", "network_configurations"))
	if endpoint == "" {
		for _, service := range arrayValue(toolset, "MCPServices", "McpServices", "mcp_services", "mcpServices") {
			serviceMap, ok := service.(map[string]any)
			if !ok {
				continue
			}
			endpoint = preferredMCPEndpoint(arrayValue(serviceMap, "NetworkConfigurations", "network_configurations"))
			if path == "" {
				path = stringValue(serviceMap, "Path", "path")
			}
			if endpoint != "" {
				break
			}
		}
	}
	if endpoint == "" {
		endpoint = stringValue(toolset, "URL", "Url", "url", "Endpoint", "endpoint")
	}
	if endpoint == "" {
		return "", errors.New("AgentKit MCP toolset has no network endpoint")
	}
	return joinURLPath(endpoint, path), nil
}

func preferredMCPEndpoint(networks []any) string {
	var fallback string
	for _, item := range networks {
		network, ok := item.(map[string]any)
		if !ok {
			continue
		}
		endpoint := stringValue(network, "Endpoint", "endpoint")
		if endpoint == "" {
			continue
		}
		if fallback == "" {
			fallback = endpoint
		}
		networkType := strings.ToLower(stringValue(network, "NetworkType", "network_type", "networkType"))
		if networkType == "" || networkType == "public" || networkType == "internet" {
			return endpoint
		}
	}
	return fallback
}

func joinURLPath(endpoint string, path string) string {
	endpoint = strings.TrimSpace(endpoint)
	path = strings.TrimSpace(path)
	if path == "" {
		return endpoint
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.HasSuffix(endpoint, path) {
		return endpoint
	}
	return strings.TrimRight(endpoint, "/") + path
}

func mcpToolsetAPIKey(toolset map[string]any) (string, error) {
	authorizerConfig := mapValue(toolset, "AuthorizerConfiguration", "authorizer_configuration", "authorizerConfiguration")
	if authorizerConfig == nil {
		return stringValue(toolset, "APIKey", "ApiKey", "api_key", "apiKey"), nil
	}
	authType := strings.ToLower(strings.ReplaceAll(stringValue(authorizerConfig, "AuthorizerType", "authorizer_type", "authorizerType"), "_", ""))
	if authType != "" && authType != "apikey" && authType != "keyauth" {
		return "", fmt.Errorf("unsupported AgentKit MCP router authorizer type: %s", authType)
	}
	authorizer := mapValue(authorizerConfig, "Authorizer", "authorizer")
	keyAuth := mapValue(authorizer, "KeyAuth", "key_auth", "keyAuth")
	for _, item := range arrayValue(keyAuth, "APIKeys", "ApiKeys", "api_keys", "apiKeys") {
		apiKey, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if key := stringValue(apiKey, "Key", "key"); key != "" {
			return key, nil
		}
	}
	if authType == "apikey" || authType == "keyauth" {
		return "", errors.New("AgentKit MCP router ApiKey authorizer has no api key")
	}
	return "", nil
}

func mapValue(src map[string]any, keys ...string) map[string]any {
	if src == nil {
		return nil
	}
	for _, key := range keys {
		value, ok := src[key]
		if !ok {
			continue
		}
		if typed, ok := value.(map[string]any); ok {
			return typed
		}
	}
	return nil
}

func arrayValue(src map[string]any, keys ...string) []any {
	if src == nil {
		return nil
	}
	for _, key := range keys {
		value, ok := src[key]
		if !ok {
			continue
		}
		if typed, ok := value.([]any); ok {
			return typed
		}
	}
	return nil
}

func stringValue(src map[string]any, keys ...string) string {
	if src == nil {
		return ""
	}
	for _, key := range keys {
		value, ok := src[key]
		if !ok {
			continue
		}
		if typed, ok := value.(string); ok {
			return strings.TrimSpace(typed)
		}
	}
	return ""
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
