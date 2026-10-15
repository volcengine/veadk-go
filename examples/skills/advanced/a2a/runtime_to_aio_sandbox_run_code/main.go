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

// Command runtime_to_aio_sandbox_run_code demonstrates loading remote skills in
// the runtime Agent while delegating code execution to an AIO/script sandbox
// through run_code.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"github.com/volcengine/veadk-go/v2/common"
	verunner "github.com/volcengine/veadk-go/v2/runner"
	"github.com/volcengine/veadk-go/v2/skills"
	"github.com/volcengine/veadk-go/v2/tool/builtin_tools"
	"github.com/volcengine/veadk-go/v2/tool/skilltool"
	"google.golang.org/adk/v2/agent"
	adkllmagent "google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/tool"
)

const (
	source      = "runtime-to-aio-sandbox-run-code"
	appName     = "aio_skills_runtime_agent"
	userID      = "aio_skills_user"
	instruction = `You are a skill execution assistant, and you have the run_code tool.
If you need to execute skills to complete a task, inspect remote skills with SkillToolset.
When the workflow needs code or shell execution, use run_code to execute in the AIO/script sandbox instead of running code locally.`
)

func main() {
	if err := run(); err != nil {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"ok": false, "error": err.Error(), "source": source})
		os.Exit(1)
	}
}

func run() error {
	toolID := flag.String("tool-id", firstEnv("AGENTKIT_TOOL_ID_SCRIPT", common.AGENTKIT_TOOL_ID), "AIO/script Sandbox Tool ID")
	skillSpaceID := flag.String("skill-space-id", os.Getenv("SKILL_SPACE_ID"), "Remote Skill Space ID")
	sessionID := flag.String("session-id", "", "Runtime Agent session ID")
	prompt := flag.String("prompt", "", "User request sent to the runtime Agent")
	modelName := flag.String("model-name", os.Getenv(common.MODEL_AGENT_NAME), "Runtime Agent model name")
	cacheDir := flag.String("cache", ".adk/remote-skills", "Remote Skill cache directory")
	flag.Parse()

	if *toolID == "" {
		return fmt.Errorf("--tool-id is required")
	}
	if *skillSpaceID == "" {
		return fmt.Errorf("--skill-space-id is required")
	}
	if *sessionID == "" {
		return fmt.Errorf("--session-id is required")
	}
	if *prompt == "" {
		return fmt.Errorf("--prompt is required")
	}
	os.Setenv(common.AGENTKIT_TOOL_ID, *toolID)

	skillToolset, err := remoteReadOnlySkillToolset(context.Background(), *skillSpaceID, *cacheDir)
	if err != nil {
		return err
	}
	runCode, err := builtin_tools.NewRunCodeSandboxTool()
	if err != nil {
		return err
	}
	cfg := veagent.Config{
		Config: adkllmagent.Config{
			Name:        "aio_skills_runtime_agent",
			Instruction: instruction,
			Toolsets:    []tool.Toolset{skillToolset},
			Tools:       []tool.Tool{runCode},
		},
	}
	if *modelName != "" {
		cfg.ModelName = *modelName
	}
	a, err := veagent.New(&cfg)
	if err != nil {
		return err
	}
	r, err := verunner.New(verunner.Config{
		AppName:           appName,
		Agent:             a,
		AutoCreateSession: true,
	})
	if err != nil {
		return err
	}
	response, err := r.RunText(context.Background(), userID, *sessionID, *prompt, agent.RunConfig{})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"ok":             true,
		"source":         source,
		"app_name":       appName,
		"user_id":        userID,
		"session_id":     *sessionID,
		"tool_id":        *toolID,
		"skill_space_id": *skillSpaceID,
		"response":       response,
	})
}

func remoteReadOnlySkillToolset(ctx context.Context, skillSpaceID, cacheDir string) (*skilltool.SkillToolset, error) {
	source, err := skills.NewRemoteSource(skills.RemoteSourceConfig{SpaceID: skillSpaceID})
	if err != nil {
		return nil, err
	}
	cache, err := skills.NewSkillCache(skills.SkillCacheConfig{Directory: cacheDir})
	if err != nil {
		return nil, err
	}
	registry, err := skills.NewRegistry(skills.RegistryConfig{Sources: []skills.SourceBinding{{Source: source}}, Cache: cache})
	if err != nil {
		return nil, err
	}
	defer registry.Close()
	if _, err := registry.Refresh(ctx); err != nil {
		return nil, err
	}
	snapshot := registry.Snapshot()
	defer snapshot.Close()
	var loaded []*skills.Skill
	for _, meta := range snapshot.List() {
		sk, err := snapshot.Get(ctx, meta.Name)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, sk)
	}
	return skilltool.NewReadOnlySkillToolset(loaded)
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
