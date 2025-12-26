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

package parallelagent

import (
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/prompts"
	"google.golang.org/adk/agent"
	googleADKParallelAgent "google.golang.org/adk/agent/workflowagents/parallelagent"
)

// Config defines the configuration for a ParallelAgent.
type Config struct {
	// Basic agent setup.
	AgentConfig agent.Config
	//TODO: add tracers
}

func New(cfg Config) (agent.Agent, error) {

	if cfg.AgentConfig.Name == "" {
		cfg.AgentConfig.Name = common.DEFAULT_PARALLELAGENT_NAME
	}
	if cfg.AgentConfig.Description == "" {
		cfg.AgentConfig.Description = prompts.DEFAULT_DESCRIPTION
	}

	return googleADKParallelAgent.New(googleADKParallelAgent.Config{
		AgentConfig: cfg.AgentConfig,
	})
}
