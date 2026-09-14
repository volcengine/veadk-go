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

package skilltool

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/volcengine/veadk-go/code_executors"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/skills"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/genai"
)

const MAX_SKILL_PAYLOAD_BYTES = skills.DefaultMaxResourceBytes
const defaultReadOnlySkillSystemInstruction = "" +
	"You can use specialized 'skills' to help you with complex tasks. You MUST use the skill tools to interact with these skills.\n\n" +
	"Skills are folders of instructions and resources that extend your capabilities for specialized tasks. Each skill folder contains:\n" +
	"- **SKILL.md** (required): The main instruction file with skill metadata and detailed markdown instructions.\n" +
	"- **references/** (Optional): Additional documentation or examples for skill usage.\n" +
	"- **assets/** (Optional): Templates, scripts or other resources used by the skill.\n" +
	"- **scripts/** (Optional): Executable scripts that can be run via bash.\n\n" +
	"This is very important:\n\n" +
	"1. If a skill seems relevant to the current user query, you MUST use the `load_skill` tool with `name=\"<SKILL_NAME>\"` to read its full instructions before proceeding.\n" +
	"2. Once you have read the instructions, follow them exactly as documented before replying to the user. For example, If the instruction lists multiple steps, please make sure you complete all of them in order.\n" +
	"3. The `load_skill_resource` tool is for viewing relative files within a skill's directory (e.g., `references/*`, `assets/*`, `scripts/*`, or files referenced beside SKILL.md). Do NOT use other tools to access these files.\n"

const DEFAULT_SKILL_SYSTEM_INSTRUCTION = defaultReadOnlySkillSystemInstruction +
	"4. Use `run_skill_script` to run scripts from a skill's `scripts/` directory. Use `load_skill_resource` to view script content first if needed.\n"

// SkillToolset A toolset for managing and interacting with agent skills.
type SkillToolset struct {
	skills       map[string]*skills.Skill
	tools        []tool.Tool
	codeExecutor code_executors.CodeExecutor
	instruction  string
	localRuntime *LocalRuntime
}

func NewSkillToolset(skillList []*skills.Skill, codeExecutor code_executors.CodeExecutor) (*SkillToolset, error) {
	return newSkillToolset(skillList, codeExecutor, true)
}

// NewReadOnlySkillToolset creates a toolset for discovered local skills. It
// exposes metadata and lazy resource reads, but deliberately does not expose
// script execution.
func NewReadOnlySkillToolset(skillList []*skills.Skill) (*SkillToolset, error) {
	return newSkillToolset(skillList, nil, false)
}

func newSkillToolset(skillList []*skills.Skill, codeExecutor code_executors.CodeExecutor, includeScriptTool bool) (*SkillToolset, error) {
	m := make(map[string]*skills.Skill, len(skillList))
	for _, s := range skillList {
		if s == nil || s.Frontmatter == nil {
			return nil, fmt.Errorf("skill and frontmatter are required")
		}
		if _, dup := m[s.Name()]; dup {
			return nil, fmt.Errorf("duplicate skill name '%s'", s.Name())
		}
		m[s.Name()] = s
	}
	st := &SkillToolset{
		skills:       m,
		codeExecutor: codeExecutor,
		instruction:  defaultReadOnlySkillSystemInstruction,
	}
	st.tools = []tool.Tool{
		st.listSkillsTool(),
		st.loadSkillTool(),
		st.loadSkillResourceTool(),
	}
	if includeScriptTool {
		st.tools = append(st.tools, st.runSkillScriptTool())
		st.instruction = DEFAULT_SKILL_SYSTEM_INSTRUCTION
	}
	return st, nil
}

func (s *SkillToolset) Name() string {
	return "SkillToolset"
}

func (s *SkillToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	return s.tools, nil
}

//func (s *SkillToolset) GetTools() []tool.Tool {
//	return s.tools
//}

func (s *SkillToolset) ProcessRequest(ctx tool.Context, req *model.LLMRequest) error {
	skillList := s.listSkills()
	skillXML := skills.FormatSkillsAsXML(skillList)
	instruction := []string{s.instruction, skillXML}
	if s.localRuntime != nil {
		instruction = append(instruction, localRuntimeInstruction)
	}
	if req.Config.SystemInstruction == nil {
		req.Config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{
				{
					Text: strings.Join(instruction, "\n\n"),
				},
			},
			Role: "user",
		}

	} else {
		req.Config.SystemInstruction.Parts = append(req.Config.SystemInstruction.Parts,
			&genai.Part{
				Text: strings.Join(instruction, "\n\n"),
			},
		)
	}
	systemInstructionStr, _ := json.Marshal(req.Config.SystemInstruction)
	log.Debugf("SkillToolset After ProcessRequest SystemInstruction is %s", systemInstructionStr)
	return nil
}

func (s *SkillToolset) getSkill(name string) (*skills.Skill, bool) {
	sk, ok := s.skills[name]
	return sk, ok
}

func (s *SkillToolset) listSkills() []*skills.Skill {
	var out = make([]*skills.Skill, 0, len(s.skills))
	for _, v := range s.skills {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

type listSkillsArgs struct{}

func (s *SkillToolset) listSkillsToolHandler(ctx tool.Context, args listSkillsArgs) (map[string]any, error) {
	xml := skills.FormatSkillsAsXML(s.listSkills())
	return map[string]any{"result": xml}, nil
}

// listSkillsTool Tool to list all available skills.
func (s *SkillToolset) listSkillsTool() tool.Tool {
	t, _ := functiontool.New(functiontool.Config{
		Name:        "list_skills",
		Description: "Lists all available skills with their names and descriptions.",
	}, s.listSkillsToolHandler)
	return t
}

type loadSkillArgs struct {
	Name string `json:"name" jsonschema:"The name of the skill to load."`
}

func (s *SkillToolset) loadSkillToolHandler(ctx tool.Context, args loadSkillArgs) (map[string]any, error) {
	if strings.TrimSpace(args.Name) == "" {
		return map[string]any{
			"error":      "Skill name is required.",
			"error_code": "MISSING_SKILL_NAME",
		}, nil
	}
	sk, ok := s.getSkill(args.Name)
	if !ok {
		return map[string]any{
			"error":      fmt.Sprintf("Skill '%s' not found.", args.Name),
			"error_code": "SKILL_NOT_FOUND",
		}, nil
	}

	return map[string]any{
		"skill_name":   sk.Name(),
		"instructions": sk.Instructions,
		"frontmatter":  sk.Frontmatter.ToJsonString(),
	}, nil
}

// loadSkillTool Tool to load a skill's instructions."""
func (s *SkillToolset) loadSkillTool() tool.Tool {
	t, _ := functiontool.New(functiontool.Config{
		Name:        "load_skill",
		Description: "Loads the SKILL.md instructions for a given skill.",
	}, s.loadSkillToolHandler)
	return t
}

type loadSkillResourceArgs struct {
	SkillName string `json:"skill_name" jsonschema:"The name of the skill."`
	Path      string `json:"path" jsonschema:"The relative path to the resource (e.g., 'references/x.md', 'assets/template.txt', 'scripts/setup.sh')."`
}

func (s *SkillToolset) loadSkillResourceToolHandler(ctx tool.Context, args loadSkillResourceArgs) (map[string]any, error) {
	if strings.TrimSpace(args.SkillName) == "" {
		return map[string]any{"error": "Skill name is required.", "error_code": "MISSING_SKILL_NAME"}, nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return map[string]any{"error": "Resource path is required.", "error_code": "MISSING_RESOURCE_PATH"}, nil
	}
	sk, ok := s.getSkill(args.SkillName)
	if !ok {
		return map[string]any{"error": fmt.Sprintf("Skill '%s' not found.", args.SkillName), "error_code": "SKILL_NOT_FOUND"}, nil
	}
	readContext := context.Background()
	if ctx != nil {
		readContext = ctx
	}
	data, err := sk.ReadResourceContext(readContext, args.Path, MAX_SKILL_PAYLOAD_BYTES)
	if err != nil {
		return resourceReadError(args, err), nil
	}

	mediaType := mime.TypeByExtension(filepath.Ext(args.Path))
	if mediaType == "" {
		mediaType = http.DetectContentType(data)
	}
	result := map[string]any{
		"skill_name": sk.Name(),
		"path":       args.Path,
		"media_type": mediaType,
	}
	if utf8.Valid(data) && !bytes.ContainsRune(data, '\x00') {
		result["content"] = string(data)
		result["encoding"] = "utf-8"
	} else {
		result["content"] = base64.StdEncoding.EncodeToString(data)
		result["encoding"] = "base64"
	}
	return result, nil
}

func resourceReadError(args loadSkillResourceArgs, err error) map[string]any {
	code := "RESOURCE_READ_ERROR"
	message := fmt.Sprintf("Resource '%s' in skill '%s' could not be read.", args.Path, args.SkillName)
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code = "RESOURCE_READ_CANCELED"
	case errors.Is(err, skills.ErrInvalidResourcePath):
		code = "INVALID_RESOURCE_PATH"
	case errors.Is(err, skills.ErrResourceNotFound):
		code = "RESOURCE_NOT_FOUND"
	case errors.Is(err, skills.ErrResourceTooLarge):
		code = "RESOURCE_TOO_LARGE"
	case errors.Is(err, skills.ErrResourceNotRegular):
		code = "INVALID_RESOURCE_TYPE"
	case errors.Is(err, skills.ErrResourceChanged):
		code = "RESOURCE_CHANGED"
	}
	return map[string]any{"error": message, "error_code": code}
}

// loadSkillResourceTool Tool to load resources (references, assets, or scripts) from a skill."""
func (s *SkillToolset) loadSkillResourceTool() tool.Tool {
	t, _ := functiontool.New(functiontool.Config{
		Name:        "load_skill_resource",
		Description: "Loads a relative resource file from within a skill.",
	}, s.loadSkillResourceToolHandler)
	return t
}

type runSkillScriptArgs struct {
	SkillName  string   `json:"skill_name" jsonschema:"The name of the skill."`
	ScriptPath string   `json:"script_path" jsonschema:"The relative path to the script (e.g., 'scripts/setup.py')."`
	Args       []string `json:"args_list" jsonschema:"Optional arguments to pass to the script as list."`
}

func (s *SkillToolset) runSkillScriptToolHandler(ctx tool.Context, args runSkillScriptArgs) (map[string]any, error) {
	if strings.TrimSpace(args.SkillName) == "" {
		return map[string]any{"error": "Skill name is required.", "error_code": "MISSING_SKILL_NAME"}, nil
	}
	if strings.TrimSpace(args.ScriptPath) == "" {
		return map[string]any{"error": "Script path is required.", "error_code": "MISSING_SCRIPT_PATH"}, nil
	}
	sk, ok := s.getSkill(args.SkillName)
	if !ok {
		return map[string]any{"error": fmt.Sprintf("Skill '%s' not found.", args.SkillName), "error_code": "SKILL_NOT_FOUND"}, nil
	}
	if s.localRuntime != nil {
		result := s.localRuntime.runSkillScript(ctx, sk, args.ScriptPath, args.Args)
		result["skill_name"] = sk.Name()
		result["script_path"] = args.ScriptPath
		return result, nil
	}
	name := args.ScriptPath
	if strings.HasPrefix(args.ScriptPath, "scripts/") {
		name = strings.TrimPrefix(args.ScriptPath, "scripts/")
	}

	if sk.Resources == nil {
		return map[string]any{"error": fmt.Sprintf("Script '%s' is not loaded for skill '%s'.", args.ScriptPath, args.SkillName), "error_code": "SCRIPT_NOT_LOADED"}, nil
	}
	if scr, ok := sk.Resources.GetScript(name); !ok || scr == nil {
		return map[string]any{"error": fmt.Sprintf("Script '%s' not found in skill '%s'.", args.ScriptPath, args.SkillName), "error_code": "SCRIPT_NOT_FOUND"}, nil
	}
	if s.codeExecutor == nil {
		return map[string]any{
			"error":      "No code executor configured. A code executor is required to run scripts.",
			"error_code": "NO_CODE_EXECUTOR",
		}, nil
	}
	argsStr, _ := json.Marshal(args)
	log.Debugf("runSkillScriptToolHandler args is %s", string(argsStr))
	codeExecutorResult, err := s.codeExecutor.ExecuteCode(nil, code_executors.CodeExecutionInput{
		Args:        args.Args,
		ScriptPath:  filepath.Join(sk.GetSkillPath(), "scripts", name),
		InputFiles:  nil,
		ExecutionID: ctx.InvocationID(),
	})
	resultStr, _ := json.Marshal(codeExecutorResult)
	log.Debugf("codeExecutor result is %s", string(resultStr))
	if err != nil {
		return map[string]any{
			"error":      fmt.Sprintf("Failed to execute script '%s':\n%s", args.ScriptPath, err.Error()),
			"error_code": "EXECUTION_ERROR",
		}, nil
	}
	status := "success"

	return map[string]any{
		"skill_name":  sk.Name(),
		"script_path": args.ScriptPath,
		"stdout":      codeExecutorResult.StdOut,
		"stderr":      codeExecutorResult.StdErr,
		"status":      status,
	}, nil
}

// runSkillScriptTool Tool to execute scripts from a skill's scripts/ directory."""
func (s *SkillToolset) runSkillScriptTool() tool.Tool {
	t, _ := functiontool.New(functiontool.Config{
		Name:        "run_skill_script",
		Description: "Executes a script from a skill's scripts/ directory.",
	}, s.runSkillScriptToolHandler)
	return t
}
