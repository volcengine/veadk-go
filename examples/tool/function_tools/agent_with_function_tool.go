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
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// CalculatorAddArgs 定义加法工具的入参。使用静态类型，便于 LLM 以 JSON 方式调用。
type CalculatorAddArgs struct {
	A float64 `json:"a" jsonschema:"第一个加数，支持整数或小数"`
	B float64 `json:"b" jsonschema:"第二个加数，支持整数或小数"`
}

// CalculatorAddTool 返回一个符合 ADK functiontool 规范的工具。
// 该工具用于执行两数相加，并返回 result 字段。
func CalculatorAddTool() (tool.Tool, error) {
	handler := func(ctx tool.Context, args CalculatorAddArgs) (map[string]any, error) {
		result := args.A + args.B
		return map[string]any{
			"result":  result,
			"explain": fmt.Sprintf("%g + %g = %g", args.A, args.B, result),
		}, nil
	}

	return functiontool.New(
		functiontool.Config{
			Name:        "calculator_add",
			Description: "一个简单的计算器工具，执行两数相加。参数: a, b; 返回: result(浮点数)",
		},
		handler,
	)
}

type MessageCheckerArgs struct {
	UserMessage string `json:"user_message" jsonschema:"The user message to check."`
}

func NewMessageCheckerTool() (tool.Tool, error) {
	messageCheckerHandler := func(ctx tool.Context, args MessageCheckerArgs) (map[string]any, error) {
		return map[string]any{
			"result": fmt.Sprintf("Checked message: %s", args.UserMessage),
			"explain": map[string]string{
				"user_message": ctx.AgentName(),
				"app_name":     ctx.AppName(),
				"agent_name":   ctx.AgentName(),
				"user_id":      ctx.UserID(),
				"session_id":   ctx.SessionID(),
			},
		}, nil
	}

	return functiontool.New(
		functiontool.Config{
			Name:        "message_checker",
			Description: "Check message and log context information.",
		},
		messageCheckerHandler,
	)
}

func main() {
	ctx := context.Background()
	rootAgent, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Tools: []tool.Tool{utils.Must(CalculatorAddTool()), utils.Must(NewMessageCheckerTool())},
		},
	})

	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	app := agentkit_server_app.NewAgentkitServerApp(apps.DefaultApiConfig())

	err = app.Run(ctx, &apps.RunConfig{
		AgentLoader: agent.NewSingleLoader(rootAgent),
	})
	if err != nil {
		fmt.Printf("Run failed: %v", err)
	}
}
