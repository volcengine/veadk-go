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
	"log"

	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/apps"
	"github.com/volcengine/veadk-go/apps/agentkit_server_app"
	vem "github.com/volcengine/veadk-go/memory"
	"github.com/volcengine/veadk-go/tool/builtin_tools"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()

	sessionServer := session.InMemoryService()
	//memoryServer := memory.InMemoryService()

	memoryServer, err := vem.NewLongTermMemoryService(vem.BackendLongTermMem0, nil)
	if err != nil {
		log.Printf("NewLongTermMemoryService failed: %v", err)
		return
	}

	onBeforeAgent := func(ctx agent.CallbackContext) (*genai.Content, error) {
		resp, err := sessionServer.Get(ctx, &session.GetRequest{AppName: ctx.AppName(), UserID: ctx.UserID(), SessionID: ctx.SessionID()})
		if err != nil {
			log.Fatalf("Failed to get completed session: %v", err)
		}
		if err := memoryServer.AddSession(ctx, resp.Session); err != nil {
			log.Fatalf("Failed to add session to memory: %v", err)
		}
		log.Println("")

		log.Printf("[Callback] Session %s added to memory.", ctx.SessionID())
		return nil, nil
	}

	a, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:                 "personal_assistant",
			Instruction:          "You are a personal assistant with long-term memory capabilities. Before answering the user's questions, you must invoke the tool to retrieve memory information.",
			Tools:                []tool.Tool{utils.Must(builtin_tools.LoadLongMemoryTool())},
			BeforeAgentCallbacks: []agent.BeforeAgentCallback{onBeforeAgent},
		},
	})
	if err != nil {
		fmt.Printf("NewLLMAgent failed: %v", err)
		return
	}

	app := agentkit_server_app.NewAgentkitServerApp(apps.DefaultApiConfig())

	err = app.Run(ctx, &apps.RunConfig{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: sessionServer,
		MemoryService:  memoryServer,
	})
	if err != nil {
		fmt.Printf("Run failed: %v", err)
	}
}
