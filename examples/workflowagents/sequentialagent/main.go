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
	"fmt"

	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/agent/workflowagents/sequentialagent"
	"github.com/volcengine/veadk-go/apps"
	"github.com/volcengine/veadk-go/apps/agentkit_server_app"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
)

func main() {
	ctx := context.Background()

	greetingAgent, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:        "greeting_agent",
			Description: "A friendly agent that greets the user.",
			Instruction: "Greet the user warmly.",
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
		fmt.Printf("NewLLMAgent greetingAgent failed: %v", err)
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
		fmt.Printf("NewLLMAgent goodbyeAgent failed: %v", err)
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
		fmt.Printf("NewSequentialAgent failed: %v", err)
		return
	}

	app := agentkit_server_app.NewAgentkitServerApp(apps.DefaultApiConfig())

	err = app.Run(ctx, &apps.RunConfig{
		AgentLoader: agent.NewSingleLoader(rootAgent),
	})
	if err != nil {
		fmt.Printf("Run failed: %v", err)
	}
}
