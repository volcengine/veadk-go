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
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/utils"
	"golang.org/x/oauth2"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

// NewLasToolset creates a LAS MCP toolset from TOOL_LAS_URL and TOOL_LAS_API_KEY.
func NewLasToolset() (tool.Toolset, error) {
	url := strings.TrimSpace(utils.GetEnvWithDefault(common.TOOL_LAS_URL))
	apiKey := strings.TrimSpace(utils.GetEnvWithDefault(common.TOOL_LAS_API_KEY))

	if url == "" {
		return nil, fmt.Errorf("%s is required", common.TOOL_LAS_URL)
	}
	if strings.Contains(url, "/mcp") {
		return mcptoolset.New(mcptoolset.Config{
			Transport: newLasMCPTransport(context.Background(), url, apiKey),
		})
	}
	if strings.Contains(url, "/sse") {
		return nil, fmt.Errorf("LAS SSE MCP endpoint 暂不支持, please configure MCP connection params manually")
	}

	return nil, fmt.Errorf("unsupported LAS MCP url: no `/mcp` or `/sse` field in url; please configure MCP connection params manually")
}

func newLasMCPTransport(ctx context.Context, url, apiKey string) mcp.Transport {
	httpClient := http.DefaultClient
	if apiKey != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: apiKey})
		httpClient = oauth2.NewClient(ctx, ts)
	}

	return &mcp.StreamableClientTransport{
		Endpoint:   url,
		HTTPClient: httpClient,
	}
}
