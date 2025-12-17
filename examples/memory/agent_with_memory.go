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
	"log"
	"strings"

	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/common"
	vem "github.com/volcengine/veadk-go/memory"
	"github.com/volcengine/veadk-go/tool/builtin_tools"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()
	appName := "ve_agent"
	userID := "user1111"

	// Define a tools that can search memory.
	memorySearchTool, err := builtin_tools.LoadLongMemoryTool()
	if err != nil {
		log.Fatal(err)
		return
	}

	infoCaptureAgent, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:        "InfoCaptureAgent",
			Instruction: "Acknowledge the user's statement.",
		},
		ModelName:    common.DEFAULT_MODEL_AGENT_NAME,
		ModelAPIBase: common.DEFAULT_MODEL_AGENT_API_BASE,
		ModelAPIKey:  utils.GetEnvWithDefault(common.MODEL_AGENT_API_KEY),
	})
	if err != nil {
		log.Printf("NewLLMAgent failed: %v", err)
		return
	}

	cfg := &veagent.Config{
		ModelName:    common.DEFAULT_MODEL_AGENT_NAME,
		ModelAPIBase: common.DEFAULT_MODEL_AGENT_API_BASE,
		ModelAPIKey:  utils.GetEnvWithDefault(common.MODEL_AGENT_API_KEY),
	}
	cfg.Name = "MemoryRecallAgent"
	cfg.Instruction = "Answer the user's question. Use the 'search_past_conversations' tools if the answer might be in past conversations."

	cfg.Tools = []tool.Tool{memorySearchTool}

	memorySearchAgent, err := veagent.New(cfg)
	if err != nil {
		log.Printf("NewLLMAgent failed: %v", err)
		return
	}

	// Use all default config
	//sessionService, err := vem.NewShortTermMemoryService(vem.BackendShortTermPostgreSQL, nil)
	//if err != nil {
	//	log.Printf("NewShortTermMemoryService failed: %v", err)
	//	return
	//}
	sessionService := session.InMemoryService()
	memoryService, err := vem.NewLongTermMemoryService(vem.BackendLongTermViking, nil)
	if err != nil {
		log.Printf("NewLongTermMemoryService failed: %v", err)
		return
	}

	runner1, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          infoCaptureAgent,
		SessionService: sessionService,
		MemoryService:  memoryService,
	})
	if err != nil {
		log.Fatal(err)
	}

	SessionID := "session123456789"

	s, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: SessionID,
	})
	if err != nil {
		log.Fatalf("sessionService.Create error: %v", err)
	}

	s.Session.State()

	userInput1 := genai.NewContentFromText("My favorite project is Project Alpha.", "user")
	var finalResponseText string
	for event, err := range runner1.Run(ctx, userID, SessionID, userInput1, agent.RunConfig{}) {
		if err != nil {
			log.Printf("Agent 1 Error: %v", err)
			continue
		}
		if event.Content != nil && !event.Partial {
			finalResponseText = strings.Join(textParts(event.Content), "")
		}
	}
	log.Printf("Agent 1 Response: %s\n", finalResponseText)

	// Add the completed session to the Memory Service
	log.Println("\n--- Adding Session 1 to Memory ---")
	resp, err := sessionService.Get(ctx, &session.GetRequest{AppName: s.Session.AppName(), UserID: s.Session.UserID(), SessionID: s.Session.ID()})
	if err != nil {
		log.Fatalf("Failed to get completed session: %v", err)
	}
	if err := memoryService.AddSession(ctx, resp.Session); err != nil {
		log.Fatalf("Failed to add session to memory: %v", err)
	}
	log.Println("Session added to memory.")

	log.Println("\n--- Turn 2: Recalling Information ---")

	runner2, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          memorySearchAgent,
		SessionService: sessionService,
		MemoryService:  memoryService,
	})
	if err != nil {
		log.Fatal(err)
	}

	s, _ = sessionService.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: "session2222",
	})

	userInput2 := genai.NewContentFromText("What is my favorite project?", "user")

	var finalResponseText2 []string
	for event, err := range runner2.Run(ctx, s.Session.UserID(), s.Session.ID(), userInput2, agent.RunConfig{}) {
		if err != nil {
			log.Printf("Agent 2 Error: %v", err)
			continue
		}
		if event.Content != nil && !event.Partial {
			for _, part := range event.Content.Parts {
				finalResponseText2 = append(finalResponseText2, part.Text)
			}
		}
	}
	log.Printf("Agent 2 Response: %s\n", strings.Join(finalResponseText2, ""))

}

func textParts(Content *genai.Content) []string {
	var texts []string
	for _, part := range Content.Parts {
		texts = append(texts, part.Text)
	}
	return texts
}
