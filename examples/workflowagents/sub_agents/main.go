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
	"github.com/volcengine/veadk-go/apps"
	"github.com/volcengine/veadk-go/apps/agentkit_server_app"
	"github.com/volcengine/veadk-go/log"
	vetool "github.com/volcengine/veadk-go/tool"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/tool"
)

func main() {
	ctx := context.Background()

	modelName := "deepseek-v3-2-251201"

	getCityWeatherTool, err := vetool.GetCityWeatherTool()
	if err != nil {
		log.Errorf("GetCityWeatherTool failed: %v", err)
		return
	}

	weatherReporter, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:        "weather_reporter",
			Description: "A weather reporter agent to report the weather.",
			Instruction: "Your responsibility is to obtain the weather conditions of the designated city. You must acquire the dressing advice through other agents.",
			Tools:       []tool.Tool{getCityWeatherTool},
		},
		ModelName: modelName,
	})
	if err != nil {
		log.Errorf("NewLLMAgent weatherReporter failed: %v", err)
		return
	}

	suggester, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:        "suggester",
			Description: "A suggester agent that can give some clothing suggestions according to a city's weather.",
			Instruction: `Provide clothing suggestions based on weather temperature:
			wear a coat when temperature is below 15°C, wear long sleeves when temperature is between 15-25°C,
			wear short sleeves when temperature is above 25°C.`,
		},
		ModelName: modelName,
	})
	if err != nil {
		log.Errorf("NewLLMAgent suggester failed: %v", err)
		return
	}

	rootAgent, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:        "planner",
			Description: "A planner that can generate a suggestion according to a city's weather.",
			Instruction: `Invoke weather reporter agent first to get the weather,
			then invoke suggester agent to get the suggestion. Return the final response to user.`,
			SubAgents: []agent.Agent{weatherReporter, suggester},
		},
		ModelName: modelName,
	})
	if err != nil {
		log.Errorf("NewLLMAgent rootAgent failed: %v", err)
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
