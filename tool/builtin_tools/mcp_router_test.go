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
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volcengine/veadk-go/v2/auth/veauth"
	"github.com/volcengine/veadk-go/v2/common"
	"github.com/volcengine/veadk-go/v2/integrations/ve_sign"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
)

type stubCredentialSource struct {
	credential veauth.VeIAMCredential
	err        error
}

func (s stubCredentialSource) Credential(context.Context) (veauth.VeIAMCredential, error) {
	return s.credential, s.err
}

func TestNewMcpRouterToolsetUsesExplicitURLAndAPIKey(t *testing.T) {
	clearMCPRouterEnv(t)
	captured := captureMCPRouterToolsetNew(t)

	toolset, err := NewMcpRouterToolset(
		WithMCPRouterURL("https://router.example.com/mcp"),
		WithMCPRouterAPIKey("router-token"),
	)

	require.NoError(t, err)
	assert.Nil(t, toolset)
	transport := requireStreamableMCPTransport(t, captured.Config)
	assert.Equal(t, "https://router.example.com/mcp", transport.Endpoint)
	assert.NotNil(t, transport.HTTPClient)
}

func TestNewMcpRouterToolsetAllowsMissingAPIKey(t *testing.T) {
	clearMCPRouterEnv(t)
	captured := captureMCPRouterToolsetNew(t)

	toolset, err := NewMcpRouterToolset(WithMCPRouterURL("https://router.example.com/mcp"))

	require.NoError(t, err)
	assert.Nil(t, toolset)
	transport := requireStreamableMCPTransport(t, captured.Config)
	assert.Equal(t, "https://router.example.com/mcp", transport.Endpoint)
	assert.Nil(t, transport.HTTPClient)
}

func TestNewMcpRouterToolsetRequiresURLOrID(t *testing.T) {
	clearMCPRouterEnv(t)
	toolset, err := NewMcpRouterToolset()

	require.Error(t, err)
	assert.ErrorIs(t, err, errMCPRouterURLRequired)
	assert.Nil(t, toolset)
}

func TestNewMcpRouterToolsetResolvesAgentKitMCPToolsetID(t *testing.T) {
	clearMCPRouterEnv(t)
	captured := captureMCPRouterToolsetNew(t)
	var request ve_sign.VeRequest
	mcpRouterDoRequest = func(req ve_sign.VeRequest) ([]byte, error) {
		request = req
		return []byte(`{
			"Result": {
				"MCPToolset": {
					"MCPToolsetId": "mt-1",
					"Name": "router",
					"Path": "/mcp",
					"NetworkConfigurations": [
						{"NetworkType": "Private", "Endpoint": "https://private.example.com"},
						{"NetworkType": "Public", "Endpoint": "https://router.example.com"}
					],
					"AuthorizerConfiguration": {
						"AuthorizerType": "ApiKey",
						"Authorizer": {
							"KeyAuth": {
								"APIKeys": [
									{"Key": "router-token", "Name": "default"}
								]
							}
						}
					}
				}
			}
		}`), nil
	}
	t.Cleanup(func() {
		mcpRouterDoRequest = func(req ve_sign.VeRequest) ([]byte, error) {
			return req.DoRequest()
		}
	})

	toolset, err := NewMcpRouterToolset(
		WithMCPRouterID("mt-1"),
		WithMCPRouterCredentialSource(stubCredentialSource{
			credential: veauth.VeIAMCredential{
				AccessKeyID:     "ak",
				SecretAccessKey: "sk",
				SessionToken:    "token",
			},
		}),
	)

	require.NoError(t, err)
	assert.Nil(t, toolset)
	assert.Equal(t, "GetMCPToolset", request.Action)
	assert.Equal(t, defaultAgentKitActionVersion, request.Version)
	assert.Equal(t, defaultAgentKitMCPHost, request.Host)
	assert.Equal(t, "token", request.Header["X-Security-Token"])
	assert.Equal(t, map[string]any{"MCPToolsetId": "mt-1"}, request.Body)
	transport := requireStreamableMCPTransport(t, captured.Config)
	assert.Equal(t, "https://router.example.com/mcp", transport.Endpoint)
	assert.NotNil(t, transport.HTTPClient)
}

func TestNewMcpRouterToolsetExplicitConfigOverridesResolvedID(t *testing.T) {
	clearMCPRouterEnv(t)
	captured := captureMCPRouterToolsetNew(t)
	mcpRouterDoRequest = func(ve_sign.VeRequest) ([]byte, error) {
		return []byte(`{
			"Result": {
				"mcp_toolset": {
					"path": "/mcp",
					"network_configurations": [
						{"network_type": "public", "endpoint": "https://resolved.example.com"}
					],
					"authorizer_configuration": {
						"authorizer_type": "ApiKey",
						"authorizer": {
							"key_auth": {"api_keys": [{"key": "resolved-token"}]}
						}
					}
				}
			}
		}`), nil
	}
	t.Cleanup(func() {
		mcpRouterDoRequest = func(req ve_sign.VeRequest) ([]byte, error) {
			return req.DoRequest()
		}
	})

	_, err := NewMcpRouterToolset(
		WithMCPRouterID("mt-1"),
		WithMCPRouterURL("https://override.example.com/custom"),
		WithMCPRouterAPIKey("override-token"),
		WithMCPRouterCredentialSource(stubCredentialSource{
			credential: veauth.VeIAMCredential{AccessKeyID: "ak", SecretAccessKey: "sk"},
		}),
	)

	require.NoError(t, err)
	transport := requireStreamableMCPTransport(t, captured.Config)
	assert.Equal(t, "https://override.example.com/custom", transport.Endpoint)
	assert.NotNil(t, transport.HTTPClient)
}

type capturedMCPRouterConfig struct {
	Config mcptoolset.Config
}

func captureMCPRouterToolsetNew(t *testing.T) *capturedMCPRouterConfig {
	t.Helper()

	captured := &capturedMCPRouterConfig{}
	original := mcpRouterToolsetNew
	mcpRouterToolsetNew = func(cfg mcptoolset.Config) (tool.Toolset, error) {
		captured.Config = cfg
		return nil, nil
	}
	t.Cleanup(func() {
		mcpRouterToolsetNew = original
	})
	return captured
}

func requireStreamableMCPTransport(t *testing.T, cfg mcptoolset.Config) *mcp.StreamableClientTransport {
	t.Helper()

	transport, ok := cfg.Transport.(*mcp.StreamableClientTransport)
	require.True(t, ok)
	return transport
}

func clearMCPRouterEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		common.TOOL_MCP_ROUTER_URL,
		common.TOOL_MCP_ROUTER_API_KEY,
		common.MCP_ROUTER_ID,
		common.MCP_TOOLSET_ID,
		"TOOLS_MCP_ROUTER_URL",
		"TOOLS_MCP_ROUTER_API_KEY",
		"TOOLS_MCP_ROUTER_ID",
		"TOOLS_MCP_ROUTER_MCP_TOOLSET_ID",
	} {
		t.Setenv(key, "")
	}
}
