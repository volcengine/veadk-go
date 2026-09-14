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

package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoteBinaryHTTPAndRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the standalone example binary")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "remote-skills")
	build := exec.Command("go", "build", "-o", binary, ".")
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"SKILL.md": "---\nname: sample\ndescription: Fixture\n---\nUse the reference", "references/value.txt": "expected-resource"} {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	t.Run("skill-space", func(t *testing.T) {
		var downloads atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				downloads.Add(1)
				_, _ = w.Write(buf.Bytes())
				return
			}
			_, _ = io.WriteString(w, `{"Result":{"Items":[{"Name":"sample","Description":"Fixture","SkillId":"id","BucketName":"bucket","TosPath":"object.zip"}]}}`)
		}))
		defer server.Close()
		id := "space-test"
		cache := filepath.Join(dir, id)
		run := func(resource string) ([]byte, error) {
			cmd := exec.Command(binary, "-space", id, "-endpoint", server.URL, "-tos-endpoint", server.URL, "-cache", cache, "-skill", "sample", "-resource", resource)
			cmd.Dir = dir
			cmd.Env = []string{"VOLCENGINE_ACCESS_KEY=fake-ak", "VOLCENGINE_SECRET_KEY=fake-sk", "VOLCENGINE_SESSION_TOKEN=fake-token"}
			return cmd.CombinedOutput()
		}
		for i := 0; i < 2; i++ {
			output, err := run("references/value.txt")
			require.NoError(t, err, string(output))
			var result struct {
				Loaded        string
				SHA256        string
				ResourceBytes int `json:"resource_bytes"`
			}
			require.NoError(t, json.Unmarshal(bytes.TrimSpace(output), &result))
			require.Equal(t, "sample", result.Loaded)
			require.Len(t, result.SHA256, 64)
			require.Equal(t, len("expected-resource"), result.ResourceBytes)
		}
		require.EqualValues(t, 1, downloads.Load())
		output, err := run("../escape")
		require.Error(t, err)
		require.NotContains(t, string(output), "fake-sk")
		entries, err := os.ReadDir(cache)
		require.NoError(t, err)
		for _, entry := range entries {
			require.False(t, strings.HasPrefix(entry.Name(), ".stage-"))
		}
	})
}
