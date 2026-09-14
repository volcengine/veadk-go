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
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var ErrSkillCacheLimit = errors.New("skill cache limit exceeded")

// SkillCacheConfig requires a private, application-owned directory. The cache
// must not be shared with untrusted writers or another SkillCache/process.
type SkillCacheConfig struct {
	Directory        string
	MaxArchiveBytes  int64
	MaxExpandedBytes int64
	MaxFiles         int
	MaxCacheBytes    int64
}

// SkillCache publishes validated immutable directories. Old revisions are kept
// until explicit Prune so existing snapshots keep working. A context-aware gate
// serializes materialization and disk accounting, including duplicate downloads.
type SkillCache struct {
	config SkillCacheConfig
	gate   chan struct{}
	mu     sync.Mutex
	pins   map[string]int
}

func NewSkillCache(c SkillCacheConfig) (*SkillCache, error) {
	if c.Directory == "" {
		return nil, errors.New("skill cache directory required")
	}
	if c.MaxArchiveBytes == 0 {
		c.MaxArchiveBytes = 64 << 20
	}
	if c.MaxExpandedBytes == 0 {
		c.MaxExpandedBytes = 256 << 20
	}
	if c.MaxFiles == 0 {
		c.MaxFiles = 10000
	}
	if c.MaxCacheBytes == 0 {
		c.MaxCacheBytes = 1 << 30
	}
	if c.MaxArchiveBytes < 1 || c.MaxArchiveBytes > 1<<30 || c.MaxExpandedBytes < 1 || c.MaxExpandedBytes > 4<<30 || c.MaxFiles < 1 || c.MaxFiles > 100000 || c.MaxCacheBytes < 1 {
		return nil, errors.New("invalid skill cache limits")
	}
	abs, err := filepath.Abs(c.Directory)
	if err != nil {
		return nil, err
	}
	c.Directory = abs
	return &SkillCache{config: c, gate: make(chan struct{}, 1), pins: map[string]int{}}, nil
}
func (c *SkillCache) lock(ctx context.Context) error {
	select {
	case c.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			c.unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *SkillCache) unlock() { <-c.gate }
func cacheKey(source string, d RemoteSkill) string {
	data, _ := json.Marshal(struct {
		Source string
		Skill  RemoteSkill
	}{source, d})
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
func (c *SkillCache) pin(key string) { c.mu.Lock(); defer c.mu.Unlock(); c.pins[key]++ }
func (c *SkillCache) unpin(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pins[key]--
	if c.pins[key] == 0 {
		delete(c.pins, key)
	}
}

// Materialize downloads on cache miss, validates every cached file against its
// manifest and repairs damaged revisions. Returned resources are loaded lazily.
func (c *SkillCache) Materialize(ctx context.Context, source SkillSource, d RemoteSkill) (*Skill, error) {
	if source == nil {
		return nil, errors.New("skill source required")
	}
	if err := validateRemote(d); err != nil {
		return nil, err
	}
	if err := c.lock(ctx); err != nil {
		return nil, err
	}
	defer c.unlock()
	if err := os.MkdirAll(c.config.Directory, 0700); err != nil {
		return nil, errors.New("cannot create skill cache")
	}
	info, err := os.Lstat(c.config.Directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("invalid skill cache directory")
	}
	key := cacheKey(source.Identity(), d)
	final := filepath.Join(c.config.Directory, key)
	if sk, err := c.loadCached(ctx, final, d); err == nil {
		return sk, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(final); err != nil {
		return nil, errors.New("cannot remove damaged cache")
	}
	used, err := c.diskUsage()
	if err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(c.config.Directory, ".stage-")
	if err != nil {
		return nil, errors.New("cannot stage skill")
	}
	defer func() { _ = os.RemoveAll(stage) }()
	archive, err := os.OpenFile(filepath.Join(stage, "archive.zip"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer func() { _ = archive.Close() }()
	available := c.config.MaxCacheBytes - used
	if available <= 0 {
		return nil, ErrSkillCacheLimit
	}
	writer := &boundedWriter{w: archive, left: min(c.config.MaxArchiveBytes, available)}
	if err := source.Download(ctx, d, writer); err != nil {
		if writer.exceeded {
			return nil, ErrSkillCacheLimit
		}
		return nil, safeRemoteError(ctx, "skill archive download failed")
	}
	if writer.exceeded {
		return nil, ErrSkillCacheLimit
	}
	stat, err := archive.Stat()
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(archive, stat.Size())
	if err != nil {
		return nil, errors.New("invalid skill zip archive")
	}
	root := filepath.Join(stage, "content")
	if err := os.Mkdir(root, 0700); err != nil {
		return nil, err
	}
	limit := min(c.config.MaxExpandedBytes, available-stat.Size())
	if limit <= 0 {
		return nil, ErrSkillCacheLimit
	}
	if err := c.extract(ctx, zr, root, limit); err != nil {
		return nil, err
	}
	skillDir, err := findRemoteRoot(root)
	if err != nil {
		return nil, err
	}
	sk, err := parseSkillMD(skillDir)
	if err != nil || sk.Name() != d.Name {
		return nil, errors.New("invalid remote SKILL.md or name mismatch")
	}
	manifest, err := c.manifest(ctx, root)
	if err != nil {
		return nil, err
	}
	manifestBytes, _ := json.Marshal(manifest)
	if int64(len(manifestBytes))+used+stat.Size()+manifest.Size > c.config.MaxCacheBytes {
		return nil, ErrSkillCacheLimit
	}
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), manifestBytes, 0600); err != nil {
		return nil, err
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	if err := os.Remove(filepath.Join(stage, "archive.zip")); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.Rename(stage, final); err != nil {
		return nil, errors.New("cannot publish skill cache")
	}
	return parseSkillMD(filepath.Join(final, "content", relativeRoot(root, skillDir)))
}

func relativeRoot(root, dir string) string { rel, _ := filepath.Rel(root, dir); return rel }

type boundedWriter struct {
	w        io.Writer
	left     int64
	exceeded bool
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.left {
		w.exceeded = true
		return 0, ErrSkillCacheLimit
	}
	n, err := w.w.Write(p)
	w.left -= int64(n)
	return n, err
}

func (c *SkillCache) extract(ctx context.Context, zr *zip.Reader, root string, limit int64) error {
	if len(zr.File) > c.config.MaxFiles {
		return ErrSkillCacheLimit
	}
	names := map[string]bool{}
	var total uint64
	for _, f := range zr.File {
		name := strings.TrimSuffix(f.Name, "/")
		if name == "" || !fs.ValidPath(name) || strings.ContainsAny(name, "\\:\x00") || len(name) > 4096 || (!f.Mode().IsRegular() && !f.Mode().IsDir()) {
			return errors.New("unsafe skill archive entry")
		}
		folded := strings.ToLower(name)
		if names[folded] {
			return errors.New("duplicate skill archive entry")
		}
		names[folded] = true
		if f.UncompressedSize64 > uint64(limit)-total {
			return ErrSkillCacheLimit
		}
		total += f.UncompressedSize64
		if total > uint64(limit) {
			return ErrSkillCacheLimit
		}
	}
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0700); err != nil {
				return errors.New("invalid archive layout")
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return errors.New("invalid archive layout")
		}
		input, err := f.Open()
		if err != nil {
			return errors.New("invalid archive member")
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			_ = input.Close()
			return errors.New("invalid archive layout")
		}
		writer := &boundedWriter{w: out, left: limit}
		n, copyErr := io.Copy(writer, &contextReader{ctx: ctx, r: input})
		closeErr := out.Close()
		_ = input.Close()
		if copyErr != nil || closeErr != nil || uint64(n) != f.UncompressedSize64 {
			return safeRemoteError(ctx, "invalid or oversized archive member")
		}
		limit -= n
	}
	return nil
}

// findRemoteRoot matches Python #1084: root first, then minimum depth and
// lexical path. Legacy lowercase skill.md remains accepted.
func findRemoteRoot(root string) (string, error) {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		if info, err := os.Lstat(filepath.Join(root, name)); err == nil && info.Mode().IsRegular() {
			return root, nil
		}
	}
	candidates := []string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("unsafe archive symlink")
		}
		if !d.IsDir() && (d.Name() == "SKILL.md" || d.Name() == "skill.md") {
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return errors.New("invalid skill document")
			}
			rel, _ := filepath.Rel(root, filepath.Dir(p))
			candidates = append(candidates, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", errors.New("archive has no SKILL.md")
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := strings.Count(candidates[i], "/"), strings.Count(candidates[j], "/")
		if a != b {
			return a < b
		}
		return candidates[i] < candidates[j]
	})
	return filepath.Join(root, filepath.FromSlash(candidates[0])), nil
}

type cacheManifest struct {
	Files map[string]string
	Size  int64
}

func (c *SkillCache) manifest(ctx context.Context, root string) (cacheManifest, error) {
	m := cacheManifest{Files: map[string]string{}}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("cache symlink")
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("invalid cache file")
		}
		m.Size += info.Size()
		if m.Size > c.config.MaxExpandedBytes || len(m.Files) >= c.config.MaxFiles {
			return ErrSkillCacheLimit
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, err = io.Copy(h, &contextReader{ctx: ctx, r: io.LimitReader(f, c.config.MaxExpandedBytes+1)})
		_ = f.Close()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		m.Files[filepath.ToSlash(rel)] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	return m, err
}
func (c *SkillCache) loadCached(ctx context.Context, dir string, d RemoteSkill) (*Skill, error) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("cache missing")
	}
	manifestInfo, err := os.Lstat(filepath.Join(dir, "manifest.json"))
	if err != nil || !manifestInfo.Mode().IsRegular() {
		return nil, errors.New("invalid cache manifest file")
	}
	f, err := os.Open(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	data, err := readBoundedContext(ctx, f, int64(c.config.MaxFiles)*4300+1024, ErrSkillCacheLimit)
	_ = f.Close()
	if err != nil {
		return nil, err
	}
	var expected cacheManifest
	if json.Unmarshal(data, &expected) != nil {
		return nil, errors.New("invalid cache manifest")
	}
	actual, err := c.manifest(ctx, filepath.Join(dir, "content"))
	if err != nil {
		return nil, err
	}
	if expected.Size != actual.Size || len(expected.Files) != len(actual.Files) {
		return nil, errors.New("damaged cache")
	}
	for p, h := range actual.Files {
		if expected.Files[p] != h {
			return nil, errors.New("damaged cache")
		}
	}
	root, err := findRemoteRoot(filepath.Join(dir, "content"))
	if err != nil {
		return nil, err
	}
	sk, err := parseSkillMD(root)
	if err != nil || sk.Name() != d.Name {
		return nil, errors.New("invalid cached skill")
	}
	return sk, nil
}
func (c *SkillCache) diskUsage() (int64, error) {
	var size int64
	err := filepath.WalkDir(c.config.Directory, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("cache contains symlink")
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return errors.New("cache contains special file")
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// Prune removes revisions not pinned by live snapshots and abandoned staging
// directories. Close retired snapshots before pruning. It never evicts live data.
func (c *SkillCache) Prune(ctx context.Context) error {
	if err := c.lock(ctx); err != nil {
		return err
	}
	defer c.unlock()
	entries, err := os.ReadDir(c.config.Directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		if c.pins[name] > 0 {
			continue
		}
		_, hashErr := hex.DecodeString(name)
		if (len(name) == 64 && hashErr == nil) || strings.HasPrefix(name, ".stage-") {
			if err := os.RemoveAll(filepath.Join(c.config.Directory, name)); err != nil {
				return err
			}
		}
	}
	return nil
}
