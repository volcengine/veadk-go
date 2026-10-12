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
	"fmt"
	"strings"

	"github.com/volcengine/veadk-go/v2/skills"
	"github.com/volcengine/veadk-go/v2/tool/skilltool"
)

func configureLocalSkills(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("llmagent config is required")
	}

	discovered := make([]*skills.Skill, 0)
	sources := make(map[string]string)
	for _, root := range cfg.Skills {
		if strings.TrimSpace(root) == "" {
			continue
		}
		rootSkills, err := skills.DiscoverSkillsFromDirWithOptions(
			context.Background(),
			root,
			cfg.SkillDiscoveryOptions,
		)
		if err != nil {
			return fmt.Errorf("discover local skills from %q: %w", root, err)
		}
		for _, skill := range rootSkills {
			if previousRoot, ok := sources[skill.Name()]; ok {
				return fmt.Errorf("%w %q from %q and %q", skills.ErrDuplicateSkill, skill.Name(), previousRoot, root)
			}
			sources[skill.Name()] = root
			discovered = append(discovered, skill)
		}
	}
	if len(discovered) == 0 {
		return nil
	}

	toolset, err := skilltool.NewReadOnlySkillToolset(discovered)
	if err != nil {
		return fmt.Errorf("create local skill toolset: %w", err)
	}
	cfg.Toolsets = append(cfg.Toolsets, toolset)
	return nil
}
