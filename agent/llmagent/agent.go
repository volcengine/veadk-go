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
	"fmt"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/knowledgebase"
	"github.com/volcengine/veadk-go/model"
	"github.com/volcengine/veadk-go/prompts"
	adkmodel "google.golang.org/adk/model"
	"github.com/volcengine/veadk-go/tool/builtin_tools"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/tool"
)

type Config struct {
	llmagent.Config
	ModelName        string
	ModelProvider    string
	ModelAPIBase     string
	ModelAPIKey      string
	ModelExtraConfig map[string]any
	KnowledgeBase    *knowledgebase.KnowledgeBase
	PromptManager    prompts.BasePromptManager
	DisableThought   bool
}

func New(cfg *Config) (agent.Agent, error) {
	if cfg.Name == "" {
		cfg.Name = common.DEFAULT_LLMAGENT_NAME
	}

	if cfg.Instruction == "" {
		if cfg.PromptManager != nil {
			cfg.Instruction = cfg.PromptManager.GetPrompt()
		} else {
			cfg.Instruction = prompts.DEFAULT_INSTRUCTION
		}
	}

	if cfg.Description == "" {
		cfg.Description = prompts.DEFAULT_DESCRIPTION
	}

	if cfg.DisableThought {
		newModelExtraConfig, err := addDisableThoughtConfig(cfg.ModelExtraConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to set DisableThought config: %w", err)
		}
		cfg.ModelExtraConfig = newModelExtraConfig
	}

	if cfg.Model == nil {
		if cfg.ModelName == "" {
			cfg.ModelName = utils.GetEnvWithDefault(common.MODEL_AGENT_NAME, configs.GetGlobalConfig().Model.Agent.Name, common.DEFAULT_MODEL_AGENT_NAME)
		}
		if cfg.ModelProvider == "" {
			cfg.ModelProvider = utils.GetEnvWithDefault(common.MODEL_AGENT_PROVIDER, configs.GetGlobalConfig().Model.Agent.Provider, common.DEFAULT_MODEL_AGENT_PROVIDER)
		}
		if cfg.ModelAPIKey == "" {
			cfg.ModelAPIKey = utils.GetEnvWithDefault(common.MODEL_AGENT_API_KEY, configs.GetGlobalConfig().Model.Agent.ApiKey, utils.Must(veauth.GetArkToken(common.DEFAULT_MODEL_REGION)))
		}
		if cfg.ModelAPIBase == "" {
			cfg.ModelAPIBase = utils.GetEnvWithDefault(common.MODEL_AGENT_API_BASE, configs.GetGlobalConfig().Model.Agent.ApiBase, common.DEFAULT_MODEL_AGENT_API_BASE)
		}

		var veModel adkmodel.LLM
		var err error

		switch cfg.ModelProvider {
		case "ark":
			veModel, err = model.NewArkModel(
				context.Background(),
				cfg.ModelName,
				&model.ArkClientConfig{
					APIKey:    cfg.ModelAPIKey,
					BaseURL:   cfg.ModelAPIBase,
					ExtraBody: cfg.ModelExtraConfig,
				})
		default: // "openai"
			veModel, err = model.NewOpenAIModel(
				context.Background(),
				cfg.ModelName,
				&model.ClientConfig{
					APIKey:    cfg.ModelAPIKey,
					BaseURL:   cfg.ModelAPIBase,
					ExtraBody: cfg.ModelExtraConfig,
				})
		}
		if err != nil {
			return nil, err
		}
		cfg.Model = veModel
	}

	if cfg.KnowledgeBase != nil {
		knowledgeTool, err := builtin_tools.LoadKnowledgeBaseTool(cfg.KnowledgeBase)
		if err != nil {
			return nil, err
		}
		if cfg.Tools == nil {
			cfg.Tools = []tool.Tool{}
		}
		cfg.Tools = append(cfg.Tools, knowledgeTool)
	}

	return llmagent.New(cfg.Config)
}

func addDisableThoughtConfig(extConfig map[string]any) (map[string]any, error) {
	if extConfig == nil {
		extConfig = map[string]any{
			"extra_body": map[string]any{
				"thinking": map[string]string{
					"type": "disabled",
				},
			},
		}
		return extConfig, nil
	}

	extraBodyVal, exists := extConfig["extra_body"]
	var extraBody map[string]any
	if !exists {
		extConfig["extra_body"] = map[string]any{
			"thinking": map[string]string{
				"type": "disabled",
			},
		}
		return extConfig, nil
	} else {
		var ok bool
		extraBody, ok = extraBodyVal.(map[string]any)
		if !ok {
			return extConfig, fmt.Errorf("type conflict for field 'extra_body' in ModelExtraConfig: expected type map[string]any, but got %T", extraBodyVal)
		}
	}

	thinkingVal, exists := extraBody["thinking"]
	var thinking map[string]string
	if !exists {
		extraBody["thinking"] = map[string]string{
			"type": "disabled",
		}
	} else {
		var ok bool
		thinking, ok = thinkingVal.(map[string]string)
		if !ok {
			return extConfig, fmt.Errorf("type conflict for field 'thinking' in ModelExtraConfig: expected type map[string]any, but got %T", thinkingVal)
		}
	}

	thinking["type"] = "disabled"
	return extConfig, nil
}
