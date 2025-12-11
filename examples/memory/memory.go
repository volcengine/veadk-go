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
	"strings"

	veagent "github.com/volcengine/veadk-go/agent"
	"github.com/volcengine/veadk-go/common"
	vem "github.com/volcengine/veadk-go/memory"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()
	appName := "ve_agent"
	userID := "user1111"

	// Define a tool that can search memory.
	memorySearchTool, err := functiontool.New(
		functiontool.Config{
			Name:        "search_past_conversations",
			Description: "Searches past conversations for relevant information.",
		},
		memorySearchToolFunc,
	)
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
	cfg.Instruction = "Answer the user's question. Use the 'search_past_conversations' tool if the answer might be in past conversations."

	cfg.Tools = []tool.Tool{memorySearchTool}

	memorySearchAgent, err := veagent.New(cfg)
	if err != nil {
		log.Printf("NewLLMAgent failed: %v", err)
		return
	}

	// Use all default config
	sessionService, err := vem.NewShortTermMemoryService(vem.BackendShortTermPostgreSQL, nil)
	if err != nil {
		log.Printf("NewShortTermMemoryService failed: %v", err)
		return
	}
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
		if event.Content != nil && !event.LLMResponse.Partial {
			finalResponseText = strings.Join(textParts(event.LLMResponse.Content), "")
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

	var finalResponseText2 string
	for event, err := range runner2.Run(ctx, s.Session.UserID(), s.Session.ID(), userInput2, agent.RunConfig{}) {
		if err != nil {
			log.Printf("Agent 2 Error: %v", err)
			continue
		}
		if event.Content != nil && !event.LLMResponse.Partial {
			finalResponseText2 = strings.Join(textParts(event.LLMResponse.Content), "")
		}
	}
	log.Printf("Agent 2 Response: %s\n", finalResponseText2)

}

// Args defines the input structure for the memory search tool.
type Args struct {
	Query string `json:"query" jsonschema:"The query to search for in the memory."`
}

// Result defines the output structure for the memory search tool.
type Result struct {
	Results []string `json:"results"`
}

// memorySearchToolFunc is the implementation of the memory search tool.
// This function demonstrates accessing memory via tool.Context.
func memorySearchToolFunc(tctx tool.Context, args Args) (Result, error) {
	fmt.Printf("Tool: Searching memory for query: '%s'\n", args.Query)
	// The SearchMemory function is available on the context.
	searchResults, err := tctx.SearchMemory(context.Background(), args.Query)
	if err != nil {
		log.Printf("Error searching memory: %v", err)
		return Result{}, fmt.Errorf("failed memory search")
	}

	var results []string
	for _, res := range searchResults.Memories {
		if res.Content != nil {
			results = append(results, textParts(res.Content)...)
		}
	}
	return Result{Results: results}, nil
}

func textParts(Content *genai.Content) []string {
	var texts []string
	for _, part := range Content.Parts {
		texts = append(texts, part.Text)
	}
	return texts
}
