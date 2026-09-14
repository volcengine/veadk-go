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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/volcengine/veadk-go/auth/veauth"
)

func fixtureZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, body := range files {
		w, err := z.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	return b.Bytes()
}
func skillArchive(t *testing.T, name, body string) []byte {
	return fixtureZip(t, map[string]string{"SKILL.md": "---\nname: " + name + "\ndescription: Test skill\n---\n" + body, "scripts/run.py": "print('ok')"})
}
func fakeCredential(counter *atomic.Int32) veauth.CredentialSource {
	return veauth.CredentialSourceFunc(func(ctx context.Context) (veauth.VeIAMCredential, error) {
		counter.Add(1)
		return veauth.VeIAMCredential{AccessKeyID: "test-ak", SecretAccessKey: "test-sk", SessionToken: "test-token"}, ctx.Err()
	})
}

type fixtureSource struct {
	mu          sync.Mutex
	id          string
	descriptors []RemoteSkill
	archives    map[string][]byte
	failure     bool
	downloads   int
}

func (f *fixtureSource) Identity() string { return f.id }
func (f *fixtureSource) Discover(ctx context.Context) ([]RemoteSkill, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failure {
		return nil, errors.New("secret source failure")
	}
	return append([]RemoteSkill(nil), f.descriptors...), ctx.Err()
}
func (f *fixtureSource) Download(ctx context.Context, d RemoteSkill, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloads++
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := w.Write(f.archives[d.Version])
	return err
}

func TestRemoteSourceProtocol(t *testing.T) {
	var creds, calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "test-token", r.Header.Get("X-Security-Token"))
		require.Contains(t, r.Header.Get("Authorization"), "test-ak/")
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "ss-test", body["SkillSpaceId"])
		require.Equal(t, "ListSkillsBySpaceId", r.URL.Query().Get("Action"))
		require.Equal(t, "2025-10-30", r.URL.Query().Get("Version"))
		_, _ = io.WriteString(w, `{"Result":{"Items":[{"Name":"sample","Description":"Test","SkillId":"id","BucketName":"bucket","TosPath":"skills/id/v1/archive.zip","SkillVersion":"v1"}]}}`)
	}))
	defer server.Close()
	source, err := NewRemoteSource(RemoteSourceConfig{SpaceID: "ss-test", Endpoint: server.URL, Credentials: fakeCredential(&creds)})
	require.NoError(t, err)
	require.Zero(t, creds.Load())
	require.Zero(t, calls.Load())
	list, err := source.Discover(t.Context())
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "sample", list[0].Name)
	require.Equal(t, "v1", list[0].Version)
}
func TestRemoteSourceRejectsSkillHub(t *testing.T) {
	var creds atomic.Int32
	source, err := NewRemoteSource(RemoteSourceConfig{SpaceID: "sp-test", Credentials: fakeCredential(&creds)})
	require.ErrorContains(t, err, "not supported")
	require.Nil(t, source)
	require.Zero(t, creds.Load())
}

func TestRemoteDiscoveryFailuresAndCancellation(t *testing.T) {
	for _, body := range []string{`{}`, `{"Items":null}`, `{"Items":[],"TotalCount":2}`, `{"Items":[{"Id":"x","Name":"../bad"}]}`, `{"ResponseMetadata":{"Error":{"Message":"secret"}}}`, `{"Items":[{"Id":"x","Name":"same"},{"Id":"y","Name":"same"}]}`, `{"Items":[],"TotalCount":-1}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
			defer server.Close()
			var count atomic.Int32
			s, err := NewRemoteSource(RemoteSourceConfig{SpaceID: "ss-test", Endpoint: server.URL, Credentials: fakeCredential(&count)})
			require.NoError(t, err)
			_, err = s.Discover(context.Background())
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret")
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
	defer server.Close()
	var count atomic.Int32
	s, err := NewRemoteSource(RemoteSourceConfig{SpaceID: "ss-test", Endpoint: server.URL, Credentials: fakeCredential(&count), Timeout: 20 * time.Millisecond})
	require.NoError(t, err)
	_, err = s.Discover(context.Background())
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRemoteCacheConcurrentRepairAndPrune(t *testing.T) {
	ctx := context.Background()
	d := RemoteSkill{Name: "sample", Description: "Test", Version: "v1"}
	source := &fixtureSource{id: "fixture", descriptors: []RemoteSkill{d}, archives: map[string][]byte{"v1": skillArchive(t, "sample", "one")}}
	cache, err := NewSkillCache(SkillCacheConfig{Directory: t.TempDir()})
	require.NoError(t, err)
	registry, err := NewRegistry(RegistryConfig{Sources: []SourceBinding{{Source: source}}, Cache: cache})
	require.NoError(t, err)
	defer func() { _ = registry.Close() }()
	_, err = registry.Refresh(ctx)
	require.NoError(t, err)
	snapshot := registry.Snapshot()
	defer func() { _ = snapshot.Close() }()
	require.Zero(t, source.downloads)
	require.Len(t, snapshot.List(), 1)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sk, e := snapshot.Get(ctx, "sample")
			require.NoError(t, e)
			if e == nil {
				require.Equal(t, "one", sk.Instructions)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, source.downloads)
	sk, err := snapshot.Get(ctx, "sample")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(sk.GetSkillPath(), "scripts/run.py"), []byte("damaged"), 0600))
	_, err = snapshot.Get(ctx, "sample")
	require.NoError(t, err)
	require.Equal(t, 2, source.downloads)
	require.NoError(t, cache.Prune(ctx))
	_, err = os.Stat(sk.SkillMDPath)
	require.NoError(t, err)
	require.NoError(t, snapshot.Close())
	require.NoError(t, registry.Close())
	require.NoError(t, cache.Prune(ctx))
	entries, err := os.ReadDir(cache.config.Directory)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestRegistryRefreshIsolationAndOptionalRecovery(t *testing.T) {
	ctx := context.Background()
	source := &fixtureSource{id: "fixture", descriptors: []RemoteSkill{{Name: "sample", Version: "v1"}}, archives: map[string][]byte{"v1": skillArchive(t, "sample", "one"), "v2": skillArchive(t, "sample", "two")}}
	cache, err := NewSkillCache(SkillCacheConfig{Directory: t.TempDir()})
	require.NoError(t, err)
	r, err := NewRegistry(RegistryConfig{Sources: []SourceBinding{{Source: source, Optional: true}}, Cache: cache})
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	source.failure = true
	issues, err := r.Refresh(ctx)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	source.failure = false
	_, err = r.Refresh(ctx)
	require.NoError(t, err)
	old := r.Snapshot()
	defer func() { _ = old.Close() }()
	source.descriptors[0].Version = "v2"
	_, err = r.Refresh(ctx)
	require.NoError(t, err)
	current := r.Snapshot()
	defer func() { _ = current.Close() }()
	a, err := old.Get(ctx, "sample")
	require.NoError(t, err)
	b, err := current.Get(ctx, "sample")
	require.NoError(t, err)
	require.Equal(t, "one", a.Instructions)
	require.Equal(t, "two", b.Instructions)
	source.failure = true
	issues, err = r.Refresh(ctx)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	fallback := r.Snapshot()
	defer func() { _ = fallback.Close() }()
	b, err = fallback.Get(ctx, "sample")
	require.NoError(t, err)
	require.Equal(t, "two", b.Instructions)
	source.failure = false
	source.descriptors[0].Version = "v1"
	_, err = r.Refresh(ctx)
	require.NoError(t, err)
	rollback := r.Snapshot()
	defer func() { _ = rollback.Close() }()
	a, err = rollback.Get(ctx, "sample")
	require.NoError(t, err)
	require.Equal(t, "one", a.Instructions)
	require.Equal(t, 2, source.downloads)
}

func TestArchiveSafety(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "C:/drive", "a\\b", "a/../../escape", "a//b"} {
		t.Run(name, func(t *testing.T) {
			testBadArchive(t, fixtureZip(t, map[string]string{"SKILL.md": "---\nname: sample\ndescription: test\n---\n", name: "bad"}), SkillCacheConfig{})
		})
	}
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	h := &zip.FileHeader{Name: "SKILL.md"}
	h.SetMode(os.ModeSymlink | 0777)
	w, err := zw.CreateHeader(h)
	require.NoError(t, err)
	_, err = w.Write([]byte("/etc/passwd"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	testBadArchive(t, b.Bytes(), SkillCacheConfig{})
	testBadArchive(t, skillArchive(t, "other", "wrong name"), SkillCacheConfig{})
	testBadArchive(t, skillArchive(t, "sample", strings.Repeat("x", 4096)), SkillCacheConfig{MaxExpandedBytes: 100})
	testBadArchive(t, skillArchive(t, "sample", "ok"), SkillCacheConfig{MaxFiles: 1})
	testBadArchive(t, []byte("not zip"), SkillCacheConfig{})
}
func testBadArchive(t *testing.T, data []byte, config SkillCacheConfig) {
	t.Helper()
	config.Directory = t.TempDir()
	cache, err := NewSkillCache(config)
	require.NoError(t, err)
	source := &fixtureSource{id: "fixture", archives: map[string][]byte{"v1": data}}
	_, err = cache.Materialize(context.Background(), source, RemoteSkill{Name: "sample", Version: "v1"})
	require.Error(t, err)
	entries, err := os.ReadDir(config.Directory)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestRemoteTOSDownload(t *testing.T) {
	archive := skillArchive(t, "sample", "TOS")
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Contains(t, r.URL.Path, "object.zip")
		require.Equal(t, "test-token", r.Header.Get("X-Tos-Security-Token"))
		require.Contains(t, r.Header.Get("Authorization"), "test-ak/")
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	source, err := NewRemoteSource(RemoteSourceConfig{SpaceID: "space-test", Credentials: fakeCredential(&count), TOSEndpoint: server.URL})
	require.NoError(t, err)
	var b bytes.Buffer
	require.NoError(t, source.Download(context.Background(), RemoteSkill{Name: "sample", Bucket: "test-bucket", ObjectKey: "object.zip"}, &b))
	require.Equal(t, archive, b.Bytes())
}

func TestRemoteTemporaryURLAndRedirectPolicy(t *testing.T) {
	var count atomic.Int32
	archive := skillArchive(t, "sample", "temporary")
	var authenticated atomic.Bool
	download := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authenticated.Store(r.Header.Get("Authorization") != "" || r.Header.Get("X-Security-Token") != "")
		_, _ = w.Write(archive)
	}))
	defer download.Close()
	signedURL := download.URL + "/skill.zip?signature=secret"
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GenTempTosObjectDownloadUrl", r.URL.Query().Get("Action"))
		_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]string{"SignedUrl": signedURL}})
	}))
	defer control.Close()
	config := RemoteSourceConfig{SpaceID: "space-test", Endpoint: control.URL, Credentials: fakeCredential(&count), HTTPClient: download.Client(), UseTemporaryDownloadURL: true, AllowedDownloadHosts: []string{strings.TrimPrefix(download.URL, "https://")}}
	source, err := NewRemoteSource(config)
	require.NoError(t, err)
	var b bytes.Buffer
	d := RemoteSkill{Name: "sample", ID: "id", ObjectKey: "skills/id/v1/archive.zip"}
	require.NoError(t, source.Download(context.Background(), d, &b))
	require.Equal(t, archive, b.Bytes())
	require.False(t, authenticated.Load())
	signedURL = "https://untrusted.invalid/archive.zip?signature=secret"
	err = source.Download(context.Background(), d, io.Discard)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, download.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	config.Endpoint = redirect.URL
	config.UseTemporaryDownloadURL = false
	source, err = NewRemoteSource(config)
	require.NoError(t, err)
	_, err = source.Discover(context.Background())
	require.Error(t, err)
}

func TestStrictRefreshCollisionAndConcurrentSnapshots(t *testing.T) {
	ctx := context.Background()
	local := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(local, "sample"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(local, "sample", "SKILL.md"), []byte("---\nname: sample\ndescription: local\n---\nlocal"), 0600))
	source := &fixtureSource{id: "first", descriptors: []RemoteSkill{{Name: "sample", Version: "v1"}, {Name: "remote", Version: "v1"}}, archives: map[string][]byte{"v1": skillArchive(t, "remote", "first")}}
	second := &fixtureSource{id: "second", descriptors: []RemoteSkill{{Name: "remote", Version: "v1"}}, archives: map[string][]byte{"v1": skillArchive(t, "remote", "second")}}
	cache, err := NewSkillCache(SkillCacheConfig{Directory: t.TempDir()})
	require.NoError(t, err)
	r, err := NewRegistry(RegistryConfig{LocalDirectories: []string{local}, Sources: []SourceBinding{{Source: source}, {Source: second}}, Cache: cache})
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	_, err = r.Refresh(ctx)
	require.NoError(t, err)
	s := r.Snapshot()
	defer func() { _ = s.Close() }()
	sk, err := s.Get(ctx, "sample")
	require.NoError(t, err)
	require.Equal(t, "local", sk.Instructions)
	sk, err = s.Get(ctx, "remote")
	require.NoError(t, err)
	require.Equal(t, "first", sk.Instructions)
	require.Zero(t, second.downloads)
	source.failure = true
	_, err = r.Refresh(ctx)
	require.Error(t, err)
	unchanged := r.Snapshot()
	require.Len(t, unchanged.List(), 2)
	require.NoError(t, unchanged.Close())
	source.failure = false
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_, err := r.Refresh(ctx)
				require.NoError(t, err)
				snapshot := r.Snapshot()
				_, err = snapshot.Get(ctx, "remote")
				require.NoError(t, err)
				require.NoError(t, snapshot.Close())
				require.NoError(t, cache.Prune(ctx))
			}
		}()
	}
	wg.Wait()
}

func TestCacheQuotaCanceledWaitAndRestart(t *testing.T) {
	ctx := context.Background()
	config := SkillCacheConfig{Directory: t.TempDir()}
	source := &fixtureSource{id: "test", archives: map[string][]byte{"v1": skillArchive(t, "sample", "test")}}
	d := RemoteSkill{Name: "sample", Version: "v1"}
	cache, err := NewSkillCache(config)
	require.NoError(t, err)
	_, err = cache.Materialize(ctx, source, d)
	require.NoError(t, err)
	restarted, err := NewSkillCache(config)
	require.NoError(t, err)
	_, err = restarted.Materialize(ctx, source, d)
	require.NoError(t, err)
	require.Equal(t, 1, source.downloads)
	require.NoError(t, cache.lock(ctx))
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = cache.Materialize(canceled, source, d)
	require.ErrorIs(t, err, context.Canceled)
	cache.unlock()
	_, err = cache.Materialize(ctx, source, d)
	require.NoError(t, err)
	quota, err := NewSkillCache(SkillCacheConfig{Directory: t.TempDir(), MaxCacheBytes: 10})
	require.NoError(t, err)
	_, err = quota.Materialize(ctx, source, d)
	require.Error(t, err)
	stage := filepath.Join(config.Directory, ".stage-abandoned")
	require.NoError(t, os.Mkdir(stage, 0700))
	require.NoError(t, restarted.Prune(ctx))
	_, err = os.Stat(stage)
	require.True(t, os.IsNotExist(err))
}

func TestPython1084ArchiveLayouts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		paths []string
		want  string
	}{
		{"root", []string{"SKILL.md", "other/SKILL.md"}, "."},
		{"wrapped-with-extra", []string{"wrapper/SKILL.md", "NOTICE"}, "wrapper"},
		{"nested", []string{"release/v2/sample/SKILL.md", "NOTICE"}, "release/v2/sample"},
		{"shallow-first", []string{"a/deep/SKILL.md", "z/SKILL.md"}, "z"},
		{"lexical-first", []string{"b/SKILL.md", "a/SKILL.md"}, "a"},
		{"legacy-case", []string{"sample/skill.md"}, "sample"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			for _, p := range tc.paths {
				files[p] = "---\nname: sample\ndescription: test\n---\n" + tc.name
			}
			data := fixtureZip(t, files)
			cache, err := NewSkillCache(SkillCacheConfig{Directory: t.TempDir()})
			require.NoError(t, err)
			source := &fixtureSource{id: "fixture", archives: map[string][]byte{"v1": data}}
			sk, err := cache.Materialize(context.Background(), source, RemoteSkill{Name: "sample", Version: "v1"})
			require.NoError(t, err)
			require.Equal(t, tc.name, sk.Instructions)
			require.True(t, strings.HasSuffix(sk.GetSkillPath(), filepath.Join("content", filepath.FromSlash(tc.want))))
		})
	}
}

func TestRemoteStatusRetriesAreBounded(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls, credentials atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "secret-provider-error")
			}))
			defer server.Close()
			source, err := NewRemoteSource(RemoteSourceConfig{SpaceID: "ss-test", Endpoint: server.URL, Credentials: fakeCredential(&credentials)})
			require.NoError(t, err)
			_, err = source.Discover(t.Context())
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret-provider-error")
			want := int32(2)
			if status == 401 || status == 403 {
				want = 1
			}
			require.Equal(t, want, calls.Load())
			require.Equal(t, want, credentials.Load())
		})
	}
}
