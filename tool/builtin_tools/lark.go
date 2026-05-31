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
	"fmt"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

func NewLarkToolset() (tool.Toolset, error) {
	endpoint := utils.GetEnvWithDefault(common.TOOL_LARK_ENDPOINT)
	apiKey := utils.GetEnvWithDefault(common.TOOL_LARK_API_KEY)
	token := utils.GetEnvWithDefault(common.TOOL_LARK_TOKEN)
	if endpoint == "" || apiKey == "" || token == "" {
		return nil, fmt.Errorf("lark MCP toolset requires %s, %s, and %s to be configured", common.TOOL_LARK_ENDPOINT, common.TOOL_LARK_API_KEY, common.TOOL_LARK_TOKEN)
	}

	cmd := exec.Command("npx", "-y", "@larksuiteoapi/lark-mcp", "mcp", "-a", endpoint, "-s", apiKey, "-u", token)
	return mcptoolset.New(mcptoolset.Config{
		Transport: &mcp.CommandTransport{Command: cmd},
	})
}
