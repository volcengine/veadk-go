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
	"iter"
	"strings"

	verunner "github.com/volcengine/veadk-go/v2/runner"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()

	echoAgent, err := newEchoAgent()
	if err != nil {
		panic(err)
	}
	r, err := verunner.New(verunner.Config{
		AppName:           "runner_basic",
		Agent:             echoAgent,
		AutoCreateSession: true,
	})
	if err != nil {
		panic(err)
	}

	answer, err := r.RunText(ctx, "user", "text-session", "hello", agent.RunConfig{})
	if err != nil {
		panic(err)
	}
	fmt.Println(answer)

	msg := verunner.UserParts(
		genai.NewPartFromText("describe this inline image"),
		genai.NewPartFromBytes([]byte("fake image bytes"), "image/png"),
	)
	answer, err = verunner.CollectFinalText(r.Run(ctx, "user", "parts-session", msg, agent.RunConfig{}))
	if err != nil {
		panic(err)
	}
	fmt.Println(answer)

	for event, err := range r.Run(ctx, "user", "event-session", verunner.UserText("show raw events"), agent.RunConfig{}) {
		if err != nil {
			panic(err)
		}
		if event.Content != nil {
			fmt.Println(visibleText(event.Content))
		}
	}
}

func newEchoAgent() (agent.Agent, error) {
	return agent.New(agent.Config{
		Name: "echo",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				event := session.NewEvent(ctx, ctx.InvocationID())
				event.Author = "echo"
				event.LLMResponse = model.LLMResponse{
					Content: genai.NewContentFromText("echo: "+describeContent(ctx.UserContent()), genai.RoleModel),
				}
				yield(event, nil)
			}
		},
	})
}

func describeContent(content *genai.Content) string {
	if content == nil {
		return ""
	}
	var textParts []string
	var inlineParts int
	for _, part := range content.Parts {
		if part == nil {
			continue
		}
		if part.Text != "" {
			textParts = append(textParts, part.Text)
		}
		if part.InlineData != nil {
			inlineParts++
		}
	}
	text := strings.Join(textParts, " ")
	if inlineParts == 0 {
		return text
	}
	return fmt.Sprintf("%s (%d inline part)", text, inlineParts)
}

func visibleText(content *genai.Content) string {
	var text strings.Builder
	for _, part := range content.Parts {
		if part != nil && !part.Thought {
			text.WriteString(part.Text)
		}
	}
	return text.String()
}
