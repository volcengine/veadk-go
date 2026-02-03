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
	"encoding/json"

	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/agent/workflowagents/parallelagent"
	"github.com/volcengine/veadk-go/apps"
	"github.com/volcengine/veadk-go/apps/agentkit_server_app"
	"github.com/volcengine/veadk-go/log"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
)

//func beforeAgentCallback() func(agent.CallbackContext) (*genai.Content, error) {
//	return func(ctx agent.CallbackContext) (*genai.Content, error) {
//		userCtx := ctx.UserContent()
//		cstr, _ := json.Marshal(userCtx)
//		fmt.Printf("%s Before Agent callback called: %s \n", ctx.AgentName(), string(cstr))
//		return nil, nil
//	}
//}

func onBeforeModel(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
	reqStr, _ := json.Marshal(req)
	log.Infof("%s [Callback] BeforeModel req: %s", ctx.AgentName(), string(reqStr))
	return nil, nil
}

func main() {
	ctx := context.Background()

	prosAgent, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:                 "pros_agent",
			Instruction:          "List and explain the positive aspects or advantages of the given topic.",
			Description:          "An expert that identifies the advantages of a topic.",
			BeforeModelCallbacks: []llmagent.BeforeModelCallback{onBeforeModel},
		},
	})
	if err != nil {
		log.Errorf("NewLLMAgent prosAgent failed: %v", err)
		return
	}

	consAgent, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:                 "cons_agent",
			Instruction:          "List and explain the negative aspects or disadvantages of the given topic.",
			Description:          "An expert that identifies the disadvantages of a topic.",
			BeforeModelCallbacks: []llmagent.BeforeModelCallback{onBeforeModel},
		},
	})
	if err != nil {
		log.Errorf("NewLLMAgent consAgent failed: %v", err)
		return
	}

	rootAgent, err := parallelagent.New(parallelagent.Config{
		AgentConfig: agent.Config{
			Name:      "veAgent",
			SubAgents: []agent.Agent{prosAgent, consAgent},
		},
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
