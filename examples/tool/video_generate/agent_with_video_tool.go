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
	"github.com/volcengine/veadk-go/tool/builtin_tools"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/tool"
)

func main() {
	ctx := context.Background()
	videoGenerate, err := builtin_tools.NewVideoGenerateTool(&builtin_tools.VideoGenerateConfig{
		ModelName: "doubao-seedance-1-5-pro-251215",
	})
	if err != nil {
		fmt.Printf("NewVideoGenerateTool failed: %v", err)
		return
	}

	rootAgent, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Tools: []tool.Tool{videoGenerate},
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
