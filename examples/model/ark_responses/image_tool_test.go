package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	vemodel "github.com/volcengine/veadk-go/v2/model"
	verunner "github.com/volcengine/veadk-go/v2/runner"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/plugin"
	adkrunner "google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

func TestToolImagePersistsAndReplays(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if requests == 1 {
			_, _ = fmt.Fprint(w, `{"status":"completed","output":[{"type":"function_call","call_id":"picture-call","name":"get_picture","arguments":"{}"}]}`)
			return
		}
		raw, _ := json.Marshal(body)
		for _, want := range []string{`"type":"function_call_output"`, `"call_id":"picture-call"`, `"image_url":"data:image/png;base64,AQID"`} {
			if !strings.Contains(string(raw), want) {
				t.Errorf("missing %s from continuation/replayed history", want)
			}
		}
		_, _ = fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"a picture"}]}]}`)
	}))
	defer server.Close()
	m, err := vemodel.NewArkResponsesModel(context.Background(), "vision", &vemodel.ArkClientConfig{APIKey: "key", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	imageTool, imagePlugin, err := imageTool(func(agent.Context) (*genai.FunctionResponsePart, error) {
		return genai.NewFunctionResponsePartFromBytes([]byte{1, 2, 3}, "image/png"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := veagent.New(&veagent.Config{Config: llmagent.Config{Name: "vision", Model: m, Tools: []tool.Tool{imageTool}}})
	if err != nil {
		t.Fatal(err)
	}
	store := session.InMemoryService()
	r, err := verunner.New(verunner.Config{AppName: "test", Agent: a, SessionService: store, AutoCreateSession: true, PluginConfig: adkrunner.PluginConfig{Plugins: []*plugin.Plugin{imagePlugin}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"look", "look again"} {
		for _, err := range r.Run(context.Background(), "user", "session", genai.NewContentFromText(prompt, "user"), agent.RunConfig{}) {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if requests != 3 {
		t.Fatalf("requests=%d", requests)
	}
	got, err := store.Get(context.Background(), &session.GetRequest{AppName: "test", UserID: "user", SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for ev := range got.Session.Events().All() {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if f := p.FunctionResponse; f != nil && f.ID == "picture-call" {
				found = true
				if len(f.Parts) != 1 || string(f.Parts[0].InlineData.Data) != string([]byte{1, 2, 3}) {
					t.Fatal("lost persisted attachment")
				}
				if _, ok := f.Response["parts"]; ok {
					t.Fatal("binary media leaked into tool JSON")
				}
			}
		}
	}
	if !found {
		t.Fatal("tool result missing")
	}
}
