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
)

type CommonModelConfig struct {
	Name     string
	Provider string
	ApiBase  string
	ApiKey   string
}

type AgentConfig struct {
	CommonModelConfig
}

type ModelConfig struct {
	Agent *AgentConfig
}

func (c *ModelConfig) MapEnvToConfig() {
	// Agent
	c.Agent.Name = getEnv(common.MODEL_AGENT_NAME, common.DEFAULT_MODEL_AGENT_NAME, false)
	c.Agent.Provider = getEnv(common.MODEL_AGENT_PROVIDER, common.DEFAULT_MODEL_AGENT_PROVIDER, false)
	c.Agent.ApiBase = getEnv(common.MODEL_AGENT_API_BASE, common.DEFAULT_MODEL_AGENT_API_BASE, false)
	c.Agent.ApiKey = getEnv(common.MODEL_AGENT_API_KEY, "", false)
}
