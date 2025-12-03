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

package llmagent

import (
	"context"

	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/model"
	"github.com/volcengine/veadk-go/prompts"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
)

type InnerConfig struct {
	Name string
}

// Config of veLLMAgent
type Config struct {
	llmagent.Config
	ModelName     string
	ModelProvider string
	ModelApiBase  string
	ModelApiKey   string
}

func New(cfg Config) (agent.Agent, error) {
	if cfg.ModelName != "" {
		model, err := model.NewModel(
			context.Background(),
			cfg.ModelName,
			&model.ClientConfig{
				APIKey:  cfg.ModelApiKey,
				BaseURL: cfg.ModelApiBase,
			})
		if err != nil {
			return nil, err
		}
		cfg.Model = model
	}
	if cfg.Name == "" {
		cfg.Name = common.DEFAULT_LLMAGENT_NAME
	}
	if cfg.Instruction == "" {
		cfg.Instruction = prompts.DEFAULT_INSTRUCTION
	}
	if cfg.Description == "" {
		cfg.Description = prompts.DEFAULT_DESCRIPTION
	}
	return llmagent.New(cfg.Config)
}
