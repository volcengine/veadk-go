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

package llmagent

import (
	"context"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volcengine/veadk-go/v2/skills"
	adkllmagent "google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"
)

type localSkillsTestModel struct{}

func (localSkillsTestModel) Name() string { return "local-skills-test" }

func (localSkillsTestModel) GenerateContent(context.Context, *adkmodel.LLMRequest, bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {}
}

func TestNewDiscoversSkillsFromRootDirectory(t *testing.T) {
	root := t.TempDir()
	writeAgentTestSkill(t, root, "zeta", "last")
	writeAgentTestSkill(t, root, "alpha", "first")

	cfg := &Config{
		Config: adkllmagent.Config{
			Name:  "root-directory-skills",
			Model: localSkillsTestModel{},
		},
		Skills: []string{"  ", root},
	}
	if _, err := New(cfg); err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := len(cfg.Toolsets); got != 1 {
		t.Fatalf("Toolsets count = %d, want 1", got)
	}
	tools, err := cfg.Toolsets[0].Tools(nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, skillTool := range tools {
		names = append(names, skillTool.Name())
	}
	if got, want := strings.Join(names, ","), "list_skills,load_skill,load_skill_resource"; got != want {
		t.Fatalf("skill tools = %q, want %q", got, want)
	}
}

func TestNewLocalSkillsHonorsStrictDiscovery(t *testing.T) {
	root := t.TempDir()
	writeAgentTestSkill(t, root, "valid", "valid")
	if err := os.MkdirAll(filepath.Join(root, "invalid"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Config: adkllmagent.Config{Name: "strict-skills", Model: localSkillsTestModel{}},
		Skills: []string{root},
		SkillDiscoveryOptions: skills.DiscoveryOptions{
			Mode: skills.DiscoveryStrict,
		},
	}
	if _, err := New(cfg); err == nil {
		t.Fatal("New() strict discovery error = nil")
	}
	if len(cfg.Toolsets) != 0 {
		t.Fatal("New() mutated toolsets after strict discovery failure")
	}
}

func TestNewLocalSkillsRejectsDuplicateNamesAcrossRoots(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	writeAgentTestSkill(t, firstRoot, "duplicate", "first")
	writeAgentTestSkill(t, secondRoot, "duplicate", "second")

	_, err := New(&Config{
		Config: adkllmagent.Config{Name: "duplicate-skills", Model: localSkillsTestModel{}},
		Skills: []string{firstRoot, secondRoot},
	})
	if !errors.Is(err, skills.ErrDuplicateSkill) {
		t.Fatalf("New() error = %v, want ErrDuplicateSkill", err)
	}
}

func writeAgentTestSkill(t *testing.T, root, name, description string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	document := "---\nname: " + name + "\ndescription: " + description + "\n---\nUse this skill.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
}
