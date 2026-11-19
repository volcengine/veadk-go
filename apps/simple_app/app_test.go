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

package simple_app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/v2/apps"
	verunner "github.com/volcengine/veadk-go/v2/runner"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestInvokeUsesRunnerFinalText(t *testing.T) {
	echoAgent, err := agent.New(agent.Config{
		Name: "echo",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				event := session.NewEvent(ctx, ctx.InvocationID())
				event.Author = "echo"
				event.LLMResponse = model.LLMResponse{
					Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
						{Text: "thinking", Thought: true},
						{Text: "echo: "},
						{Text: ctx.UserContent().Parts[0].Text},
					}},
				}
				yield(event, nil)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	app := NewAgentkitSimpleApp(apps.DefaultApiConfig())
	router := mux.NewRouter()
	err = app.SetupRouters(router, &apps.RunConfig{
		AgentLoader:    agent.NewSingleLoader(echoAgent),
		SessionService: session.InMemoryService(),
	})
	if err != nil {
		t.Fatalf("SetupRouters() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"prompt":"hello"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var got Response
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Code != http.StatusOK {
		t.Fatalf("response code = %d, want %d", got.Code, http.StatusOK)
	}
	if got.SessionId == "" {
		t.Fatal("session_id is empty")
	}
	if got.Data != "echo: hello" {
		t.Fatalf("data = %q, want %q", got.Data, "echo: hello")
	}
}

func TestUpdateSessionEndpointUsesRuntimeSessionService(t *testing.T) {
	stateAgent, err := agent.New(agent.Config{
		Name: "state_reader",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				phase, err := ctx.Session().State().Get("phase")
				if err != nil {
					yield(nil, err)
					return
				}
				event := session.NewEvent(ctx, ctx.InvocationID())
				event.Author = "state_reader"
				event.LLMResponse = model.LLMResponse{
					Content: genai.NewContentFromText(fmt.Sprint(phase), genai.RoleModel),
				}
				yield(event, nil)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	sessionService := session.InMemoryService()
	app := NewAgentkitSimpleApp(apps.DefaultApiConfig()).(*agentkitSimpleApp)
	router := mux.NewRouter()
	if err := app.SetupRouters(router, &apps.RunConfig{
		AgentLoader:    agent.NewSingleLoader(stateAgent),
		SessionService: sessionService,
	}); err != nil {
		t.Fatalf("SetupRouters() error = %v", err)
	}
	sessionID := app.session.ID()

	body, err := json.Marshal(verunner.SessionUpdateRequest{
		StateDelta: map[string]any{"phase": "approved"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/sessions/"+sessionID+":update", bytes.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	invoke := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"prompt":"read"}`))
	invokeResponse := httptest.NewRecorder()
	router.ServeHTTP(invokeResponse, invoke)
	if invokeResponse.Code != http.StatusOK {
		t.Fatalf("invoke status = %d, want %d: %s", invokeResponse.Code, http.StatusOK, invokeResponse.Body.String())
	}
	var got Response
	if err := json.NewDecoder(invokeResponse.Body).Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Data != "approved" {
		t.Fatalf("data = %q, want approved", got.Data)
	}
}

func TestUpdateSessionEndpointContentAndArtifact(t *testing.T) {
	noopAgent, err := agent.New(agent.Config{
		Name: "noop",
		Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(func(*session.Event, error) bool) {}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	sessionService := session.InMemoryService()
	app := NewAgentkitSimpleApp(apps.DefaultApiConfig()).(*agentkitSimpleApp)
	router := mux.NewRouter()
	if err := app.SetupRouters(router, &apps.RunConfig{
		AgentLoader:    agent.NewSingleLoader(noopAgent),
		SessionService: sessionService,
	}); err != nil {
		t.Fatalf("SetupRouters() error = %v", err)
	}
	sessionID := app.session.ID()

	req := verunner.SessionUpdateRequest{
		Author:        "external_tool",
		Content:       genai.NewContentFromText("approval granted", genai.RoleUser),
		ArtifactDelta: map[string]int64{"approval.pdf": 3},
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/sessions/"+sessionID+":update", bytes.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	stored, err := sessionService.Get(t.Context(), &session.GetRequest{
		AppName: app.appName, UserID: app.userID, SessionID: sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Session.Events().Len() != 1 {
		t.Fatalf("events = %d, want 1", stored.Session.Events().Len())
	}
	event := stored.Session.Events().At(0)
	if event.Author != "external_tool" || event.Content == nil || event.Content.Parts[0].Text != "approval granted" {
		t.Fatalf("unexpected event content: %+v", event)
	}
	if got := event.Actions.ArtifactDelta["approval.pdf"]; got != 3 {
		t.Fatalf("artifact delta = %d, want 3", got)
	}

	badRequest := httptest.NewRequest(http.MethodPost, "/sessions/"+sessionID+":update", strings.NewReader(`{"content":{"role":"user","parts":[{"text":"missing author"}]}}`))
	badResponse := httptest.NewRecorder()
	router.ServeHTTP(badResponse, badRequest)
	if badResponse.Code != http.StatusBadRequest {
		t.Fatalf("bad status = %d, want %d: %s", badResponse.Code, http.StatusBadRequest, badResponse.Body.String())
	}
}
