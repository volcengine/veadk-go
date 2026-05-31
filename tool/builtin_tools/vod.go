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
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

func NewVodToolset() (tool.Toolset, error) {
	ak := utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
	sk := utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	if ak == "" || sk == "" {
		return nil, fmt.Errorf("vod MCP toolset requires %s and %s to be configured via environment variables or config file", common.VOLCENGINE_ACCESS_KEY, common.VOLCENGINE_SECRET_KEY)
	}

	cmd := exec.Command("uvx", "--from",
		"git+https://github.com/volcengine/mcp-server#subdirectory=server/mcp_server_vod",
		"mcp-server-vod")
	cmd.Env = append(os.Environ(), common.VOLCENGINE_ACCESS_KEY+"="+ak, common.VOLCENGINE_SECRET_KEY+"="+sk)
	if groups := utils.GetEnvWithDefault(common.TOOL_VOD_GROUPS); groups != "" {
		cmd.Env = append(cmd.Env, "MCP_TOOL_GROUPS="+groups)
	}

	return mcptoolset.New(mcptoolset.Config{
		Transport: &mcp.CommandTransport{Command: cmd},
	})
}
