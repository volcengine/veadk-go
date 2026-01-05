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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/utils"
	"golang.org/x/oauth2"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

func agentkitMCPTransport(ctx context.Context) mcp.Transport {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: utils.GetEnvWithDefault(common.TOOL_MCP_ROUTER_API_KEY, configs.GetGlobalConfig().Tool.MCPRouter.ApiKey)},
	)
	return &mcp.StreamableClientTransport{
		Endpoint:   utils.GetEnvWithDefault(common.TOOL_MCP_ROUTER_URL, configs.GetGlobalConfig().Tool.MCPRouter.Url),
		HTTPClient: oauth2.NewClient(ctx, ts),
	}
}

func NewMcpRouter() tool.Toolset {
	ctx := context.Background()
	mcpRouter, _ := mcptoolset.New(mcptoolset.Config{
		Transport: agentkitMCPTransport(ctx),
	})
	return mcpRouter
}
