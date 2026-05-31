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
	"testing"

	"github.com/bytedance/mockey"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

func TestNewPlaywrightToolset(t *testing.T) {
	mockey.PatchConvey("builds playwright stdio command", t, func() {
		captured, calls := mockMCPToolsetNew()

		ts, err := NewPlaywrightToolset()

		assert.NoError(t, err)
		assert.Nil(t, ts)
		assert.Equal(t, 1, *calls)
		transport := requireCommandTransport(t, *captured)
		assert.Contains(t, transport.Command.Args, "npx")
		assert.Contains(t, transport.Command.Args, "@playwright/mcp@latest")
	})
}

func TestNewLarkToolset(t *testing.T) {
	mockey.PatchConvey("builds lark stdio command", t, func() {
		t.Setenv(common.TOOL_LARK_ENDPOINT, "https://open.larksuite.com")
		t.Setenv(common.TOOL_LARK_API_KEY, "test-api-key")
		t.Setenv(common.TOOL_LARK_TOKEN, "test-token")
		captured, calls := mockMCPToolsetNew()

		ts, err := NewLarkToolset()

		assert.NoError(t, err)
		assert.Nil(t, ts)
		assert.Equal(t, 1, *calls)
		transport := requireCommandTransport(t, *captured)
		assert.Contains(t, transport.Command.Args, "npx")
		assert.Contains(t, transport.Command.Args, "@larksuiteoapi/lark-mcp")
		assert.Contains(t, transport.Command.Args, "-a")
		assert.Contains(t, transport.Command.Args, "https://open.larksuite.com")
		assert.Contains(t, transport.Command.Args, "-s")
		assert.Contains(t, transport.Command.Args, "test-api-key")
		assert.Contains(t, transport.Command.Args, "-u")
		assert.Contains(t, transport.Command.Args, "test-token")
	})
}

func TestNewLarkToolsetMissingEnv(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		apiKey   string
		token    string
	}{
		{name: "missing endpoint", apiKey: "test-api-key", token: "test-token"},
		{name: "missing api key", endpoint: "https://open.larksuite.com", token: "test-token"},
		{name: "missing token", endpoint: "https://open.larksuite.com", apiKey: "test-api-key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockey.PatchConvey(tt.name, t, func() {
				t.Setenv(common.TOOL_LARK_ENDPOINT, tt.endpoint)
				t.Setenv(common.TOOL_LARK_API_KEY, tt.apiKey)
				t.Setenv(common.TOOL_LARK_TOKEN, tt.token)
				_, calls := mockMCPToolsetNew()

				ts, err := NewLarkToolset()

				assert.Error(t, err)
				assert.Contains(t, err.Error(), common.TOOL_LARK_ENDPOINT)
				assert.Contains(t, err.Error(), common.TOOL_LARK_API_KEY)
				assert.Contains(t, err.Error(), common.TOOL_LARK_TOKEN)
				assert.Nil(t, ts)
				assert.Equal(t, 0, *calls)
			})
		})
	}
}

func TestNewVodToolset(t *testing.T) {
	mockey.PatchConvey("builds vod stdio command with env", t, func() {
		t.Setenv(common.VOLCENGINE_ACCESS_KEY, "test-ak")
		t.Setenv(common.VOLCENGINE_SECRET_KEY, "test-sk")
		t.Setenv(common.TOOL_VOD_GROUPS, "base,upload")
		mockey.Mock(configs.GetGlobalConfig).Return(emptyGlobalConfig()).Build()
		captured, calls := mockMCPToolsetNew()

		ts, err := NewVodToolset()

		assert.NoError(t, err)
		assert.Nil(t, ts)
		assert.Equal(t, 1, *calls)
		transport := requireCommandTransport(t, *captured)
		assert.Contains(t, transport.Command.Args, "uvx")
		assert.Contains(t, transport.Command.Args, "mcp-server-vod")
		assert.Contains(t, transport.Command.Env, common.VOLCENGINE_ACCESS_KEY+"=test-ak")
		assert.Contains(t, transport.Command.Env, common.VOLCENGINE_SECRET_KEY+"=test-sk")
		assert.Contains(t, transport.Command.Env, "MCP_TOOL_GROUPS=base,upload")
	})
}

func TestNewVodToolsetMissingCredential(t *testing.T) {
	tests := []struct {
		name string
		ak   string
		sk   string
	}{
		{name: "missing ak", sk: "test-sk"},
		{name: "missing sk", ak: "test-ak"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockey.PatchConvey(tt.name, t, func() {
				t.Setenv(common.VOLCENGINE_ACCESS_KEY, tt.ak)
				t.Setenv(common.VOLCENGINE_SECRET_KEY, tt.sk)
				t.Setenv(common.TOOL_VOD_GROUPS, "")
				mockey.Mock(configs.GetGlobalConfig).Return(emptyGlobalConfig()).Build()
				_, calls := mockMCPToolsetNew()

				ts, err := NewVodToolset()

				assert.Error(t, err)
				assert.Contains(t, err.Error(), common.VOLCENGINE_ACCESS_KEY)
				assert.Contains(t, err.Error(), common.VOLCENGINE_SECRET_KEY)
				assert.Nil(t, ts)
				assert.Equal(t, 0, *calls)
			})
		})
	}
}

func mockMCPToolsetNew() (*mcptoolset.Config, *int) {
	var captured mcptoolset.Config
	calls := 0
	mockey.Mock(mcptoolset.New).To(func(cfg mcptoolset.Config) (tool.Toolset, error) {
		captured = cfg
		calls++
		return nil, nil
	}).Build()
	return &captured, &calls
}

func requireCommandTransport(t *testing.T, cfg mcptoolset.Config) *mcp.CommandTransport {
	t.Helper()

	transport, ok := cfg.Transport.(*mcp.CommandTransport)
	require.True(t, ok)
	require.NotNil(t, transport.Command)
	return transport
}

func emptyGlobalConfig() *configs.VeADKConfig {
	return &configs.VeADKConfig{
		Volcengine: &configs.Volcengine{},
	}
}
