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

package main

import (
	"context"

	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/agent/workflowagents/sequentialagent"
	"github.com/volcengine/veadk-go/apps"
	"github.com/volcengine/veadk-go/apps/agentkit_server_app"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/tool/builtin_tools"
	"github.com/volcengine/veadk-go/tool/builtin_tools/web_search"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
)

func main() {
	ctx := context.Background()

	webSearch, err := web_search.NewWebSearchTool(&web_search.Config{})
	if err != nil {
		log.Errorf("NewWebSearchTool failed: %v", err)
		return
	}

	greetingAgent, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:        "greeting_agent",
			Description: "A friendly agent that greets the user.",
			Instruction: "Greet the user warmly.",
			Tools: []tool.Tool{
				webSearch,
			},
		},
		ModelExtraConfig: map[string]any{
			"extra_body": map[string]any{
				"thinking": map[string]string{
					"type": "disabled",
				},
			},
		},
	})
	if err != nil {
		log.Errorf("NewLLMAgent greetingAgent failed: %v", err)
		return
	}

	goodbyeAgent, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:        "goodbye_agent",
			Description: "A polite agent that says goodbye to the user.",
			Instruction: "Directly return goodbye",
		},
		ModelExtraConfig: map[string]any{
			"extra_body": map[string]any{
				"thinking": map[string]string{
					"type": "disabled",
				},
			},
		},
	})
	if err != nil {
		log.Errorf("NewLLMAgent goodbyeAgent failed: %v", err)
		return
	}

	rootAgent, err := sequentialagent.New(sequentialagent.Config{
		AgentConfig: agent.Config{
			Name:        "veAgent",
			SubAgents:   []agent.Agent{greetingAgent, goodbyeAgent},
			Description: "Executes a sequence of greeting and goodbye.",
		},
	})

	if err != nil {
		log.Errorf("NewSequentialAgent failed: %v", err)
		return
	}

	app := agentkit_server_app.NewAgentkitServerApp(apps.DefaultApiConfig())

	err = app.Run(ctx, &apps.RunConfig{
		AgentLoader:    agent.NewSingleLoader(rootAgent),
		SessionService: session.InMemoryService(),
		PluginConfig: runner.PluginConfig{
			Plugins: []*plugin.Plugin{
				//NewTestPlugins(),
				utils.Must(builtin_tools.NewLLMShieldPlugins()),
			},
		},
	})
	if err != nil {
		log.Errorf("Run failed: %v", err)
	}
}

func beforeModelCallBack(ctx agent.CallbackContext, llmRequest *model.LLMRequest) (*model.LLMResponse, error) {
	log.Infof("%s BeforeModelCallBack called\n", ctx.AgentName())
	return nil, nil
}

func afterModelCallBack(ctx agent.CallbackContext, llmResponse *model.LLMResponse, llmResponseError error) (*model.LLMResponse, error) {
	log.Infof("%s afterModelCallback called\n", ctx.AgentName())
	return nil, nil
}

func beforeToolCallback(ctx tool.Context, tool tool.Tool, args map[string]any) (map[string]any, error) {
	log.Infof("%s beforeToolCallBack called\n", tool.Name())
	return nil, nil
}

func afterToolCallback(ctx tool.Context, tool tool.Tool, args, result map[string]any, err error) (map[string]any, error) {
	log.Infof("%s afterToolCallback called\n", tool.Name())
	return nil, nil
}

func NewTestPlugins() *plugin.Plugin {
	plugins, _ := plugin.New(plugin.Config{
		Name:                "llm_shield_test",
		BeforeModelCallback: beforeModelCallBack,
		AfterModelCallback:  afterModelCallBack,
		BeforeToolCallback:  beforeToolCallback,
		AfterToolCallback:   afterToolCallback,
	})
	return plugins
}
