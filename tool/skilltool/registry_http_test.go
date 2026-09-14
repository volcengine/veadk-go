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

package skilltool_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/volcengine/veadk-go/apps"
	"github.com/volcengine/veadk-go/apps/simple_app"
	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/skills"
	"github.com/volcengine/veadk-go/tool/skilltool"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

type remoteLoopModel struct{}

func (remoteLoopModel) Name() string { return "remote-loop-fixture" }
func (remoteLoopModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		last := req.Contents[len(req.Contents)-1]
		var response *genai.FunctionResponse
		for _, p := range last.Parts {
			if p.FunctionResponse != nil {
				response = p.FunctionResponse
			}
		}
		part := &genai.Part{FunctionCall: &genai.FunctionCall{ID: "load", Name: "load_skill", Args: map[string]any{"name": "sample"}}}
		if response != nil {
			switch response.Name {
			case "load_skill":
				if response.Response["instructions"] != "Read the reference" {
					yield(nil, fmt.Errorf("unexpected skill instructions"))
					return
				}
				part = &genai.Part{FunctionCall: &genai.FunctionCall{ID: "resource", Name: "load_skill_resource", Args: map[string]any{"skill_name": "sample", "path": "references/result.txt"}}}
			case "load_skill_resource":
				value, ok := response.Response["content"].(string)
				if !ok {
					yield(nil, fmt.Errorf("missing skill resource result"))
					return
				}
				part = &genai.Part{Text: value}
			}
		}
		yield(&model.LLMResponse{Content: &genai.Content{Role: "model", Parts: []*genai.Part{part}}, TurnComplete: part.FunctionCall == nil}, nil)
	}
}

func TestRegistryAgentHTTPToolLoop(t *testing.T) {
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	for name, body := range map[string]string{"release/sample/SKILL.md": "---\nname: sample\ndescription: HTTP skill\n---\nRead the reference", "release/sample/references/result.txt": "remote-skill-http-success"} {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	var downloads atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			downloads.Add(1)
			_, _ = w.Write(archive.Bytes())
			return
		}
		_, _ = io.WriteString(w, `{"Result":{"Items":[{"Name":"sample","Description":"HTTP skill","BucketName":"bucket","TosPath":"package.zip","SkillVersion":"v1"}]}}`)
	}))
	defer upstream.Close()
	source, err := skills.NewRemoteSource(skills.RemoteSourceConfig{SpaceID: "ss-test", Endpoint: upstream.URL, TOSEndpoint: upstream.URL, Credentials: veauth.NewRoleCredentialSource(veauth.RoleCredentialSourceConfig{AccessKeyID: "fake-ak", SecretAccessKey: "fake-sk", SessionToken: "fake-token"})})
	require.NoError(t, err)
	cache, err := skills.NewSkillCache(skills.SkillCacheConfig{Directory: t.TempDir()})
	require.NoError(t, err)
	registry, err := skills.NewRegistry(skills.RegistryConfig{Sources: []skills.SourceBinding{{Source: source}}, Cache: cache})
	require.NoError(t, err)
	defer func() { _ = registry.Close() }()
	_, err = registry.Refresh(t.Context())
	require.NoError(t, err)
	snapshot := registry.Snapshot()
	defer func() { _ = snapshot.Close() }()
	toolset, err := skilltool.NewRegistrySkillToolset(snapshot)
	require.NoError(t, err)
	root, err := llmagent.New(llmagent.Config{Name: "remote_http_agent", Model: remoteLoopModel{}, Toolsets: []tool.Toolset{toolset}})
	require.NoError(t, err)
	app := simple_app.NewAgentkitSimpleApp(apps.DefaultApiConfig())
	router := mux.NewRouter()
	require.NoError(t, app.SetupRouters(router, &apps.RunConfig{AgentLoader: agent.NewSingleLoader(root), SessionService: session.InMemoryService(), DisableObservability: true}))
	server := httptest.NewServer(router)
	defer server.Close()
	require.Zero(t, downloads.Load())
	resp, err := http.Post(server.URL+"/invoke", "application/json", strings.NewReader(`{"prompt":"Use the remote skill"}`))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result simple_app.Response
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.Equal(t, "remote-skill-http-success", result.Data)
	require.EqualValues(t, 1, downloads.Load())
}
