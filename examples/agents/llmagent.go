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
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/tool"
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

	config := &launcher.Config{
		AgentLoader: agent.NewSingleLoader(a),
	}

	l := full.NewLauncher()
	if err = l.Execute(ctx, config, os.Args[1:]); err != nil {
		log.Fatalf("Run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}

}
