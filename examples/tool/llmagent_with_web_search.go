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
	"os"

	"github.com/volcengine/veadk-go/agents/llmagent"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/prompts"
	"github.com/volcengine/veadk-go/tool/builtin_tools/web_search"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()
	cfg := llmagent.Config{
		ModelName:    common.DEFAULT_MODEL_AGENT_NAME,
		ModelApiBase: common.DEFAULT_MODEL_AGENT_API_BASE,
		ModelApiKey:  os.Getenv("MODEL_API_KEY"),
	}
	cfg.Name = "veadk-llmagent"
	cfg.Instruction = prompts.DEFAULT_INSTRUCTION
	cfg.Description = prompts.DEFAULT_DESCRIPTION

	cfg.Tools = []tool.Tool{web_search.WebSearchTool}

	a, err := llmagent.New(cfg)
	if err != nil {
		fmt.Printf("NewLLMAgent failed: %v", err)
		return
	}

	sessionService := session.InMemoryService()
	runner, err := runner.New(runner.Config{
		AppName:        "weather_sentiment_agent",
		Agent:          a,
		SessionService: sessionService,
	})
	if err != nil {
		log.Fatal(err)
	}

	session, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName: "weather_sentiment_agent",
		UserID:  "user1234",
	})
	if err != nil {
		log.Fatal(err)
	}

	run(ctx, runner, session.Session.ID(), "How is the weather in beijing today")

}

func run(ctx context.Context, r *runner.Runner, sessionID string, prompt string) {
	fmt.Printf("\n> %s\n", prompt)
	events := r.Run(
		ctx,
		"user1234",
		sessionID,
		genai.NewContentFromText(prompt, genai.RoleUser),
		agent.RunConfig{
			StreamingMode: agent.StreamingModeNone,
		},
	)
	for event, err := range events {
		if err != nil {
			log.Fatalf("ERROR during agent execution: %v", err)
		}

		if event.Content.Parts[0].Text != "" {
			fmt.Printf("Agent Response: %s\n", event.Content.Parts[0].Text)
		}
	}
}
