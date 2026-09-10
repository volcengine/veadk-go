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

package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDiscoverSkillsFromDirIsMetadataFirstStableAndRooted(t *testing.T) {
	realRoot := t.TempDir()
	writeTestSkill(t, realRoot, "zeta-skill", "last", "Use zeta.")
	writeTestSkill(t, realRoot, "alpha-skill", "first", "Use alpha.")
	writeTestFile(t, filepath.Join(realRoot, "alpha-skill", "assets", "nested", "template.txt"), "not loaded at discovery")
	writeTestFile(t, filepath.Join(realRoot, "README.md"), "not a skill")
	writeTestSkill(t, filepath.Join(realRoot, "container"), "nested-skill", "nested", "not immediate")

	linkedRoot := filepath.Join(t.TempDir(), "skills-link")
	if err := os.Symlink(realRoot, linkedRoot); err != nil {
		t.Fatal(err)
	}
	discovered, err := DiscoverSkillsFromDir(linkedRoot)
	if err != nil {
		t.Fatalf("DiscoverSkillsFromDir() error = %v", err)
	}
	if got, want := skillNames(discovered), []string{"alpha-skill", "zeta-skill"}; !slices.Equal(got, want) {
		t.Fatalf("skill names = %v, want %v", got, want)
	}
	for _, skill := range discovered {
		if skill.Resources != nil {
			t.Fatalf("skill %q loaded resources eagerly", skill.Name())
		}
		if !strings.HasPrefix(skill.SkillMDPath, linkedRoot) {
			t.Fatalf("SkillMDPath = %q, want path below supplied root %q", skill.SkillMDPath, linkedRoot)
		}
	}
	if got, want := discovered[0].Instructions, "Use alpha."; got != want {
		t.Fatalf("Instructions = %q, want %q", got, want)
	}
}

func TestDiscoverSkillsFromDirSkipAndStrictDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "valid-skill", "valid", "instructions")
	writeTestFile(t, filepath.Join(root, "bad-yaml", "SKILL.md"), "---\nname: [\n---\n")
	writeTestFile(t, filepath.Join(root, "mismatch", "SKILL.md"), "---\nname: another-name\ndescription: mismatch\n---\n")
	writeTestFile(t, filepath.Join(root, "lowercase-only", "skill.md"), "---\nname: lowercase-only\ndescription: legacy filename\n---\n")
	writeTestFile(t, filepath.Join(root, "oversized", "SKILL.md"), "---\nname: oversized\ndescription: "+strings.Repeat("x", 256)+"\n---\n")

	var skipped []string
	discovered, err := DiscoverSkillsFromDirWithOptions(t.Context(), root, DiscoveryOptions{
		MaxSkillDocumentBytes: 128,
		OnIssue: func(issue DiscoveryIssue) {
			skipped = append(skipped, issue.Entry)
		},
	})
	if err != nil {
		t.Fatalf("skip discovery error = %v", err)
	}
	if got, want := skillNames(discovered), []string{"valid-skill"}; !slices.Equal(got, want) {
		t.Fatalf("skill names = %v, want %v", got, want)
	}
	if got, want := skipped, []string{"bad-yaml", "lowercase-only", "mismatch", "oversized"}; !slices.Equal(got, want) {
		t.Fatalf("issues = %v, want %v", got, want)
	}

	var strictIssues []DiscoveryIssue
	partial, err := DiscoverSkillsFromDirWithOptions(t.Context(), root, DiscoveryOptions{
		Mode:                  DiscoveryStrict,
		MaxSkillDocumentBytes: 128,
		OnIssue: func(issue DiscoveryIssue) {
			strictIssues = append(strictIssues, issue)
		},
	})
	if err == nil {
		t.Fatal("strict discovery error = nil")
	}
	if got, want := skillNames(partial), []string{"valid-skill"}; !slices.Equal(got, want) {
		t.Fatalf("partial skill names = %v, want %v", got, want)
	}
	if len(strictIssues) != 4 {
		t.Fatalf("strict issue count = %d, want 4", len(strictIssues))
	}
	var issue *DiscoveryIssue
	if !errors.As(err, &issue) {
		t.Fatalf("strict error %T does not contain DiscoveryIssue", err)
	}
	if !errors.Is(err, ErrSkillDocumentTooLarge) {
		t.Fatalf("strict error = %v, want ErrSkillDocumentTooLarge", err)
	}
}

func TestDiscoverSkillsFromDirContextAndOptions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DiscoverSkillsFromDirWithOptions(ctx, t.TempDir(), DiscoveryOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled discovery error = %v, want context.Canceled", err)
	}
	if _, err := DiscoverSkillsFromDirWithOptions(t.Context(), t.TempDir(), DiscoveryOptions{Mode: DiscoveryMode(99)}); !errors.Is(err, ErrInvalidDiscoveryMode) {
		t.Fatalf("invalid mode error = %v, want ErrInvalidDiscoveryMode", err)
	}
	if _, err := DiscoverSkillsFromDirWithOptions(t.Context(), t.TempDir(), DiscoveryOptions{MaxSkillDocumentBytes: -1}); !errors.Is(err, ErrSkillDocumentTooLarge) {
		t.Fatalf("negative limit error = %v, want ErrSkillDocumentTooLarge", err)
	}
}

func TestLoadSkillFromDirKeepsLegacyLowercaseFilenameCompatibility(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "legacy-skill")
	writeTestFile(t, filepath.Join(dir, "skill.md"), "---\nname: legacy-skill\ndescription: legacy\n---\ninstructions\n")
	skill, err := LoadSkillFromDir(dir)
	if err != nil {
		t.Fatalf("LoadSkillFromDir() error = %v", err)
	}
	if got, want := filepath.Base(skill.SkillMDPath), "skill.md"; got != want {
		t.Fatalf("SkillMDPath base = %q, want %q", got, want)
	}
}

func TestParseSkillDocumentSupportsExtendedFrontmatter(t *testing.T) {
	document := `---
name: extended-skill
version: 1.3.0
description: |
  这是一个用于验证扩展 frontmatter 的多行中文描述。
  它包含较长文本、英文 token、尖括号 <tag>、路径 references/example.md 和若干标点。
  解析器应完整保留该描述，并按 Unicode 字符数执行长度校验。
allowed-tools:
  - ToolA
  - ToolB
  - ToolC
triggers:
  - extended-skill
  - 示例触发词
  - example trigger
metadata:
  license: Apache-2.0
  requires:
    bins:
      - example-bin
    optional:
      - optional-bin
  runtimes:
    - runtime-a
    - runtime-b
---
# Extended Skill
`

	skill, err := parseSkillDocument([]byte(document))
	if err != nil {
		t.Fatalf("parseSkillDocument() error = %v", err)
	}
	if got, want := skill.Frontmatter.Version, "1.3.0"; got != want {
		t.Fatalf("Version = %q, want %q", got, want)
	}
	if got, want := skill.Frontmatter.AllowedTools, "ToolA ToolB ToolC"; got != want {
		t.Fatalf("AllowedTools = %q, want %q", got, want)
	}
	if got, want := skill.Frontmatter.AllowedToolsList, []string{"ToolA", "ToolB", "ToolC"}; !slices.Equal(got, want) {
		t.Fatalf("AllowedToolsList = %v, want %v", got, want)
	}
	if got, want := skill.Frontmatter.Triggers, []string{"extended-skill", "示例触发词", "example trigger"}; !slices.Equal(got, want) {
		t.Fatalf("Triggers = %v, want %v", got, want)
	}
	requires, ok := skill.Frontmatter.Metadata["requires"].(map[string]any)
	if !ok {
		t.Fatalf("metadata.requires = %#v, want map[string]any", skill.Frontmatter.Metadata["requires"])
	}
	bins, ok := requires["bins"].([]any)
	if !ok || len(bins) != 1 || bins[0] != "example-bin" {
		t.Fatalf("metadata.requires.bins = %#v, want [example-bin]", requires["bins"])
	}
}

func TestDiscoverSkillsFromDirReportsUnreadableSkill(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read files regardless of permission bits")
	}
	root := t.TempDir()
	path := filepath.Join(root, "unreadable", "SKILL.md")
	writeTestFile(t, path, "---\nname: unreadable\ndescription: unreadable\n---\n")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	var issues []DiscoveryIssue
	discovered, err := DiscoverSkillsFromDirWithOptions(t.Context(), root, DiscoveryOptions{
		OnIssue: func(issue DiscoveryIssue) { issues = append(issues, issue) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != 0 || len(issues) != 1 || issues[0].Entry != "unreadable" {
		t.Fatalf("discovered=%v issues=%v, want one unreadable issue", skillNames(discovered), issues)
	}
}

func skillNames(skillList []*Skill) []string {
	names := make([]string, 0, len(skillList))
	for _, skill := range skillList {
		names = append(names, skill.Name())
	}
	return names
}

func writeTestSkill(t *testing.T, root, name, description, instructions string) {
	t.Helper()
	writeTestFile(t, filepath.Join(root, name, "SKILL.md"), fmtSkillDocument(name, description, instructions))
}

func fmtSkillDocument(name, description, instructions string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n" + instructions + "\n"
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
