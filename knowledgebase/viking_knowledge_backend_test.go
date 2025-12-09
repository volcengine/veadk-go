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

package knowledgebase

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/volcengine/veadk-go/common"
)

func writeFile(t *testing.T, dir string, rel string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func assertEqualPaths(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got=%d want=%d\ngot=%v\nwant=%v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("path mismatch at %d: got=%s want=%s\nall got=%v\nall want=%v", i, got[i], want[i], got, want)
		}
	}
}

func TestGetFilesInDirectory_NonExistent(t *testing.T) {
	base := t.TempDir()
	missing := filepath.Join(base, "missing")
	_, err := getFilesInDirectory(missing)
	if err == nil {
		t.Fatalf("expected error for non-existent directory")
	}
}

func TestGetFilesInDirectory_Empty(t *testing.T) {
	dir := t.TempDir()
	files, err := getFilesInDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqualPaths(t, files, []string{})
}

func TestGetFilesInDirectory_Flat(t *testing.T) {
	dir := t.TempDir()
	f1 := writeFile(t, dir, "a.txt")
	f2 := writeFile(t, dir, "b.md")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	files, err := getFilesInDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqualPaths(t, files, []string{f1, f2})
}

func TestGetFilesInDirectory_Nested(t *testing.T) {
	dir := t.TempDir()
	r1 := writeFile(t, dir, "root.txt")
	n1 := writeFile(t, dir, "sub/inner/deep.txt")
	n2 := writeFile(t, dir, "sub/inner/another.bin")
	n3 := writeFile(t, dir, "sub/file.log")
	files, err := getFilesInDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Log("files:", files)
	assertEqualPaths(t, files, []string{r1, n1, n2, n3})
}

func TestGetFilesInDirectory_SymlinkIgnored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink requires privileges on Windows")
	}
	dir := t.TempDir()
	target := writeFile(t, dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink not supported in environment")
	}
	files, err := getFilesInDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqualPaths(t, files, []string{target})
}

func newBackendOrSkip(t *testing.T, idx string) *VikingKnowledgeBackend {
	t.Helper()
	ak := os.Getenv(common.VOLCENGINE_ACCESS_KEY)
	sk := os.Getenv(common.VOLCENGINE_SECRET_KEY)
	if ak == "" || sk == "" {
		t.Skip("missing VOLCENGINE_ACCESS_KEY/VOLCENGINE_SECRET_KEY")
	}
	region := os.Getenv(common.DATABASE_VIKING_REGION)
	if region == "" {
		region = "cn-beijing"
	}
	bucket := "veadk-ut-20251208152204"

	cfg := &Config{
		AK:                  ak,
		SK:                  sk,
		Index:               idx,
		Project:             "default",
		Region:              region,
		CreateIfNotExist:    true,
		TopK:                5,
		ChunkDiffusionCount: 1,
		TOSBucket:           bucket,
	}
	kb, err := NewVikingKnowledgeBackend(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return kb.(*VikingKnowledgeBackend)
}

func TestVikingKnowledgeBackend_Search(t *testing.T) {
	idx := "sjy_test_coffee_kg"
	kb := newBackendOrSkip(t, idx)
	entries, err := kb.Search("拿铁")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("viking kg search result: ", entries)
	if len(entries) > 0 && entries[0].Content == "" {
		t.Fatalf("empty content")
	}
}

func TestVikingKnowledgeBackend_AddFromText(t *testing.T) {
	idx := fmt.Sprintf("veadk_kb_text_%d", time.Now().UnixNano())
	kb := newBackendOrSkip(t, idx)
	defer func() {
		_, err := kb.viking.CollectionDelete()
		if err != nil {
			t.Fatal("CollectionDelete ", idx, "failed:", err)
		}

	}()
	err := kb.AddFromText([]string{"hello", "world"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestVikingKnowledgeBackend_AddFromFiles(t *testing.T) {
	idx := fmt.Sprintf("veadk_kb_files_%d", time.Now().UnixNano())
	kb := newBackendOrSkip(t, idx)
	defer func() {
		_, err := kb.viking.CollectionDelete()
		if err != nil {
			t.Fatal("CollectionDelete ", idx, "failed:", err)
		}

	}()
	dir := t.TempDir()
	f1 := writeFile(t, dir, "a.txt")
	f2 := writeFile(t, dir, "b.md")
	err := kb.AddFromFiles([]string{f1, f2})
	if err != nil {
		t.Fatal(err)
	}
}

func TestVikingKnowledgeBackend_AddFromDirectory(t *testing.T) {
	idx := fmt.Sprintf("veadk_kb_dir_%d", time.Now().UnixNano())
	kb := newBackendOrSkip(t, idx)
	defer func() {
		_, err := kb.viking.CollectionDelete()
		if err != nil {
			t.Fatal("CollectionDelete ", idx, "failed:", err)
		}
	}()
	dir := "/Users/bytedance/Desktop/files"
	err := kb.AddFromDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
}
