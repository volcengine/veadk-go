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
	"path/filepath"
	"strings"

	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"github.com/volcengine/veadk-go/v2/code_executors"
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
Use remote Skill metadata only to understand available workflows.
When the user asks you to run Python code, verify code output, or perform a skill workflow step that requires a Python skill script, you must call run_code or run_skill_script before answering.
SkillToolset Python script execution is backed by AgentKit run_code, so run_skill_script also executes in the AIO/script sandbox.
Do not simulate execution or claim code has run unless run_code or run_skill_script returned a result.
If the user only asks what skills are available, answer from the loaded Skill metadata; that query does not require AIO sandbox execution.`
	defaultCodeTimeoutSeconds = 300
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
	setScriptToolIDEnv(*toolID)

	skillToolset, err := remoteSkillToolset(context.Background(), *skillSpaceID, *cacheDir)
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

type agentKitRunCodeExecutor struct {
	timeout uint
}

func (e *agentKitRunCodeExecutor) ExecuteCode(ctx agent.InvocationContext, input code_executors.CodeExecutionInput) (code_executors.CodeExecutionResult, error) {
	toolCtx, ok := ctx.(agent.Context)
	if !ok || toolCtx == nil {
		return code_executors.CodeExecutionResult{StdErr: "MISSING_TOOL_CONTEXT: AgentKit run_code execution requires agent tool context."}, nil
	}
	ext := strings.ToLower(filepath.Ext(input.ScriptPath))
	if ext != ".py" {
		extMsg := "(no extension)"
		if ext != "" {
			extMsg = ext
		}
		return code_executors.CodeExecutionResult{StdErr: fmt.Sprintf("UNSUPPORTED_SCRIPT_TYPE: Unsupported script type '%s'. Supported types: .py", extMsg)}, nil
	}
	script, err := os.ReadFile(input.ScriptPath)
	if err != nil {
		return code_executors.CodeExecutionResult{}, err
	}
	args := normalizeScriptArgs(input.Args)
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return code_executors.CodeExecutionResult{}, err
	}
	code := fmt.Sprintf("import sys\nsys.argv = [%q] + %s\n%s", filepath.Base(input.ScriptPath), string(argsJSON), string(script))
	timeout := e.timeout
	if timeout == 0 || timeout > defaultCodeTimeoutSeconds {
		timeout = defaultCodeTimeoutSeconds
	}
	output, err := builtin_tools.RunCodeSandbox(toolCtx, builtin_tools.RunCodeArgs{
		Code:     code,
		Language: "python3",
		Timeout:  timeout,
	})
	if err != nil {
		return code_executors.CodeExecutionResult{}, err
	}
	return code_executors.CodeExecutionResult{StdOut: stringifyRunCodeOutput(output["result"])}, nil
}

func normalizeScriptArgs(raw any) []string {
	var out []string
	switch args := raw.(type) {
	case []string:
		return args
	case []any:
		for _, item := range args {
			out = append(out, fmt.Sprint(item))
		}
	default:
		if raw != nil {
			out = append(out, fmt.Sprint(raw))
		}
	}
	return out
}

func stringifyRunCodeOutput(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

func remoteSkillToolset(ctx context.Context, skillSpaceID, cacheDir string) (*skilltool.SkillToolset, error) {
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
	return skilltool.NewSkillToolset(loaded, &agentKitRunCodeExecutor{timeout: defaultCodeTimeoutSeconds})
}

func setScriptToolIDEnv(toolID string) {
	os.Setenv(common.AGENTKIT_TOOL_ID_SCRIPT, toolID)
	if os.Getenv(common.AGENTKIT_TOOL_ID) == "" {
		os.Setenv(common.AGENTKIT_TOOL_ID, toolID)
	}
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
