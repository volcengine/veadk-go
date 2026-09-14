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
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/volcengine/veadk-go/skills"
)

type toolSource struct {
	calls   int
	archive []byte
}

func (s *toolSource) Identity() string { return "tool-fixture" }
func (s *toolSource) Discover(context.Context) ([]skills.RemoteSkill, error) {
	return []skills.RemoteSkill{{Name: "sample", Description: "Tool fixture", Version: "v1"}}, nil
}
func (s *toolSource) Download(_ context.Context, _ skills.RemoteSkill, w io.Writer) error {
	s.calls++
	_, err := w.Write(s.archive)
	return err
}
func TestRegistryToolsetLoadsPinnedResources(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"SKILL.md": "---\nname: sample\ndescription: Tool fixture\n---\nInstructions", "references/data.txt": "resource-content"} {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	source := &toolSource{archive: buf.Bytes()}
	cache, err := skills.NewSkillCache(skills.SkillCacheConfig{Directory: t.TempDir()})
	require.NoError(t, err)
	registry, err := skills.NewRegistry(skills.RegistryConfig{Sources: []skills.SourceBinding{{Source: source}}, Cache: cache})
	require.NoError(t, err)
	defer func() { _ = registry.Close() }()
	_, err = registry.Refresh(context.Background())
	require.NoError(t, err)
	snapshot := registry.Snapshot()
	defer func() { _ = snapshot.Close() }()
	st, err := NewRegistrySkillToolset(snapshot)
	require.NoError(t, err)
	require.Zero(t, source.calls)
	tools, err := st.Tools(nil)
	require.NoError(t, err)
	require.Len(t, tools, 3)
	result, err := st.load(nil, loadSkillArgs{Name: "sample"})
	require.NoError(t, err)
	require.Equal(t, "Instructions", result["instructions"])
	result, err = st.resource(nil, loadSkillResourceArgs{SkillName: "sample", Path: "references/data.txt"})
	require.NoError(t, err)
	require.Equal(t, "resource-content", result["content"])
	require.Equal(t, 1, source.calls)
	result, err = st.resource(nil, loadSkillResourceArgs{SkillName: "sample", Path: "../escape"})
	require.NoError(t, err)
	require.Equal(t, "INVALID_RESOURCE_PATH", result["error_code"])
	result, err = st.load(nil, loadSkillArgs{Name: "missing"})
	require.NoError(t, err)
	require.Equal(t, "SKILL_LOAD_FAILED", result["error_code"])
}
