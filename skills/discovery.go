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
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/volcengine/veadk-go/log"
)

const DefaultMaxSkillDocumentBytes int64 = 1024 * 1024

var (
	ErrInvalidDiscoveryMode  = errors.New("invalid skill discovery mode")
	ErrSkillDocumentTooLarge = errors.New("skill document is too large")
	ErrDuplicateSkill        = errors.New("duplicate skill name")
)

// DiscoveryMode controls whether malformed immediate child directories are
// skipped or returned to the caller as an aggregate error.
type DiscoveryMode uint8

const (
	DiscoverySkipInvalid DiscoveryMode = iota
	DiscoveryStrict
)

// DiscoveryIssue describes one child directory which could not be loaded.
// Entry contains only the child name, not the contents of its SKILL.md.
type DiscoveryIssue struct {
	Entry string
	Err   error
}

func (e *DiscoveryIssue) Error() string {
	if e == nil {
		return "skill discovery failed"
	}
	return fmt.Sprintf("discover skill %q: %v", e.Entry, e.Err)
}

func (e *DiscoveryIssue) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// DiscoveryOptions configures local metadata-first discovery.
type DiscoveryOptions struct {
	Mode                  DiscoveryMode
	MaxSkillDocumentBytes int64
	OnIssue               func(DiscoveryIssue)
}

// DiscoverSkillsFromDir discovers valid skills in immediate child
// directories. Invalid children are skipped and logged. Resources are not
// traversed or loaded.
func DiscoverSkillsFromDir(skillsDir string) ([]*Skill, error) {
	return DiscoverSkillsFromDirWithOptions(context.Background(), skillsDir, DiscoveryOptions{})
}

// DiscoverSkillsFromDirWithOptions discovers valid skills in immediate child
// directories in stable name order. In strict mode it returns all valid skills
// together with an aggregate error for invalid children.
func DiscoverSkillsFromDirWithOptions(ctx context.Context, skillsDir string, options DiscoveryOptions) ([]*Skill, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Mode != DiscoverySkipInvalid && options.Mode != DiscoveryStrict {
		return nil, ErrInvalidDiscoveryMode
	}
	maxBytes := options.MaxSkillDocumentBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxSkillDocumentBytes
	}
	if maxBytes < 0 {
		return nil, fmt.Errorf("%w: maximum SKILL.md size must not be negative", ErrSkillDocumentTooLarge)
	}

	abs, err := filepath.Abs(skillsDir)
	if err != nil {
		return nil, fmt.Errorf("resolve skills directory: %w", err)
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("open skills directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("read skills directory: %w", err)
	}

	discovered := make([]*Skill, 0, len(entries))
	seen := make(map[string]string, len(entries))
	issues := make([]error, 0)
	report := func(entry string, issueErr error) {
		issue := DiscoveryIssue{Entry: entry, Err: issueErr}
		if options.OnIssue != nil {
			options.OnIssue(issue)
		}
		log.Warnf("Skipping invalid skill directory %q: %v", entry, issueErr)
		if options.Mode == DiscoveryStrict {
			issues = append(issues, &issue)
		}
	}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return discovered, err
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}

		childRoot, openErr := root.OpenRoot(entry.Name())
		if openErr != nil {
			report(entry.Name(), fmt.Errorf("open directory: %w", openErr))
			continue
		}
		skill, loadErr := parseSkillRoot(ctx, childRoot, filepath.Join(abs, entry.Name()), maxBytes, false)
		closeErr := childRoot.Close()
		if loadErr == nil && closeErr != nil {
			loadErr = fmt.Errorf("close directory: %w", closeErr)
		}
		if loadErr != nil {
			report(entry.Name(), loadErr)
			continue
		}
		if skill.Name() != entry.Name() {
			report(entry.Name(), fmt.Errorf("declared name %q does not match directory name", skill.Name()))
			continue
		}
		if previous, ok := seen[skill.Name()]; ok {
			report(entry.Name(), fmt.Errorf("%w %q (already provided by %q)", ErrDuplicateSkill, skill.Name(), previous))
			continue
		}
		seen[skill.Name()] = entry.Name()
		discovered = append(discovered, skill)
	}

	if len(issues) != 0 {
		return discovered, errors.Join(issues...)
	}
	return discovered, nil
}

func parseSkillMD(skillDir string) (*Skill, error) {
	abs, err := filepath.Abs(skillDir)
	if err != nil {
		return nil, fmt.Errorf("resolve skill directory: %w", err)
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("open skill directory: %w", err)
	}
	defer func() { _ = root.Close() }()
	return parseSkillRoot(context.Background(), root, abs, DefaultMaxSkillDocumentBytes, true)
}

func parseSkillRoot(ctx context.Context, root *os.Root, skillDir string, maxBytes int64, allowLegacyFilename bool) (*Skill, error) {
	var (
		file     *os.File
		fileName string
	)
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, fmt.Errorf("read skill directory: %w", err)
	}
	names := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		names[entry.Name()] = struct{}{}
	}
	candidates := []string{"SKILL.md"}
	if allowLegacyFilename {
		candidates = append(candidates, "skill.md")
	}
	for _, candidate := range candidates {
		if _, ok := names[candidate]; !ok {
			continue
		}
		opened, err := root.Open(candidate)
		if err == nil {
			file = opened
			fileName = candidate
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("open %s: %w", candidate, err)
		}
	}
	if file == nil {
		return nil, fmt.Errorf("SKILL.md not found")
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat SKILL.md: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("SKILL.md is not a regular file")
	}
	data, err := readBoundedContext(ctx, file, maxBytes, ErrSkillDocumentTooLarge)
	if err != nil {
		return nil, fmt.Errorf("read SKILL.md: %w", err)
	}

	skill, err := parseSkillDocument(data)
	if err != nil {
		return nil, err
	}
	skill.SkillMDPath = filepath.Join(skillDir, fileName)
	log.Debugf("Successfully discovered local skill %q", skill.Name())
	return skill, nil
}

func parseSkillDocument(data []byte) (*Skill, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, fmt.Errorf("invalid SKILL.md frontmatter")
	}

	closing := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			closing = index
			break
		}
	}
	if closing < 0 {
		return nil, fmt.Errorf("missing closing SKILL.md frontmatter separator")
	}
	if closing == 1 {
		return nil, fmt.Errorf("empty SKILL.md frontmatter")
	}

	frontmatter, err := parseFrontmatter([]byte(strings.Join(lines[1:closing], "\n")))
	if err != nil {
		return nil, fmt.Errorf("parse SKILL.md frontmatter: %w", err)
	}
	if err := frontmatter.Validate(); err != nil {
		return nil, fmt.Errorf("validate SKILL.md frontmatter: %w", err)
	}

	return &Skill{
		Frontmatter:  frontmatter,
		Instructions: strings.Join(lines[closing+1:], "\n"),
	}, nil
}

func readBoundedContext(ctx context.Context, reader io.Reader, maxBytes int64, tooLarge error) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limited := reader
	if maxBytes < math.MaxInt64 {
		limited = io.LimitReader(reader, maxBytes+1)
	}
	buffer := make([]byte, 0, min(maxBytes, 32*1024))
	chunk := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		count, err := limited.Read(chunk)
		if count > 0 {
			buffer = append(buffer, chunk[:count]...)
			if int64(len(buffer)) > maxBytes {
				return nil, fmt.Errorf("%w: limit is %d bytes", tooLarge, maxBytes)
			}
		}
		if errors.Is(err, io.EOF) {
			return buffer, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
