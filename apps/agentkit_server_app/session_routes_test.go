// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package agentkit_server_app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/v2/apps"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func sessionRouter(t *testing.T, store session.Service, options ...Option) *mux.Router {
	t.Helper()
	root, err := agent.New(agent.Config{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	app := NewAgentkitServerApp(apps.DefaultApiConfig().SetApiPathPrefix("/api").SetSimpleAPIEnabled(false).SetWebUIEnabled(false), options...)
	router := mux.NewRouter()
	if err := app.SetupRouters(router, &apps.RunConfig{AgentLoader: agent.NewSingleLoader(root), SessionService: store, DisableObservability: true}); err != nil {
		t.Fatal(err)
	}
	return router
}
func callSession(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}
func jsonObject(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("%d: %s", w.Code, w.Body.String())
	}
	return v
}

const sessionBase = "/api/apps/test/users/user/sessions"

func TestSessionProtocolPaginationAndADKEvents(t *testing.T) {
	store := session.InMemoryService()
	router := sessionRouter(t, store)
	for i := 0; i < 3; i++ {
		w := callSession(router, "POST", sessionBase, fmt.Sprintf(`{"sessionId":"s%d","state":{"secret":"business"}}`, i))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := callSession(router, "POST", sessionBase, `{"sessionId":"s1"}`); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if w := callSession(router, "POST", sessionBase+"/path", `{"sessionId":"body"}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	first := jsonObject(t, callSession(router, "GET", sessionBase+"?limit=2", ""))
	items := first["sessions"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["id"] != "s2" || len(items[0].(map[string]any)["state"].(map[string]any)) != 0 {
		t.Fatalf("%+v", first)
	}
	cursor := url.QueryEscape(first["nextCursor"].(string))
	second := jsonObject(t, callSession(router, "GET", sessionBase+"?limit=2&cursor="+cursor, ""))
	if len(second["sessions"].([]any)) != 1 || second["nextCursor"] != nil {
		t.Fatalf("%+v", second)
	}
	if w := callSession(router, "GET", "/api/apps/test/users/other/sessions?cursor="+cursor, ""); w.Code != 422 {
		t.Fatal(w.Code)
	}
	for _, q := range []string{"limit=0", "limit=101", "limit=no", "cursor=bad", "limit=1&limit=2"} {
		if w := callSession(router, "GET", sessionBase+"?"+q, ""); w.Code != 422 {
			t.Fatal(q, w.Code)
		}
	}
	got, _ := store.Get(context.Background(), &session.GetRequest{AppName: "test", UserID: "user", SessionID: "s0"})
	for i := 0; i < 5; i++ {
		event := &session.Event{ID: fmt.Sprintf("e%d", i), Author: "model", Timestamp: time.Unix(100, 0), Output: map[string]any{"i": i}}
		event.ModelVersion = "version"
		if err := store.AppendEvent(context.Background(), got.Session, event); err != nil {
			t.Fatal(err)
		}
	}
	page := jsonObject(t, callSession(router, "GET", sessionBase+"/s0?limit=2", ""))
	events := page["events"].([]any)
	e := events[0].(map[string]any)
	if e["id"] != "e3" || e["modelVersion"] != "version" || e["output"] == nil || e["timestamp"] != nil {
		t.Fatalf("%+v", page)
	}
	// Append with the same timestamp between pages: earlier history must not shift.
	if err := store.AppendEvent(context.Background(), got.Session, &session.Event{ID: "e5", Author: "model", Timestamp: time.Unix(100, 0)}); err != nil {
		t.Fatal(err)
	}
	page = jsonObject(t, callSession(router, "GET", sessionBase+"/s0?limit=2&cursor="+url.QueryEscape(page["nextCursor"].(string)), ""))
	if page["events"].([]any)[0].(map[string]any)["id"] != "e1" {
		t.Fatalf("%+v", page)
	}
	if w := callSession(router, "POST", sessionBase+"/s0/files", ""); w.Code != 422 || !bytes.Contains(w.Body.Bytes(), []byte("SANDBOX_DISABLED")) {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestSessionImportPreservesADKFieldsAndRejectsReservedState(t *testing.T) {
	router := sessionRouter(t, session.InMemoryService())
	w := callSession(router, "POST", sessionBase, `{"sessionId":"import","events":[{"id":"event","author":"model","finishReason":"STOP","modelVersion":"v","output":{"result":42},"content":{"role":"model","parts":[{"functionCall":{"name":"test"}}]}}]}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	e := jsonObject(t, w)["events"].([]any)[0].(map[string]any)
	if e["finishReason"] != "STOP" || e["output"] == nil || e["timestamp"] != nil {
		t.Fatalf("%+v", e)
	}
	args := e["content"].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)["args"]
	if args == nil {
		t.Fatal("ADK requires empty function-call args object")
	}
	for _, body := range []string{`{"state":{"_veadk_aio_sandbox":{}}}`, `{"events":[{"actions":{"stateDelta":{"_veadk_aio_sandbox":{}}}}]}`} {
		if w := callSession(router, "POST", sessionBase, body); w.Code != 422 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := callSession(router, "POST", "/api/run", `{"stateDelta":{"_veadk_aio_sandbox":{}}}`); w.Code != 422 {
		t.Fatal(w.Code)
	}
}

func TestReservedRunStateUsesADKCaseInsensitiveFieldDecoding(t *testing.T) {
	router := sessionRouter(t, session.InMemoryService())
	w := callSession(router, "POST", "/api/run", `{"StateDelta":{"_veadk_aio_sandbox":{}}}`)
	if w.Code != 422 {
		t.Fatalf("reserved binding accepted through case alias: %d", w.Code)
	}
}

func TestPathSessionCreationRejectsBodyID(t *testing.T) {
	router := sessionRouter(t, session.InMemoryService())
	for _, body := range []string{`{"sessionId":"path"}`, `{"sessionId":"other"}`, `{"sessionId":""}`, `{"sessionId":null}`, `{"SessionID":"path"}`} {
		w := callSession(router, "POST", sessionBase+"/path", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: got %d: %s", body, w.Code, w.Body.String())
		}
	}
	if w := callSession(router, "GET", sessionBase+"/path", ""); w.Code != http.StatusNotFound {
		t.Fatalf("rejected requests created session: %d", w.Code)
	}
	w := callSession(router, "POST", sessionBase+"/path", `{"state":{"key":"value"},"events":[{"id":"imported","author":"user","content":{"role":"user","parts":[{"text":"hello"}]}}]}`)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	result := jsonObject(t, w)
	if result["id"] != "path" || result["state"].(map[string]any)["key"] != "value" || len(result["events"].([]any)) != 1 {
		t.Fatalf("unexpected session: %+v", result)
	}
}
