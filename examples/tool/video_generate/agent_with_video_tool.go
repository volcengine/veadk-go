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

	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/tool/builtin_tools"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/artifact"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
)

func main() {
	ctx := context.Background()
	cfg := &veagent.Config{
		ModelName:    common.DEFAULT_MODEL_AGENT_NAME,
		ModelAPIBase: common.DEFAULT_MODEL_AGENT_API_BASE,
		ModelAPIKey:  os.Getenv(common.MODEL_AGENT_API_KEY),
	}
	videoGenerate, err := builtin_tools.NewVideoGenerateTool(&builtin_tools.VideoGenerateConfig{
		ModelName: common.DEFAULT_MODEL_VIDEO_NAME,
		BaseURL:   common.DEFAULT_MODEL_VIDEO_API_BASE,
		APIKey:    os.Getenv(common.MODEL_VIDEO_API_KEY),
	})
	if err != nil {
		fmt.Printf("NewVideoGenerateTool failed: %v", err)
		return
	}

	cfg.Tools = []tool.Tool{videoGenerate}

	sessionService := session.InMemoryService()
	rootAgent, err := veagent.New(cfg)

	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	agentLoader, err := agent.NewMultiLoader(
		rootAgent,
	)
	if err != nil {
		log.Fatalf("Failed to create agent loader: %v", err)
	}

	artifactservice := artifact.InMemoryService()
	config := &launcher.Config{
		ArtifactService: artifactservice,
		SessionService:  sessionService,
		AgentLoader:     agentLoader,
	}

	l := full.NewLauncher()
	if err = l.Execute(ctx, config, os.Args[1:]); err != nil {
		log.Fatalf("Run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}
