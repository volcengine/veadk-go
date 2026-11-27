// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package agentkit_server_app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"github.com/volcengine/veadk-go/v2/log"
	"google.golang.org/adk/v2/agent"
)

// aioRunHandler protects reserved state and holds the lifecycle lock until the ADK
// handler has finished, including the final SSE frame.
func (a *AgentkitServerApp) aioRunHandler(next http.Handler, loader agent.Loader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, strings.TrimRight(a.ApiPathPrefix, "/"))
		if r.Method == http.MethodOptions || (path != "/run" && path != "/run_sse" && path != "/run_live") {
			next.ServeHTTP(w, r)
			return
		}
		var app, user, id string
		if path == "/run_live" {
			app, user, id = r.URL.Query().Get("appName"), r.URL.Query().Get("userId"), r.URL.Query().Get("sessionId")
			if app == "" {
				app = r.URL.Query().Get("app_name")
			}
			if user == "" {
				user = r.URL.Query().Get("user_id")
			}
			if id == "" {
				id = r.URL.Query().Get("session_id")
			}
		} else {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				var large *http.MaxBytesError
				if errors.As(err, &large) {
					http.Error(w, "run request exceeds configured body limit", http.StatusRequestEntityTooLarge)
					return
				}
				http.Error(w, "invalid run request", http.StatusBadRequest)
				return
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(data, &body); err != nil {
				http.Error(w, "invalid run request", http.StatusBadRequest)
				return
			}
			for _, name := range []string{"app_name", "user_id", "session_id", "new_message", "state_delta", "function_call_event_id", "model_name", "thinking_level"} {
				if _, ok := body[name]; ok {
					http.Error(w, "unknown run field "+name, http.StatusBadRequest)
					return
				}
			}
			for key, raw := range body {
				if !strings.EqualFold(key, "stateDelta") {
					continue
				}
				var delta map[string]any
				if json.Unmarshal(raw, &delta) != nil {
					http.Error(w, "invalid stateDelta", http.StatusUnprocessableEntity)
					return
				}
				if _, forbidden := delta[agentkit.AIOSandboxStateKey]; forbidden {
					http.Error(w, "sandbox binding is server-owned", http.StatusUnprocessableEntity)
					return
				}
			}
			_ = json.Unmarshal(body["appName"], &app)
			_ = json.Unmarshal(body["userId"], &user)
			_ = json.Unmarshal(body["sessionId"], &id)
			r.Body = io.NopCloser(bytes.NewReader(data))
		}
		if a.aioLifecycle == nil {
			next.ServeHTTP(w, r)
			return
		}
		if _, err := loader.LoadAgent(app); err != nil || app == "" || user == "" || id == "" {
			http.Error(w, "unknown app or missing session identifiers", http.StatusNotFound)
			return
		}
		forwarded := false
		if err := a.aioLifecycle.WithSessionContext(r.Context(), app, user, id, func(ctx context.Context) error {
			forwarded = true
			next.ServeHTTP(w, r.WithContext(ctx))
			return nil
		}); err != nil {
			if forwarded {
				log.Warnf("AIO request ended with lifecycle coordination error: %v", err)
				if strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") && r.Context().Err() == nil {
					_, _ = io.WriteString(w, "event: error\ndata: {\"error\":\"session lifecycle bookkeeping failed\"}\n\n")
					_ = http.NewResponseController(w).Flush()
				}
				return
			}
			writeAIOError(w, err)
		}
	})
}

func writeAIOError(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	message := "session or sandbox unavailable"
	switch {
	case errors.Is(err, agentkit.ErrAIOSessionBusy):
		status = http.StatusConflict
		message = "SESSION_BUSY"
	case errors.Is(err, agentkit.ErrAIOSessionExists):
		status = http.StatusConflict
		message = "session already exists"
	case errors.Is(err, agentkit.ErrAIOSessionNotFound):
		status = http.StatusNotFound
		message = "session not found"
	case errors.Is(err, agentkit.ErrAIOReservedState):
		status = http.StatusUnprocessableEntity
		message = "sandbox binding is server-owned"
	}
	http.Error(w, message, status)
}
