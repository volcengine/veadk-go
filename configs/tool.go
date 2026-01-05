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

package configs

import (
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/utils"
)

type BuiltinToolConfigs struct {
	MCPRouter *MCPRouter `yaml:"mcp_router"`
	RunCode   *RunCode   `yaml:"run_code"`
}

func (b *BuiltinToolConfigs) MapEnvToConfig() {
	b.MCPRouter.MapEnvToConfig()
	b.RunCode.MapEnvToConfig()
}

type MCPRouter struct {
	Url    string `yaml:"url"`
	ApiKey string `yaml:"api_key"`
}

func (m *MCPRouter) MapEnvToConfig() {
	m.Url = utils.GetEnvWithDefault(common.TOOL_MCP_ROUTER_URL)
	m.ApiKey = utils.GetEnvWithDefault(common.TOOL_MCP_ROUTER_API_KEY)
}

type RunCode struct {
	ToolID      string `yaml:"tool_id"`
	Host        string `yaml:"host"`
	ServiceCode string `yaml:"service_code"`
	Region      string `yaml:"region"`
}

func (r *RunCode) MapEnvToConfig() {
	r.ToolID = utils.GetEnvWithDefault(common.AGENTKIT_TOOL_ID)
	r.Host = utils.GetEnvWithDefault(common.AGENTKIT_TOOL_HOST)
	r.ServiceCode = utils.GetEnvWithDefault(common.AGENTKIT_TOOL_SERVICE_CODE)
	r.Region = utils.GetEnvWithDefault(common.AGENTKIT_TOOL_REGION)
}
