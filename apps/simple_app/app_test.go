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
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/v2/apps"
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
