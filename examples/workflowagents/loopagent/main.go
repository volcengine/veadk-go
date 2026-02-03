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
	"github.com/volcengine/veadk-go/agent/workflowagents/loopagent"
	"github.com/volcengine/veadk-go/apps"
	"github.com/volcengine/veadk-go/apps/agentkit_server_app"
	"github.com/volcengine/veadk-go/log"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

func main() {
	ctx := context.Background()

	exitLoopTool, err := GetExitLoopTool()
	if err != nil {
		log.Errorf("GetExitLoopTool failed: %v", err)
		return
	}

	plannerAgent, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:        "planner_agent",
			Description: "Decomposes a complex task into smaller actionable steps.",
			Instruction: "Given the user's goal and current progress, decide the NEXT step to take. You don't need to execute the step, just describe it clearly. If all steps are done, respond with 'TASK COMPLETE'.",
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
		log.Errorf("NewLLMAgent plannerAgent failed: %v", err)
		return
	}

	executorAgent, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:        "executor_agent",
			Description: "Executes a given step and returns the result.",
			Instruction: "Execute the provided step and describe what was done or what result was obtained. If you received 'TASK COMPLETE', you must call the 'exit_loop' function. Do not output any text.",
			Tools:       []tool.Tool{exitLoopTool},
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
		log.Errorf("NewLLMAgent executorAgent failed: %v", err)
		return
	}

	rootAgent, err := loopagent.New(loopagent.Config{
		AgentConfig: agent.Config{
			SubAgents:   []agent.Agent{plannerAgent, executorAgent},
			Description: "Executes a sequence of code writing, reviewing, and refactoring.",
		},
		MaxIterations: 3,
	})

	if err != nil {
		log.Errorf("NewSequentialAgent failed: %v", err)
		return
	}

	app := agentkit_server_app.NewAgentkitServerApp(apps.DefaultApiConfig())

	err = app.Run(ctx, &apps.RunConfig{
		AgentLoader: agent.NewSingleLoader(rootAgent),
	})
	if err != nil {
		log.Errorf("Run failed: %v", err)
	}
}

type ExitLoopToolArgs struct {
	Name string `json:"name" jsonschema:"name of the agent invoke exit loop tool"`
}

func GetExitLoopTool() (tool.Tool, error) {
	handler := func(ctx tool.Context, args ExitLoopToolArgs) (map[string]any, error) {
		ctx.Actions().Escalate = true
		return map[string]any{}, nil
	}
	return functiontool.New(
		functiontool.Config{
			Name:        "exit_loop",
			Description: `A tools to exit the loop`,
		},
		handler)
}
