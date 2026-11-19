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

package agentkit_server_app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/v2/apps"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type recordingArtifactService struct {
	artifact.Service
	deletes []artifact.DeleteRequest
	ctx     context.Context
	err     error
}

func (s *recordingArtifactService) Delete(ctx context.Context, request *artifact.DeleteRequest) error {
	s.deletes = append(s.deletes, *request)
	s.ctx = ctx
	if s.err != nil {
		return s.err
	}
	if s.Service != nil {
		return s.Service.Delete(ctx, request)
	}
	return nil
}

func artifactPathVars() map[string]string {
	return map[string]string{
		"app_name": "artifact_agent", "user_id": "user", "session_id": "session",
		"artifact_name": "report.txt", "version": "2",
	}
}

// REQ-001: all five fields and the incoming context must reach the same Service.
func TestDeleteArtifactVersionHandlerMapsRequest(t *testing.T) {
	for _, test := range []struct {
		version string
		want    int64
	}{
		{"1", 1}, {"2", 2}, {"0002", 2}, {"+2", 2}, {"9223372036854775807", 9223372036854775807},
	} {
		t.Run(test.version, func(t *testing.T) {
			service := &recordingArtifactService{}
			vars := artifactPathVars()
			vars["version"] = test.version
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request := mux.SetURLVars(httptest.NewRequest(http.MethodDelete, "/", nil).WithContext(ctx), vars)
			response := httptest.NewRecorder()
			NewDeleteArtifactVersionHandler(service)(response, request)
			if response.Code != http.StatusOK || response.Body.Len() != 0 {
				t.Fatalf("response = %d %q, want 200 and empty body", response.Code, response.Body.String())
			}
			want := artifact.DeleteRequest{AppName: "artifact_agent", UserID: "user", SessionID: "session", FileName: "report.txt", Version: test.want}
			if len(service.deletes) != 1 || service.deletes[0] != want {
				t.Fatalf("Delete requests = %v, want [%v]", service.deletes, want)
			}
			if service.ctx != request.Context() {
				t.Fatal("Delete did not receive the request context")
			}
			cancel()
			if !errors.Is(service.ctx.Err(), context.Canceled) {
				t.Fatal("Delete context did not preserve cancellation")
			}
		})
	}
}

// REQ-002: invalid input must never reach Delete, particularly Version=0.
func TestDeleteArtifactVersionHandlerRejectsInvalidParameters(t *testing.T) {
	for _, version := range []string{"abc", "1.5", "1e2", " ", " 2", "2 ", "0x2", "1_000", "9223372036854775808", "-9223372036854775809", "0", "+0", "-0", "-1"} {
		t.Run("version="+version, func(t *testing.T) {
			vars := artifactPathVars()
			vars["version"] = version
			assertArtifactParametersRejected(t, vars, "version")
		})
	}
	for name := range artifactPathVars() {
		for _, missing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/missing=%t", name, missing), func(t *testing.T) {
				vars := maps.Clone(artifactPathVars())
				vars[name] = ""
				if missing {
					delete(vars, name)
				}
				assertArtifactParametersRejected(t, vars, name)
			})
		}
	}
	assertArtifactParametersRejected(t, nil, "app_name")
}

func assertArtifactParametersRejected(t *testing.T, vars map[string]string, parameter string) {
	t.Helper()
	service := &recordingArtifactService{}
	response := httptest.NewRecorder()
	NewDeleteArtifactVersionHandler(service)(response, mux.SetURLVars(httptest.NewRequest(http.MethodDelete, "/", nil), vars))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), parameter) {
		t.Fatalf("response = %d %q, want 400 identifying %s", response.Code, response.Body.String(), parameter)
	}
	if len(service.deletes) != 0 {
		t.Fatalf("invalid parameters triggered Delete: %v", service.deletes)
	}
}

func newArtifactRouter(t *testing.T, service artifact.Service, prefix string, disableObservability bool, options ...Option) *mux.Router {
	t.Helper()
	root, err := agent.New(agent.Config{Name: "artifact_agent"})
	if err != nil {
		t.Fatal(err)
	}
	app := NewAgentkitServerApp(apps.DefaultApiConfig().SetApiPathPrefix(prefix).SetSimpleAPIEnabled(false).SetWebUIEnabled(false), options...)
	router := mux.NewRouter()
	if err := app.SetupRouters(router, &apps.RunConfig{
		AgentLoader: agent.NewSingleLoader(root), SessionService: session.InMemoryService(),
		ArtifactService: service, DisableObservability: disableObservability,
	}); err != nil {
		t.Fatal(err)
	}
	return router
}

func artifactHTTP(t *testing.T, router http.Handler, method, path string, status int) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	if response.Code != status {
		t.Fatalf("%s %s = %d %q, want %d", method, path, response.Code, response.Body.String(), status)
	}
	return response
}

func saveArtifactVersions(t *testing.T, service artifact.Service, scope artifact.DeleteRequest, count int) {
	t.Helper()
	for version := 1; version <= count; version++ {
		response, err := service.Save(context.Background(), &artifact.SaveRequest{
			AppName: scope.AppName, UserID: scope.UserID, SessionID: scope.SessionID, FileName: scope.FileName,
			Part: genai.NewPartFromText(fmt.Sprintf("content-%d", version)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if response.Version != int64(version) {
			t.Fatalf("saved version = %d, want %d", response.Version, version)
		}
	}
}

func assertArtifactContent(t *testing.T, service artifact.Service, scope artifact.DeleteRequest, versions ...int64) {
	t.Helper()
	got, err := service.Versions(context.Background(), &artifact.VersionsRequest{
		AppName: scope.AppName, UserID: scope.UserID, SessionID: scope.SessionID, FileName: scope.FileName,
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got.Versions)
	if !slices.Equal(got.Versions, versions) {
		t.Fatalf("scope %v versions = %v, want %v", scope, got.Versions, versions)
	}
	for _, version := range versions {
		loaded, err := service.Load(context.Background(), &artifact.LoadRequest{
			AppName: scope.AppName, UserID: scope.UserID, SessionID: scope.SessionID, FileName: scope.FileName, Version: version,
		})
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Part.Text != fmt.Sprintf("content-%d", version) {
			t.Fatalf("version %d content changed: %q", version, loaded.Part.Text)
		}
	}
}

// REQ-001/003: exercise the mux router without TCP, with actual stored versions.
func TestArtifactVersionRoutePreservesOtherVersionsAndLegacyAPI(t *testing.T) {
	for _, prefix := range []string{"", "/api"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			service := &recordingArtifactService{Service: artifact.InMemoryService()}
			scope := artifact.DeleteRequest{AppName: "artifact_agent", UserID: "user", SessionID: "session", FileName: "report.txt"}
			scopes := []artifact.DeleteRequest{
				scope,
				{AppName: "other_app", UserID: "user", SessionID: "session", FileName: "report.txt"},
				{AppName: "artifact_agent", UserID: "other_user", SessionID: "session", FileName: "report.txt"},
				{AppName: "artifact_agent", UserID: "user", SessionID: "other_session", FileName: "report.txt"},
				{AppName: "artifact_agent", UserID: "user", SessionID: "session", FileName: "control.txt"},
			}
			for _, item := range scopes {
				saveArtifactVersions(t, service, item, 3)
			}
			router := newArtifactRouter(t, service, prefix, true)
			base := prefix + "/apps/artifact_agent/users/user/sessions/session/artifacts"
			path := base + "/report.txt"
			assertArtifactLoad := func(path, content string) {
				t.Helper()
				response := artifactHTTP(t, router, http.MethodGet, path, http.StatusOK)
				var part genai.Part
				if err := json.Unmarshal(response.Body.Bytes(), &part); err != nil {
					t.Fatal(err)
				}
				if part.Text != content {
					t.Fatalf("GET %s content = %q, want %q", path, part.Text, content)
				}
			}
			for _, suffix := range []string{"", "?version=3", "/versions/3"} {
				assertArtifactLoad(path+suffix, "content-3")
			}
			response := artifactHTTP(t, router, http.MethodDelete, path+"/versions/2", http.StatusOK)
			if response.Body.Len() != 0 {
				t.Fatalf("successful DELETE body = %q", response.Body.String())
			}
			assertArtifactContent(t, service, scope, 1, 3)
			assertArtifactLoad(path+"/versions/1", "content-1")
			assertArtifactLoad(path+"?version=1", "content-1")
			artifactHTTP(t, router, http.MethodGet, path+"/versions/2", http.StatusInternalServerError)
			for _, version := range []string{"2", "9223372036854775807"} {
				artifactHTTP(t, router, http.MethodDelete, path+"/versions/"+version, http.StatusOK)
				assertArtifactContent(t, service, scope, 1, 3)
			}
			for _, version := range []string{"abc", "1.5", "1e2", "%20", "9223372036854775808", "0", "-1"} {
				calls := len(service.deletes)
				artifactHTTP(t, router, http.MethodDelete, path+"/versions/"+version, http.StatusBadRequest)
				if len(service.deletes) != calls {
					t.Fatal("invalid version reached Delete")
				}
			}
			listed := artifactHTTP(t, router, http.MethodGet, base, http.StatusOK)
			var names []string
			if err := json.Unmarshal(listed.Body.Bytes(), &names); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(names, "report.txt") || !slices.Contains(names, "control.txt") {
				t.Fatalf("artifact list = %v", names)
			}
			oldResponse := artifactHTTP(t, router, http.MethodDelete, path, http.StatusOK)
			if oldResponse.Body.Len() != 0 || oldResponse.Header().Get("Content-Type") != response.Header().Get("Content-Type") {
				t.Fatalf("new and old successful DELETE responses differ: new=%v old=%v", response.Header(), oldResponse.Header())
			}
			if service.deletes[len(service.deletes)-1].Version != 0 {
				t.Fatal("old DELETE no longer passes Version=0")
			}
			listed = artifactHTTP(t, router, http.MethodGet, base, http.StatusOK)
			if strings.Contains(listed.Body.String(), "report.txt") {
				t.Fatal("old DELETE did not remove the entire artifact")
			}
			for _, other := range scopes[1:] {
				assertArtifactContent(t, service, other, 1, 2, 3)
			}
		})
	}
}

func TestArtifactVersionRouteDeletesLatestAndOnlyVersion(t *testing.T) {
	service := artifact.InMemoryService()
	scope := artifact.DeleteRequest{AppName: "artifact_agent", UserID: "user", SessionID: "session", FileName: "report.txt"}
	saveArtifactVersions(t, service, scope, 3)
	router := newArtifactRouter(t, service, "", true)
	base := "/apps/artifact_agent/users/user/sessions/session/artifacts"
	path := base + "/report.txt"
	for _, version := range []int{3, 2, 1} {
		artifactHTTP(t, router, http.MethodDelete, fmt.Sprintf("%s/versions/%d", path, version), http.StatusOK)
		if version > 1 {
			response := artifactHTTP(t, router, http.MethodGet, path, http.StatusOK)
			var part genai.Part
			if err := json.Unmarshal(response.Body.Bytes(), &part); err != nil {
				t.Fatal(err)
			}
			if part.Text != fmt.Sprintf("content-%d", version-1) {
				t.Fatalf("latest did not fall back: %q", part.Text)
			}
		}
	}
	response := artifactHTTP(t, router, http.MethodGet, base, http.StatusOK)
	if strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("last version deletion did not remove artifact: %q", response.Body.String())
	}
}

func TestArtifactVersionRouteErrors(t *testing.T) {
	path := "/api/apps/artifact_agent/users/user/sessions/session/artifacts/report.txt/versions/2"
	service := &recordingArtifactService{err: errors.New("artifact backend unavailable")}
	router := newArtifactRouter(t, service, "/api", true)
	response := artifactHTTP(t, router, http.MethodDelete, path, http.StatusInternalServerError)
	if response.Body.String() != service.err.Error()+"\n" || len(service.deletes) != 1 || service.deletes[0].Version != 2 {
		t.Fatalf("backend error response = %q, calls = %v", response.Body.String(), service.deletes)
	}
	router = newArtifactRouter(t, nil, "/api", true)
	response = artifactHTTP(t, router, http.MethodDelete, path, http.StatusInternalServerError)
	if response.Body.String() != "artifact service is not configured\n" {
		t.Fatalf("nil service response = %q", response.Body.String())
	}
	artifactHTTP(t, router, http.MethodDelete, strings.TrimSuffix(path, "2")+"0", http.StatusBadRequest)
}

func TestArtifactVersionRouteMethodsMissingSegmentsAndMiddleware(t *testing.T) {
	for _, disableObservability := range []bool{true, false} {
		t.Run(fmt.Sprintf("disableObservability=%t", disableObservability), func(t *testing.T) {
			service := &recordingArtifactService{Service: artifact.InMemoryService()}
			scope := artifact.DeleteRequest{AppName: "artifact_agent", UserID: "user", SessionID: "session", FileName: "report.txt"}
			saveArtifactVersions(t, service, scope, 3)
			path := "/api/apps/artifact_agent/users/user/sessions/session/artifacts/report.txt/versions/2"
			router := newArtifactRouter(t, service, "/api", disableObservability,
				WithMiddleware(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("X-Artifact-Middleware", "enabled")
						next.ServeHTTP(w, r)
					})
				}),
				WithRouteSetup(func(router *mux.Router, _ *apps.RunConfig) error {
					router.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(299) }).Methods(http.MethodDelete)
					return nil
				}),
			)
			artifactHTTP(t, router, http.MethodGet, path, http.StatusOK)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodOptions, path, nil))
			for _, malformed := range []string{
				strings.TrimSuffix(path, "2"), strings.TrimSuffix(path, "/2"),
				strings.Replace(path, "/report.txt/", "//", 1), strings.Replace(path, "/user/", "//", 1),
				strings.Replace(path, "/session/", "//", 1), strings.Replace(path, "/artifact_agent/", "//", 1),
			} {
				requestPath := malformed
				for redirects := 0; ; redirects++ {
					response := httptest.NewRecorder()
					router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, requestPath, nil))
					if response.Code == http.StatusOK {
						t.Fatalf("malformed path succeeded: %s", requestPath)
					}
					location := response.Header().Get("Location")
					if location == "" {
						break
					}
					if redirects >= 3 {
						t.Fatal("unexpected redirect loop")
					}
					requestPath = location
				}
			}
			if len(service.deletes) != 0 {
				t.Fatalf("GET/OPTIONS/malformed paths triggered Delete: %v", service.deletes)
			}
			assertArtifactContent(t, service, scope, 1, 2, 3)
			response = artifactHTTP(t, router, http.MethodDelete, path, http.StatusOK)
			if response.Header().Get("X-Artifact-Middleware") != "enabled" || len(service.deletes) != 1 {
				t.Fatal("new built-in route did not preserve middleware or route priority")
			}
		})
	}
}
