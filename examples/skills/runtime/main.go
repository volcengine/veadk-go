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

// Command runtime demonstrates explicitly enabling local Skill execution.
//
// LocalRuntime executes trusted Skill commands as the current process user. It
// is not an OS sandbox; deploy it inside the container or sandbox which defines
// the intended filesystem and network boundary.
package main

import (
	"context"
	"os"

	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/apps"
	"github.com/volcengine/veadk-go/apps/agentkit_server_app"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/skills"
	"github.com/volcengine/veadk-go/tool/skilltool"
	"google.golang.org/adk/agent"
	adkllmagent "google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/tool"
)

func main() {
	ctx := context.Background()
	skillsRoot := os.Getenv("VEADK_SKILLS_DIR")
	if skillsRoot == "" {
		skillsRoot = ".adk/skills"
	}
	discovered, err := skills.DiscoverSkillsFromDirWithOptions(
		ctx,
		skillsRoot,
		skills.DiscoveryOptions{Mode: skills.DiscoveryStrict},
	)
	if err != nil {
		log.Errorf("DiscoverSkillsFromDir failed: %v", err)
		return
	}
	runtimeToolset, err := skilltool.NewLocalSkillToolset(
		discovered,
		skilltool.LocalRuntimeConfig{
			WorkspaceRoot: os.Getenv("VEADK_SKILL_WORKSPACE_ROOT"),
		},
	)
	if err != nil {
		log.Errorf("NewLocalSkillToolset failed: %v", err)
		return
	}
	defer func() {
		if err := runtimeToolset.Close(); err != nil {
			log.Errorf("Close local Skill runtime failed: %v", err)
		}
	}()

	a, err := veagent.New(&veagent.Config{Config: adkllmagent.Config{
		Toolsets: []tool.Toolset{runtimeToolset},
	}})
	if err != nil {
		log.Errorf("NewLLMAgent failed: %v", err)
		return
	}
	app := agentkit_server_app.NewAgentkitServerApp(apps.DefaultApiConfig())
	if err := app.Run(ctx, &apps.RunConfig{
		AgentLoader: agent.NewSingleLoader(a),
	}); err != nil {
		log.Errorf("Run failed: %v", err)
	}
}
