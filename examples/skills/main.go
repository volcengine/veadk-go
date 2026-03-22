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
	"time"

	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/apps"
	"github.com/volcengine/veadk-go/apps/agentkit_server_app"
	"github.com/volcengine/veadk-go/code_executors"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/skills"
	"github.com/volcengine/veadk-go/tool/skilltool"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adktool "google.golang.org/adk/tool"
)

func main() {
	ctx := context.Background()

	skillPathList := []string{
		".adk/skills/multiplication-calculator",
		".adk/skills/image-generate",
		".adk/skills/video-generate",
	}
	var skillList []*skills.Skill
	for _, path := range skillPathList {
		skill, err := skills.LoadSkillFromDir(path)
		if err != nil {
			panic(err)
		}
		skillList = append(skillList, skill)
	}

	skillToolset, err := skilltool.NewSkillToolset(skillList, code_executors.NewUnsafeLocalCodeExecutor(300*time.Second))
	if err != nil {
		panic(err)
	}
	a, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Toolsets: []adktool.Toolset{skillToolset},
		},
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
