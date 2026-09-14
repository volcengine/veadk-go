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
	"context"
	"errors"

	"github.com/volcengine/veadk-go/skills"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// RegistrySkillToolset exposes a pinned registry snapshot with lazy downloads.
// The caller owns the snapshot and closes it after all runs using this toolset
// finish. Create another toolset from a new snapshot to adopt a refresh.
// Execution is deliberately left to the caller's existing runtime/tool policy.
type RegistrySkillToolset struct {
	snapshot *skills.SkillSnapshot
	metadata *SkillToolset
	tools    []tool.Tool
}

func NewRegistrySkillToolset(snapshot *skills.SkillSnapshot) (*RegistrySkillToolset, error) {
	if snapshot == nil {
		return nil, errors.New("skill snapshot required")
	}
	list := []*skills.Skill{}
	for _, f := range snapshot.List() {
		front := f
		list = append(list, &skills.Skill{Frontmatter: &front})
	}
	metadata, err := NewReadOnlySkillToolset(list)
	if err != nil {
		return nil, err
	}
	s := &RegistrySkillToolset{snapshot: snapshot, metadata: metadata}
	load, err := functiontool.New(functiontool.Config{Name: "load_skill", Description: "Loads instructions for a skill, downloading its pinned remote version on demand."}, s.load)
	if err != nil {
		return nil, err
	}
	resource, err := functiontool.New(functiontool.Config{Name: "load_skill_resource", Description: "Reads a relative resource from a skill's pinned version."}, s.resource)
	if err != nil {
		return nil, err
	}
	s.tools = []tool.Tool{metadata.listSkillsTool(), load, resource}
	return s, nil
}
func (s *RegistrySkillToolset) Name() string { return "RegistrySkillToolset" }
func (s *RegistrySkillToolset) Tools(agent.ReadonlyContext) ([]tool.Tool, error) {
	return append([]tool.Tool(nil), s.tools...), nil
}
func (s *RegistrySkillToolset) ProcessRequest(ctx tool.Context, req *model.LLMRequest) error {
	return s.metadata.ProcessRequest(ctx, req)
}
func (s *RegistrySkillToolset) resolve(ctx tool.Context, name string) (*SkillToolset, error) {
	requestContext := context.Background()
	if ctx != nil {
		requestContext = ctx
	}
	sk, err := s.snapshot.Get(requestContext, name)
	if err != nil {
		return nil, err
	}
	return NewReadOnlySkillToolset([]*skills.Skill{sk})
}
func (s *RegistrySkillToolset) load(ctx tool.Context, args loadSkillArgs) (map[string]any, error) {
	st, err := s.resolve(ctx, args.Name)
	if err != nil {
		return registryToolError(err), nil
	}
	return st.loadSkillToolHandler(ctx, args)
}
func (s *RegistrySkillToolset) resource(ctx tool.Context, args loadSkillResourceArgs) (map[string]any, error) {
	st, err := s.resolve(ctx, args.SkillName)
	if err != nil {
		return registryToolError(err), nil
	}
	return st.loadSkillResourceToolHandler(ctx, args)
}
func registryToolError(err error) map[string]any {
	code := "SKILL_LOAD_FAILED"
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = "SKILL_LOAD_CANCELED"
	}
	return map[string]any{"error": "Skill could not be loaded.", "error_code": code}
}
