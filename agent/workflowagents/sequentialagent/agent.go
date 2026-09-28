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

package sequentialagent

import (
	"github.com/volcengine/veadk-go/v2/common"
	"github.com/volcengine/veadk-go/v2/prompts"
	"google.golang.org/adk/v2/agent"
	googleADKSequentialAgent "google.golang.org/adk/v2/agent/workflowagents/sequentialagent"
)

// Config defines the configuration for a SequentialAgent.
type Config struct {
	// Basic agent setup.
	AgentConfig agent.Config

	//TODO: add tracers
}

// New creates a SequentialAgent.
//
// SequentialAgent executes its sub-agents once, in the order they are listed.
//
// Use the SequentialAgent when you want the execution to occur in a fixed,
// strict order.
func New(cfg Config) (agent.Agent, error) {

	if cfg.AgentConfig.Name == "" {
		cfg.AgentConfig.Name = common.DEFAULT_SEQUENTIALAGENT_NAME
	}
	if cfg.AgentConfig.Description == "" {
		cfg.AgentConfig.Description = prompts.DEFAULT_DESCRIPTION
	}

	return googleADKSequentialAgent.New(googleADKSequentialAgent.Config{
		AgentConfig: cfg.AgentConfig,
	})
}
