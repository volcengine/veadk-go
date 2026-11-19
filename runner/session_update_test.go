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

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/volcengine/veadk-go/v2/configs"
	vememory "github.com/volcengine/veadk-go/v2/memory"
	"github.com/volcengine/veadk-go/v2/memory/short_term_memory_backends"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	adkrunner "google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func newUpdateTestService(t *testing.T, backend string) session.Service {
	t.Helper()
	var cfg any
	if backend == "sqlite" {
		cfg = &short_term_memory_backends.SqliteBackendConfig{
			CommonDatabaseConfig: &configs.CommonDatabaseConfig{DBUrl: filepath.Join(t.TempDir(), "session.db")},
		}
	}
	svc, err := vememory.NewShortTermMemoryService(vememory.ShortTermBackendType(backend), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func createUpdateTestSession(t *testing.T, svc session.Service, app, user, id string) {
	t.Helper()
	_, err := svc.Create(context.Background(), &session.CreateRequest{
		AppName: app, UserID: user, SessionID: id,
		State: map[string]any{"phase": "created", "keep": true, "nested": map[string]any{"old": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func getUpdateTestSession(t *testing.T, svc session.Service, app, user, id string) session.Session {
	t.Helper()
	resp, err := svc.Get(context.Background(), &session.GetRequest{AppName: app, UserID: user, SessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Session
}

func assertUpdateState(t *testing.T, s session.Session, key string, want any) {
	t.Helper()
	got, err := s.State().Get(key)
	if err != nil {
		t.Fatalf("state %q: %v", key, err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("state %q = %s, want %s", key, gotJSON, wantJSON)
	}
}

// REQ-003–006/008: exercise real storage and fresh reads, including event history.
func TestUpdateSessionStateAndEvents(t *testing.T) {
	for _, backend := range []string{"local", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			svc := newUpdateTestService(t, backend)
			createUpdateTestSession(t, svc, "app", "user", "session")
			before := getUpdateTestSession(t, svc, "app", "user", "session")
			delta := map[string]any{
				"phase": "approved", "new": "value", "number": 12.5, "nullable": nil,
				"list": []any{"hello", true, 3.0}, "nested": map[string]any{"new": true}, "temp:scratch": "discard",
			}
			original, err := json.Marshal(delta)
			if err != nil {
				t.Fatal(err)
			}
			if err := UpdateSession(ctx, svc, "app", "user", "session", delta); err != nil {
				t.Fatal(err)
			}
			after := getUpdateTestSession(t, svc, "app", "user", "session")
			for key, value := range delta {
				if !strings.HasPrefix(key, session.KeyPrefixTemp) {
					assertUpdateState(t, after, key, value)
				}
			}
			assertUpdateState(t, after, "keep", true)
			if _, err := after.State().Get("temp:scratch"); !errors.Is(err, session.ErrStateKeyNotExist) {
				t.Fatalf("temp key persisted: %v", err)
			}
			if after.Events().Len() != 1 {
				t.Fatalf("events = %d, want 1", after.Events().Len())
			}
			event := after.Events().At(0)
			if event.ID == "" || event.InvocationID == "" || event.Timestamp.IsZero() || event.Author != "system" || event.Partial || event.Content != nil {
				t.Fatalf("invalid external update event: %+v", event)
			}
			if event.Timestamp.Before(before.LastUpdateTime().Truncate(time.Microsecond)) || !after.LastUpdateTime().Equal(event.Timestamp) {
				t.Fatal("update timestamp does not match persisted event")
			}
			if _, ok := event.Actions.StateDelta["temp:scratch"]; ok {
				t.Fatal("temp key entered persisted event history")
			}
			unchanged, err := json.Marshal(delta)
			if err != nil || string(unchanged) != string(original) {
				t.Fatal("input map was modified")
			}
			// Changing the caller's top-level map after return must not change stored state/history.
			delta["phase"] = "caller_changed"
			assertUpdateState(t, getUpdateTestSession(t, svc, "app", "user", "session"), "phase", "approved")
			if event.Actions.StateDelta["phase"] != "approved" {
				t.Fatal("stored event aliases caller map")
			}
			if err := UpdateSession(ctx, svc, "app", "user", "session", map[string]any{"phase": "completed"}); err != nil {
				t.Fatal(err)
			}
			latest := getUpdateTestSession(t, svc, "app", "user", "session")
			assertUpdateState(t, latest, "phase", "completed")
			assertUpdateState(t, latest, "new", "value")
			if latest.Events().Len() != 2 || latest.Events().At(1).ID == event.ID || latest.Events().At(1).InvocationID == event.InvocationID {
				t.Fatal("sequential updates must have distinct events")
			}
			for _, empty := range []map[string]any{nil, {}, {"temp:scratch": make(chan int)}} {
				if err := UpdateSession(ctx, svc, "app", "user", "session", empty); err != nil {
					t.Fatal(err)
				}
				fresh := getUpdateTestSession(t, svc, "app", "user", "session")
				if fresh.Events().Len() != 2 || !fresh.LastUpdateTime().Equal(latest.LastUpdateTime()) {
					t.Fatal("empty update changed history or timestamp")
				}
			}
		})
	}
}

// REQ-002/005: ordinary keys are isolated, app/user keys preserve ADK sharing.
func TestUpdateSessionScopes(t *testing.T) {
	for _, backend := range []string{"local", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			svc := newUpdateTestService(t, backend)
			for _, target := range [][3]string{{"app", "user", "s1"}, {"app", "user", "s2"}, {"app", "other", "s1"}, {"other", "user", "s1"}} {
				createUpdateTestSession(t, svc, target[0], target[1], target[2])
			}
			if err := UpdateSession(context.Background(), svc, "app", "user", "s1", map[string]any{
				"phase": "approved", "app:brand": "veadk", "user:locale": "zh",
			}); err != nil {
				t.Fatal(err)
			}
			assertUpdateState(t, getUpdateTestSession(t, svc, "app", "user", "s1"), "phase", "approved")
			sameUser := getUpdateTestSession(t, svc, "app", "user", "s2")
			assertUpdateState(t, sameUser, "phase", "created")
			assertUpdateState(t, sameUser, "user:locale", "zh")
			assertUpdateState(t, sameUser, "app:brand", "veadk")
			otherUser := getUpdateTestSession(t, svc, "app", "other", "s1")
			assertUpdateState(t, otherUser, "phase", "created")
			assertUpdateState(t, otherUser, "app:brand", "veadk")
			if _, err := otherUser.State().Get("user:locale"); !errors.Is(err, session.ErrStateKeyNotExist) {
				t.Fatal("user state crossed users")
			}
			otherApp := getUpdateTestSession(t, svc, "other", "user", "s1")
			assertUpdateState(t, otherApp, "phase", "created")
			for _, key := range []string{"user:locale", "app:brand"} {
				if _, err := otherApp.State().Get(key); !errors.Is(err, session.ErrStateKeyNotExist) {
					t.Fatal("shared state crossed apps")
				}
			}
			if sameUser.Events().Len() != 0 || otherUser.Events().Len() != 0 || otherApp.Events().Len() != 0 {
				t.Fatal("update event was appended to another session")
			}
		})
	}
}

// REQ-001/008: UpdateSession and Run use the normalized service; no agent or
// long-term memory call occurs as a side effect of updating alone.
func TestRunnerUpdateSessionUsesRuntimeService(t *testing.T) {
	for _, mode := range []string{"default", "short_term", "session", "both"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			a, err := agent.New(agent.Config{Name: "state_reader", Run: func(ic agent.InvocationContext) iter.Seq2[*session.Event, error] {
				return func(yield func(*session.Event, error) bool) {
					calls++
					phase, err := ic.Session().State().Get("phase")
					if err != nil {
						yield(nil, err)
						return
					}
					event := session.NewEvent(ic, ic.InvocationID())
					event.Author = "state_reader"
					event.LLMResponse = model.LLMResponse{Content: genai.NewContentFromText(fmt.Sprint(phase), genai.RoleModel)}
					yield(event, nil)
				}
			}})
			if err != nil {
				t.Fatal(err)
			}
			longTerm := &recordingMemoryService{}
			cfg := Config{Agent: a, MemoryService: longTerm, AutoSaveSession: true, AutoCreateSession: true}
			wantApp := defaultAppName
			if mode != "default" {
				cfg.AppName = "custom_app"
				wantApp = cfg.AppName
			}
			var selected, ignored session.Service
			switch mode {
			case "short_term":
				selected = session.InMemoryService()
				cfg.ShortTermMemory = selected
			case "session":
				selected = session.InMemoryService()
				cfg.SessionService = selected
			case "both":
				selected, ignored = session.InMemoryService(), session.InMemoryService()
				cfg.SessionService, cfg.ShortTermMemory = selected, ignored
			}
			r, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if selected == nil {
				selected = r.SessionService()
			}
			if r.SessionService() != selected || r.AppName() != wantApp {
				t.Fatal("runtime service or normalized app name differs")
			}
			createUpdateTestSession(t, selected, r.AppName(), "user", "session")
			if ignored != nil {
				createUpdateTestSession(t, ignored, r.AppName(), "user", "session")
			}
			if err := r.UpdateSession(context.Background(), "user", "session", map[string]any{"phase": "approved"}); err != nil {
				t.Fatal(err)
			}
			if calls != 0 || len(longTerm.sessions) != 0 {
				t.Fatal("update executed agent or saved long-term memory")
			}
			assertUpdateState(t, getUpdateTestSession(t, selected, r.AppName(), "user", "session"), "phase", "approved")
			if ignored != nil {
				assertUpdateState(t, getUpdateTestSession(t, ignored, r.AppName(), "user", "session"), "phase", "created")
			}
			text, err := r.RunText(context.Background(), "user", "session", "read", agent.RunConfig{})
			if err != nil || text != "approved" || calls != 1 {
				t.Fatalf("next run = %q, %v, calls=%d", text, err, calls)
			}
			text, err = r.RunText(context.Background(), "user", "session", "read", agent.RunConfig{}, adkrunner.WithStateDelta(map[string]any{"phase": "run_delta"}))
			if err != nil || text != "run_delta" {
				t.Fatalf("WithStateDelta = %q, %v", text, err)
			}
			for _, delta := range []map[string]any{nil, {"phase": "approved"}, {"temp:key": true}} {
				if err := r.UpdateSession(context.Background(), "user", "missing", delta); err == nil {
					t.Fatal("missing session accepted")
				}
			}
			if _, err := selected.Get(context.Background(), &session.GetRequest{AppName: r.AppName(), UserID: "user", SessionID: "missing"}); err == nil {
				t.Fatal("update used AutoCreateSession")
			}
		})
	}
}

type updateRecordingService struct {
	session.Service
	getFn    func(context.Context, *session.GetRequest) (*session.GetResponse, error)
	appendFn func(context.Context, session.Session, *session.Event) error
	gets     int
	appends  int
}

func (s *updateRecordingService) Get(ctx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
	s.gets++
	if s.getFn != nil {
		return s.getFn(ctx, req)
	}
	return s.Service.Get(ctx, req)
}

func (s *updateRecordingService) AppendEvent(ctx context.Context, target session.Session, event *session.Event) error {
	s.appends++
	if s.appendFn != nil {
		return s.appendFn(ctx, target, event)
	}
	return s.Service.AppendEvent(ctx, target, event)
}

// REQ-002/007: invalid parameters fail before accessing the service.
func TestUpdateSessionInvalidParameters(t *testing.T) {
	for _, target := range [][3]string{{"", "user", "session"}, {"app", "", "session"}, {"app", "user", ""}} {
		svc := &updateRecordingService{}
		if err := UpdateSession(context.Background(), svc, target[0], target[1], target[2], nil); err == nil || !strings.Contains(err.Error(), "required") {
			t.Fatalf("invalid tuple accepted: %v", err)
		}
		if svc.gets != 0 || svc.appends != 0 {
			t.Fatal("invalid input accessed service")
		}
	}
	if err := UpdateSession(context.Background(), nil, "app", "user", "session", nil); err == nil {
		t.Fatal("nil service accepted")
	}
	for _, r := range []*Runner{nil, {}} {
		if err := r.UpdateSession(context.Background(), "user", "session", nil); err == nil {
			t.Fatal("invalid runner accepted")
		}
	}
}

// REQ-003/007: JSON rejection must not produce an event on either backend.
func TestUpdateSessionRejectsNonJSONValues(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	for _, backend := range []string{"local", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			base := newUpdateTestService(t, backend)
			createUpdateTestSession(t, base, "app", "user", "session")
			before := getUpdateTestSession(t, base, "app", "user", "session")
			for _, value := range []any{func() {}, make(chan int), cycle, math.NaN(), math.Inf(1)} {
				svc := &updateRecordingService{Service: base}
				err := UpdateSession(context.Background(), svc, "app", "user", "session", map[string]any{"phase": "wrong", "invalid": value})
				var typeErr *json.UnsupportedTypeError
				var valueErr *json.UnsupportedValueError
				if !errors.As(err, &typeErr) && !errors.As(err, &valueErr) {
					t.Fatalf("expected wrapped JSON error, got %v", err)
				}
				if svc.appends != 0 {
					t.Fatal("invalid delta was appended")
				}
				after := getUpdateTestSession(t, base, "app", "user", "session")
				assertUpdateState(t, after, "phase", "created")
				if after.Events().Len() != 0 || !after.LastUpdateTime().Equal(before.LastUpdateTime()) {
					t.Fatal("serialization failure changed storage")
				}
			}
		})
	}
}

// REQ-007: preserve error causes, context, and one-attempt append semantics.
func TestUpdateSessionErrorsAndCancellation(t *testing.T) {
	base := session.InMemoryService()
	createUpdateTestSession(t, base, "app", "user", "session")
	cause := errors.New("storage failure")
	for _, stage := range []string{"get", "append", "nil_response", "nil_session", "canceled_before_get", "canceled_after_get", "canceled_empty", "canceled_after_validation", "deadline"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			svc := &updateRecordingService{Service: base}
			delta := map[string]any{"phase": "wrong"}
			var want error
			wantGets, wantAppends := 1, 0
			switch stage {
			case "get":
				want = cause
				svc.getFn = func(context.Context, *session.GetRequest) (*session.GetResponse, error) { return nil, cause }
			case "append":
				want, wantAppends = cause, 1
				svc.appendFn = func(gotCtx context.Context, target session.Session, _ *session.Event) error {
					if gotCtx != ctx || target.ID() != "session" || target.AppName() != "app" || target.UserID() != "user" {
						t.Fatal("context or target not forwarded")
					}
					return cause
				}
			case "nil_response":
				svc.getFn = func(context.Context, *session.GetRequest) (*session.GetResponse, error) { return nil, nil }
			case "nil_session":
				svc.getFn = func(context.Context, *session.GetRequest) (*session.GetResponse, error) {
					return &session.GetResponse{}, nil
				}
			case "canceled_before_get":
				cancel()
				want, wantGets = context.Canceled, 0
			case "deadline":
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer deadlineCancel()
				want, wantGets = context.DeadlineExceeded, 0
			case "canceled_after_get", "canceled_empty":
				want = context.Canceled
				if stage == "canceled_empty" {
					delta = nil
				}
				svc.getFn = func(gotCtx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
					resp, err := base.Get(gotCtx, req)
					cancel()
					return resp, err
				}
			case "canceled_after_validation":
				want = context.Canceled
				delta["cancel"] = updateCancelMarshaler{cancel: cancel}
			}
			err := UpdateSession(ctx, svc, "app", "user", "session", delta)
			if err == nil || (want != nil && !errors.Is(err, want)) {
				t.Fatalf("error = %v, want cause %v", err, want)
			}
			if !reflect.DeepEqual([]int{svc.gets, svc.appends}, []int{wantGets, wantAppends}) {
				t.Fatalf("calls = get %d append %d, want %d/%d", svc.gets, svc.appends, wantGets, wantAppends)
			}
			after := getUpdateTestSession(t, base, "app", "user", "session")
			assertUpdateState(t, after, "phase", "created")
			if after.Events().Len() != 0 {
				t.Fatal("failed update entered storage")
			}
		})
	}
}

type updateCancelMarshaler struct{ cancel context.CancelFunc }

func (v updateCancelMarshaler) MarshalJSON() ([]byte, error) {
	v.cancel()
	return []byte("true"), nil
}
