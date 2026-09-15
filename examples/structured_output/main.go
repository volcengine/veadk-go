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
	"os"

	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/log"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()

	rootAgent, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:        "structured_output_agent",
			Description: "Agent that returns structured weather information.",
			Instruction: "Return the requested weather information using the output schema.",
			OutputSchema: &genai.Schema{
				Type:  genai.TypeObject,
				Title: "weather_response",
				Properties: map[string]*genai.Schema{
					"location":    {Type: genai.TypeString},
					"temperature": {Type: genai.TypeNumber},
					"unit": {
						Type: genai.TypeString,
						Enum: []string{"celsius", "fahrenheit"},
					},
				},
				Required: []string{"location", "temperature", "unit"},
			},
		},
	})
	if err != nil {
		log.Errorf("NewLLMAgent failed: %v", err)
		return
	}

	config := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(rootAgent),
		SessionService: session.InMemoryService(),
	}
	l := full.NewLauncher()
	if err = l.Execute(ctx, config, os.Args[1:]); err != nil {
		log.Errorf("Run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}
