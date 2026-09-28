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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/volcengine/veadk-go/v2/log"
	"gopkg.in/yaml.v3"
)

// loadDir Recursively load files from a directory into a dictionary.
// Args:
//
//	directory: Path to the directory to load.
//
// Returns:
//
//	Dictionary mapping relative file paths to their string content.
func loadDir(directory string) (map[string]string, error) {
	files := make(map[string]string)
	info, err := os.Stat(directory)
	if err != nil {
		return files, nil
	}
	if !info.IsDir() {
		return files, nil
	}
	err = filepath.WalkDir(directory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		for _, part := range strings.Split(path, string(filepath.Separator)) {
			if part == "__pycache__" {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if !utf8.Valid(b) {
			return nil
		}
		rel, err := filepath.Rel(directory, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		files[rel] = string(b)
		return nil
	})
	if err != nil {
		return files, err
	}
	return files, nil
}

func parseFrontmatter(text []byte) (*Frontmatter, error) {
	var skillMeta Frontmatter

	if err := yaml.Unmarshal(text, &skillMeta); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	if skillMeta.Name == "" || skillMeta.Description == "" {
		return nil, fmt.Errorf("frontmatter is missing name or description")
	}

	log.Debugf("Successfully loaded skill frontmatter, name=%s", skillMeta.Name)
	return &skillMeta, nil
}

func LoadSkillFromDir(skillDir string) (*Skill, error) {
	var skill *Skill
	var err error
	abs, err := filepath.Abs(skillDir)
	if err != nil {
		return skill, err
	}
	skill, err = parseSkillMD(abs)
	if err != nil {
		return skill, fmt.Errorf("failed to parse skill directory '%s' error:%w", skillDir, err)
	}

	if base := filepath.Base(abs); base != skill.Name() {
		return skill, fmt.Errorf("skill name '%s' does not match directory name '%s'", skill.Name(), base)
	}
	refs, err := loadDir(filepath.Join(abs, "references"))
	if err != nil {
		return skill, fmt.Errorf("failed to load references dir '%s' error:%w", abs, err)
	}
	assets, err := loadDir(filepath.Join(abs, "assets"))
	if err != nil {
		return skill, fmt.Errorf("failed to load assets dir '%s' error:%w", abs, err)
	}
	rawScripts, err := loadDir(filepath.Join(abs, "scripts"))
	if err != nil {
		return skill, fmt.Errorf("failed to load scripts dir '%s' error:%w", abs, err)
	}
	scripts := make(map[string]*Script, len(rawScripts))
	for k, v := range rawScripts {
		scripts[k] = &Script{Src: v}
	}
	skill.Resources = &Resources{
		References: refs,
		Assets:     assets,
		Scripts:    scripts,
	}

	return skill, nil
}

func ReadSkillProperties(skillDir string) (*Frontmatter, error) {
	abs, err := filepath.Abs(skillDir)
	if err != nil {
		return nil, err
	}
	skill, err := parseSkillMD(abs)
	if err != nil {
		return nil, err
	}
	return skill.Frontmatter, nil
}
