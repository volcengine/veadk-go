// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package agentkit_server_app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/v2/apps"
	"github.com/volcengine/veadk-go/v2/apps/agentkit_server_app/internal/adkcompat"
	"github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"google.golang.org/adk/v2/session"
)

// ADK does not expose its REST DTOs publicly. Keep the versioned adapter in
// internal/adkcompat, and use ADK's runtime handlers unchanged.
func sessionSummary(value session.Session) map[string]any {
	return map[string]any{"id": value.ID(), "appName": value.AppName(), "userId": value.UserID(), "lastUpdateTime": value.LastUpdateTime().Unix()}
}
func sessionState(value session.Session) map[string]any {
	state := map[string]any{}
	for k, v := range value.State().All() {
		if k != agentkit.AIOSandboxStateKey {
			state[k] = v
		}
	}
	return state
}
func adkSessionValue(value session.Session) map[string]any {
	state := sessionState(value)
	events := []adkcompat.Event{}
	for e := range value.Events().All() {
		if e.Author != "veadk_lifecycle" {
			events = append(events, publicEvent(e))
		}
	}
	return map[string]any{"id": value.ID(), "appName": value.AppName(), "userId": value.UserID(), "state": state, "events": events, "lastUpdateTime": value.LastUpdateTime().Unix()}
}
func publicEvent(e *session.Event) adkcompat.Event {
	result := adkcompat.FromSessionEvent(*e)
	if e.Actions.StateDelta != nil {
		result.Actions.StateDelta = map[string]any{}
		for k, v := range e.Actions.StateDelta {
			if k != agentkit.AIOSandboxStateKey {
				result.Actions.StateDelta[k] = v
			}
		}
	}
	return result
}
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func writeADKSession(w http.ResponseWriter, value session.Session) {
	writeJSON(w, adkSessionValue(value))
}

type pageCursor struct {
	Kind     string    `json:"k"`
	App      string    `json:"a"`
	User     string    `json:"u"`
	Session  string    `json:"s,omitempty"`
	Time     time.Time `json:"t,omitempty"`
	ID       string    `json:"i,omitempty"`
	Position int       `json:"p,omitempty"`
}

func cursorString(c pageCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func pageRequest(r *http.Request, kind, app, user, id string) (int, *pageCursor, error) {
	q := r.URL.Query()
	limit := 50
	if values, ok := q["limit"]; ok {
		if len(values) != 1 {
			return 0, nil, errors.New("invalid limit")
		}
		n, e := strconv.Atoi(values[0])
		if e != nil || n < 1 || n > 100 {
			return 0, nil, errors.New("limit must be between 1 and 100")
		}
		limit = n
	}
	if values, ok := q["cursor"]; ok {
		if len(values) != 1 || len(values[0]) > 4096 {
			return 0, nil, errors.New("invalid cursor")
		}
		b, e := base64.RawURLEncoding.DecodeString(values[0])
		if e != nil {
			return 0, nil, errors.New("invalid cursor")
		}
		var c pageCursor
		if json.Unmarshal(b, &c) != nil || c.Kind != kind || c.App != app || c.User != user || c.Session != id {
			return 0, nil, errors.New("cursor does not belong to this request")
		}
		return limit, &c, nil
	}
	return limit, nil, nil
}

func (a *AgentkitServerApp) setupSessionRoutes(router *mux.Router, cfg *apps.RunConfig) error {
	if cfg.SessionService == nil {
		return errors.New("session routes require SessionService")
	}
	base := strings.TrimRight(a.ApiPathPrefix, "/") + "/apps/{app_name}/users/{user_id}/sessions"
	validApp := func(w http.ResponseWriter, r *http.Request) bool {
		_, err := cfg.AgentLoader.LoadAgent(mux.Vars(r)["app_name"])
		if err != nil {
			http.Error(w, "unknown app", 404)
			return false
		}
		return true
	}
	// Serializes the local create/check/import transaction. AIO uses its lifecycle
	// coordinator, which also excludes run, delete and background work.
	var localCreate sync.Mutex
	create := func(w http.ResponseWriter, r *http.Request) {
		if !validApp(w, r) {
			return
		}
		v := mux.Vars(r)
		type createFields struct {
			State  map[string]any    `json:"state"`
			Events []adkcompat.Event `json:"events"`
		}
		var body createFields
		id := v["session_id"]
		var bodyID string
		var payload any = &body
		if id == "" {
			payload = &struct {
				*createFields
				SessionID *string `json:"sessionId"`
			}{createFields: &body, SessionID: &bodyID}
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 20<<20))
		d.DisallowUnknownFields()
		if err := d.Decode(payload); err != nil && err != io.EOF {
			http.Error(w, "invalid session request", 400)
			return
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			http.Error(w, "invalid session request", 400)
			return
		}
		if _, ok := body.State[agentkit.AIOSandboxStateKey]; ok {
			writeAIOError(w, agentkit.ErrAIOReservedState)
			return
		}
		events := make([]*session.Event, 0, len(body.Events))
		for _, e := range body.Events {
			if _, ok := e.Actions.StateDelta[agentkit.AIOSandboxStateKey]; ok {
				writeAIOError(w, agentkit.ErrAIOReservedState)
				return
			}
			if e.Author == "veadk_lifecycle" {
				http.Error(w, "reserved event author", 422)
				return
			}
			event := adkcompat.ToSessionEvent(e)
			event.Timestamp = time.Now()
			events = append(events, event)
		}
		if id == "" {
			id = bodyID
		}
		if id == "" {
			id = uuid.NewString()
		}
		var created session.Session
		var err error
		if a.aioLifecycle != nil {
			created, err = a.aioLifecycle.CreateWithEvents(r.Context(), v["app_name"], v["user_id"], id, body.State, events)
		} else {
			localCreate.Lock()
			defer localCreate.Unlock()
			var list *session.ListResponse
			list, err = cfg.SessionService.List(r.Context(), &session.ListRequest{AppName: v["app_name"], UserID: v["user_id"]})
			if err == nil {
				for _, s := range list.Sessions {
					if s.ID() == id {
						err = agentkit.ErrAIOSessionExists
						break
					}
				}
			}
			if err == nil {
				var response *session.CreateResponse
				response, err = cfg.SessionService.Create(r.Context(), &session.CreateRequest{AppName: v["app_name"], UserID: v["user_id"], SessionID: id, State: body.State})
				if err == nil {
					created = response.Session
					for _, e := range events {
						if err = cfg.SessionService.AppendEvent(r.Context(), created, e); err != nil {
							cleanup, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
							_ = cfg.SessionService.Delete(cleanup, &session.DeleteRequest{AppName: v["app_name"], UserID: v["user_id"], SessionID: id})
							cancel()
							break
						}
					}
				}
			}
		}
		if err != nil {
			writeAIOError(w, err)
			return
		}
		writeADKSession(w, created)
	}
	router.HandleFunc(base, create).Methods(http.MethodPost)
	router.HandleFunc(base+"/{session_id}", create).Methods(http.MethodPost)
	router.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		if !validApp(w, r) {
			return
		}
		v := mux.Vars(r)
		limit, c, err := pageRequest(r, "sessions", v["app_name"], v["user_id"], "")
		if err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
		listed, err := cfg.SessionService.List(r.Context(), &session.ListRequest{AppName: v["app_name"], UserID: v["user_id"]})
		if err != nil {
			writeAIOError(w, err)
			return
		}
		sort.Slice(listed.Sessions, func(i, j int) bool {
			a, b := listed.Sessions[i], listed.Sessions[j]
			if a.LastUpdateTime().Unix() == b.LastUpdateTime().Unix() {
				return a.ID() > b.ID()
			}
			return a.LastUpdateTime().Unix() > b.LastUpdateTime().Unix()
		})
		items := []any{}
		var next any
		var last session.Session
		for _, s := range listed.Sessions {
			if c != nil && (s.LastUpdateTime().Unix() > c.Time.Unix() || (s.LastUpdateTime().Unix() == c.Time.Unix() && s.ID() >= c.ID)) {
				continue
			}
			if len(items) == limit {
				next = cursorString(pageCursor{Kind: "sessions", App: v["app_name"], User: v["user_id"], Time: time.Unix(last.LastUpdateTime().Unix(), 0), ID: last.ID()})
				break
			}
			items = append(items, map[string]any{"id": s.ID(), "appName": s.AppName(), "userId": s.UserID(), "lastUpdateTime": s.LastUpdateTime().Unix(), "state": map[string]any{}, "events": []any{}})
			last = s
		}
		writeJSON(w, map[string]any{"sessions": items, "nextCursor": next})
	}).Methods(http.MethodGet)
	router.HandleFunc(base+"/{session_id}", func(w http.ResponseWriter, r *http.Request) {
		if !validApp(w, r) {
			return
		}
		v := mux.Vars(r)
		limit, c, err := pageRequest(r, "events", v["app_name"], v["user_id"], v["session_id"])
		if err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
		s, err := agentkit.GetADKSession(r.Context(), cfg.SessionService, v["app_name"], v["user_id"], v["session_id"])
		if err != nil {
			writeAIOError(w, err)
			return
		}
		end := s.Events().Len()
		if c != nil {
			if c.Position <= 0 || c.Position > end || s.Events().At(c.Position-1).ID != c.ID {
				http.Error(w, "stale or invalid event cursor", 422)
				return
			}
			end = c.Position
		}
		events := []adkcompat.Event{}
		start := end
		for start > 0 && len(events) < limit {
			start--
			e := s.Events().At(start)
			if e.Author != "veadk_lifecycle" {
				events = append(events, publicEvent(e))
			}
		}
		for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
			events[i], events[j] = events[j], events[i]
		}
		var next any
		for start > 0 && s.Events().At(start-1).Author == "veadk_lifecycle" {
			start--
		}
		if start > 0 {
			next = cursorString(pageCursor{Kind: "events", App: v["app_name"], User: v["user_id"], Session: v["session_id"], Position: start, ID: s.Events().At(start - 1).ID})
		}
		value := sessionSummary(s)
		value["state"] = sessionState(s)
		value["events"] = events
		value["nextCursor"] = next
		writeJSON(w, value)
	}).Methods(http.MethodGet)
	if a.aioLifecycle != nil {
		router.HandleFunc(base+"/{session_id}", func(w http.ResponseWriter, r *http.Request) {
			if !validApp(w, r) {
				return
			}
			v := mux.Vars(r)
			if err := a.aioLifecycle.Delete(r.Context(), v["app_name"], v["user_id"], v["session_id"]); err != nil {
				writeAIOError(w, err)
				return
			}
			writeJSON(w, nil)
		}).Methods(http.MethodDelete)
	}
	router.HandleFunc(base+"/{session_id}/files", a.uploadHandler(cfg)).Methods(http.MethodPost)
	return nil
}
