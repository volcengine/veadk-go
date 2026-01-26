// Copyright (c) 2025 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
//
// Licensed under the Apache License, Beijing 2.0 (the "License");
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
	"os"

	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/observability"
	"github.com/volcengine/veadk-go/utils"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/session"
)

func main() {
	stdExporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	observability.Init(context.Background())
	observability.AddSpanExporter(stdExporter)

	ctx := context.Background()

	// Create agent configuration
	cfg := &veagent.Config{
		ModelName:    common.DEFAULT_MODEL_AGENT_NAME,
		ModelAPIBase: common.DEFAULT_MODEL_AGENT_API_BASE,
		ModelAPIKey:  utils.GetEnvWithDefault(common.MODEL_AGENT_API_KEY),
	}

	a, err := veagent.New(cfg)
	if err != nil {
		log.Fatalf("NewLLMAgent failed: %v", err)
	}

	config := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
	}

	// 2. Wrap the Launcher for full richness (Restore root span)
	l := observability.NewObservedLauncher(full.NewLauncher())

	// Run with CLI arguments
	// Use os.Args[1:] to let ADK handle its own subcommands (console, api, etc.)
	args := os.Args[1:]

	log.Println("Starting Observed Launcher...")
	if err = l.Execute(ctx, config, args); err != nil {
		log.Fatalf("Run failed: %v", err)
	}
}
