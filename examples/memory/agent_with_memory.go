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
	"strings"

	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/tool/builtin_tools"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()
	appName := "ve_agent"
	userID := "user4567"

	sessionServer := session.InMemoryService()
	memoryServer := memory.InMemoryService()
	//memoryServer, err := vem.NewLongTermMemoryService(vem.BackendLongTermViking, nil)
	//if err != nil {
	//	log.Errorf("NewLongTermMemoryService failed: %v", err)
	//	return
	//}

	onBeforeAgent := func(ctx agent.CallbackContext) (*genai.Content, error) {
		resp, err := sessionServer.Get(ctx, &session.GetRequest{AppName: ctx.AppName(), UserID: ctx.UserID(), SessionID: ctx.SessionID()})
		if err != nil {
			log.Errorf("Failed to get completed session: %v", err)
			return nil, fmt.Errorf("failed to get completed session: %v", err)
		}
		if err := memoryServer.AddSession(ctx, resp.Session); err != nil {
			log.Errorf("Failed to add session to memory: %v", err)
			return nil, fmt.Errorf("failed to add session to memory: %v", err)
		}

		log.Infof("[Callback] Session %s added to memory.", ctx.SessionID())
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
		log.Errorf("NewLLMAgent failed: %v", err)
		return
	}

	runner1, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          a,
		SessionService: sessionServer,
		MemoryService:  memoryServer,
	})
	if err != nil {
		log.Errorf("create runner1 error %v", err)
	}

	SessionID := "session123456789"

	s, err := sessionServer.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: SessionID,
	})
	if err != nil {
		log.Errorf("sessionService.Create error: %v", err)
	}

	s.Session.State()

	userInput1 := genai.NewContentFromText("My favorite project is Project Alpha.", "user")
	var finalResponseText string
	for event, err := range runner1.Run(ctx, userID, SessionID, userInput1, agent.RunConfig{}) {
		if err != nil {
			log.Errorf("Agent 1 Error: %v", err)
			continue
		}
		if event.Content != nil && !event.Partial {
			finalResponseText = strings.Join(textParts(event.Content), "")
		}
	}
	log.Infof("Agent 1 Response: %s\n", finalResponseText)

	// Add the completed session to the Memory Service
	log.Info("\n--- Adding Session 1 to Memory ---")
	resp, err := sessionServer.Get(ctx, &session.GetRequest{AppName: s.Session.AppName(), UserID: s.Session.UserID(), SessionID: s.Session.ID()})
	if err != nil {
		log.Errorf("Failed to get completed session: %v", err)
		return
	}
	if err := memoryServer.AddSession(ctx, resp.Session); err != nil {
		log.Errorf("Failed to add session to memory: %v", err)
		return
	}
	log.Info("Session added to memory.")

	log.Info("\n--- Turn 2: Recalling Information ---")

	runner2, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          a,
		SessionService: sessionServer,
		MemoryService:  memoryServer,
	})
	if err != nil {
		log.Errorf("create runner2 error %v", err)
		return
	}

	s, _ = sessionServer.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: "session2222",
	})

	userInput2 := genai.NewContentFromText("What is my favorite project?", "user")

	var finalResponseText2 []string
	for event, err := range runner2.Run(ctx, s.Session.UserID(), s.Session.ID(), userInput2, agent.RunConfig{}) {
		if err != nil {
			log.Errorf("Agent 2 Error: %v", err)
			continue
		}
		if event.Content != nil && !event.Partial {
			for _, part := range event.Content.Parts {
				finalResponseText2 = append(finalResponseText2, part.Text)
			}
		}
	}
	log.Infof("Agent 2 Response: %s\n", strings.Join(finalResponseText2, ""))
}

func textParts(Content *genai.Content) []string {
	var texts []string
	for _, part := range Content.Parts {
		texts = append(texts, part.Text)
	}
	return texts
}
