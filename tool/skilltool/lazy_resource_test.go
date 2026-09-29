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
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volcengine/veadk-go/v2/skills"
)

func TestLoadSkillResourceToolReadsLazyTextAndBinary(t *testing.T) {
	root := t.TempDir()
	writeLazySkillFile(t, filepath.Join(root, "lazy-skill", "SKILL.md"), "---\nname: lazy-skill\ndescription: lazy resources\n---\ninstructions\n")
	writeLazySkillFile(t, filepath.Join(root, "lazy-skill", "references", "guide.md"), "guide")
	binary := []byte{0xff, 0x00, 0x01}
	binaryPath := filepath.Join(root, "lazy-skill", "assets", "template.bin")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, binary, 0o644); err != nil {
		t.Fatal(err)
	}

	discovered, err := skills.DiscoverSkillsFromDir(root)
	if err != nil {
		t.Fatal(err)
	}
	toolset, err := NewSkillToolset(discovered, nil)
	if err != nil {
		t.Fatal(err)
	}
	textResult, err := toolset.loadSkillResourceToolHandler(nil, loadSkillResourceArgs{SkillName: "lazy-skill", Path: "references/guide.md"})
	if err != nil {
		t.Fatal(err)
	}
	if textResult["encoding"] != "utf-8" || textResult["content"] != "guide" {
		t.Fatalf("text result = %#v", textResult)
	}
	binaryResult, err := toolset.loadSkillResourceToolHandler(nil, loadSkillResourceArgs{SkillName: "lazy-skill", Path: "assets/template.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if binaryResult["encoding"] != "base64" || binaryResult["content"] != base64.StdEncoding.EncodeToString(binary) {
		t.Fatalf("binary result = %#v", binaryResult)
	}
}

func TestLoadSkillResourceToolStableErrors(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "lazy-skill")
	writeLazySkillFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: lazy-skill\ndescription: lazy resources\n---\ninstructions\n")
	if err := os.MkdirAll(filepath.Join(skillDir, "assets", "directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	largePath := filepath.Join(skillDir, "assets", "large.bin")
	writeLazySkillFile(t, largePath, "x")
	if err := os.Truncate(largePath, MAX_SKILL_PAYLOAD_BYTES+1); err != nil {
		t.Fatal(err)
	}
	discovered, err := skills.DiscoverSkillsFromDir(root)
	if err != nil {
		t.Fatal(err)
	}
	toolset, err := NewSkillToolset(discovered, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path string
		code string
	}{
		{path: "../outside", code: "INVALID_RESOURCE_PATH"},
		{path: "assets/missing", code: "RESOURCE_NOT_FOUND"},
		{path: "assets/directory", code: "INVALID_RESOURCE_TYPE"},
		{path: "assets/large.bin", code: "RESOURCE_TOO_LARGE"},
	} {
		result, callErr := toolset.loadSkillResourceToolHandler(nil, loadSkillResourceArgs{SkillName: "lazy-skill", Path: test.path})
		if callErr != nil {
			t.Fatalf("path %q call error = %v", test.path, callErr)
		}
		if result["error_code"] != test.code {
			t.Errorf("path %q error code = %v, want %s", test.path, result["error_code"], test.code)
		}
		if strings.Contains(result["error"].(string), root) {
			t.Errorf("path %q exposed root in error %q", test.path, result["error"])
		}
	}

	canceled := resourceReadError(loadSkillResourceArgs{SkillName: "lazy-skill", Path: "assets/data"}, context.Canceled)
	if canceled["error_code"] != "RESOURCE_READ_CANCELED" {
		t.Fatalf("canceled error = %#v", canceled)
	}
}

func TestNewSkillToolsetRejectsNilAndDuplicateSkills(t *testing.T) {
	if _, err := NewSkillToolset([]*skills.Skill{nil}, nil); err == nil {
		t.Fatal("NewSkillToolset(nil skill) error = nil")
	}
	first := &skills.Skill{Frontmatter: &skills.Frontmatter{Name: "duplicate", Description: "first"}}
	second := &skills.Skill{Frontmatter: &skills.Frontmatter{Name: "duplicate", Description: "second"}}
	if _, err := NewSkillToolset([]*skills.Skill{first, second}, nil); err == nil {
		t.Fatal("NewSkillToolset(duplicate skills) error = nil")
	}
}

func TestNewReadOnlySkillToolsetOmitsScriptExecution(t *testing.T) {
	root := t.TempDir()
	writeLazySkillFile(t, filepath.Join(root, "lazy-skill", "SKILL.md"), "---\nname: lazy-skill\ndescription: lazy resources\n---\ninstructions\n")
	discovered, err := skills.DiscoverSkillsFromDir(root)
	if err != nil {
		t.Fatal(err)
	}
	toolset, err := NewReadOnlySkillToolset(discovered)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := toolset.Tools(nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, skillTool := range tools {
		names = append(names, skillTool.Name())
	}
	if got, want := strings.Join(names, ","), "list_skills,load_skill,load_skill_resource"; got != want {
		t.Fatalf("read-only tools = %q, want %q", got, want)
	}
	if strings.Contains(toolset.instruction, "run_skill_script") {
		t.Fatal("read-only instruction advertises run_skill_script")
	}
}

func writeLazySkillFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
