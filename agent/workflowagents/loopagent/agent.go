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

package loopagent

import (
	"github.com/volcengine/veadk-go/v2/common"
	"github.com/volcengine/veadk-go/v2/prompts"
	"google.golang.org/adk/v2/agent"
	googleADKLoopAgent "google.golang.org/adk/v2/agent/workflowagents/loopagent"
)

// Config defines the configuration for a veLoopAgent.
type Config struct {
	// Basic agent setup.
	AgentConfig agent.Config

	// If MaxIterations == 0, then LoopAgent runs indefinitely or until any
	// sub-agent escalates.
	MaxIterations uint

	//TODO: add tracers
}

// New creates a LoopAgent.
//
// LoopAgent repeatedly runs its sub-agents in sequence for a specified number
// of iterations or until a termination condition is met.
//
// Use the LoopAgent when your workflow involves repetition or iterative
// refinement, such as like revising code.
func New(cfg Config) (agent.Agent, error) {

	if cfg.AgentConfig.Name == "" {
		cfg.AgentConfig.Name = common.DEFAULT_LOOPAGENT_NAME
	}
	if cfg.AgentConfig.Description == "" {
		cfg.AgentConfig.Description = prompts.DEFAULT_DESCRIPTION
	}

	return googleADKLoopAgent.New(googleADKLoopAgent.Config{
		AgentConfig:   cfg.AgentConfig,
		MaxIterations: cfg.MaxIterations,
	})
}
