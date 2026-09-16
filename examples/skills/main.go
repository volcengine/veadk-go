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

	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"github.com/volcengine/veadk-go/v2/apps"
	"github.com/volcengine/veadk-go/v2/apps/agentkit_server_app"
	"github.com/volcengine/veadk-go/v2/log"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
)

func main() {
	ctx := context.Background()

	skillsRoot := os.Getenv("VEADK_SKILLS_DIR")
	if skillsRoot == "" {
		skillsRoot = ".adk/skills"
	}
	a, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{},
		Skills: []string{skillsRoot},
	})
	if err != nil {
		log.Errorf("NewLLMAgent failed: %v", err)
		return
	}

	app := agentkit_server_app.NewAgentkitServerApp(apps.DefaultApiConfig())

	err = app.Run(ctx, &apps.RunConfig{
		AgentLoader: agent.NewSingleLoader(a),
	})
	if err != nil {
		log.Errorf("Run failed: %v", err)
	}
}
