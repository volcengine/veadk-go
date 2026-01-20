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

type PromptPilotConfig struct {
	Url         string `yaml:"url"`
	ApiKey      string `yaml:"api_key"`
	WorkspaceId string `yaml:"workspace_id"`
}

func (v *PromptPilotConfig) MapEnvToConfig() {
	v.Url = utils.GetEnvWithDefault(common.AGENTPILOT_API_URL)
	v.ApiKey = utils.GetEnvWithDefault(common.AGENTPILOT_API_KEY)
	v.WorkspaceId = utils.GetEnvWithDefault(common.AGENTPILOT_WORKSPACE_ID)
}
