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
	"fmt"
	"strings"

	"google.golang.org/adk/v2/platform"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// SessionUpdateRequest describes one external update to an existing session.
// StateDelta, Content, and ArtifactDelta are appended as one standard ADK event.
type SessionUpdateRequest struct {
	// AppName identifies the application. Runner.UpdateSession always uses the
	// runner's configured app name; package-level UpdateSession requires this
	// field.
	AppName string `json:"app_name,omitempty"`
	UserID  string `json:"user_id,omitempty"`
	// SessionID identifies the existing session to update. Missing sessions are
	// not created, even when the runtime enables AutoCreateSession.
	SessionID string `json:"session_id,omitempty"`

	// StateDelta is merged into ADK state by top-level key. temp: keys are
	// discarded before validation and persistence.
	StateDelta map[string]any `json:"state_delta,omitempty"`
	// Content, when set, is appended to session history and may affect later
	// agent prompts. Author must be set when Content is present.
	Content *genai.Content `json:"content,omitempty"`
	// ArtifactDelta records artifact version references in the event. It does
	// not save artifact contents; callers must save artifacts through the
	// runtime ArtifactService first.
	ArtifactDelta map[string]int64 `json:"artifact_delta,omitempty"`

	// Author labels the external event. It defaults to "system" only for
	// state/artifact-only updates. Content updates must provide it explicitly.
	Author string `json:"author,omitempty"`
	// InvocationID optionally groups the event with a caller-provided invocation.
	// If empty, a new UUID is generated.
	InvocationID string `json:"invocation_id,omitempty"`
}

// UpdateSession appends one external update event using the runner's configured
// app name and session service, without executing the agent.
// See [UpdateSession] for state scopes, validation, and concurrency semantics.
func (r *Runner) UpdateSession(ctx context.Context, req SessionUpdateRequest) error {
	if r == nil {
		return fmt.Errorf("runner is required")
	}
	if req.AppName != "" && req.AppName != r.appName {
		return fmt.Errorf("app_name %q does not match runner app_name %q", req.AppName, r.appName)
	}
	req.AppName = r.appName
	return UpdateSession(ctx, r.sessionService, req)
}

// UpdateSession appends one external update event to an existing app/user/session
// through sessionService.Get and sessionService.AppendEvent. Pass the same
// service instance used by the runtime. It never creates a session, runs an
// agent, saves artifact content, or writes long-term memory.
//
// StateDelta keys are merged at the top level; a value replaces the entire
// previous value, and nil stores null rather than deleting a key. ADK app: and
// user: keys retain their shared scopes. temp: keys are discarded before JSON
// validation and do not enter state or history. Input maps are not modified.
//
// Content updates enter session history and can affect later prompts, so Author
// is required when Content is present. ArtifactDelta only records artifact
// filename/version references; save artifact bytes through ArtifactService before
// calling this method. If StateDelta, Content, and ArtifactDelta are all empty
// after temp filtering, the call verifies that the session exists and appends no
// event.
//
// Persistent values must be JSON serializable. Nonempty updates append one
// non-partial ADK event. Errors wrap the underlying cause; storage conflicts are
// returned without retrying.
// Serialize updates and agent runs for the same session: Get plus AppendEvent
// does not provide compare-and-swap or cross-request deduplication. The caller
// is responsible for authorization and keeping input values stable during use.
func UpdateSession(ctx context.Context, sessionService session.Service, req SessionUpdateRequest) error {
	if sessionService == nil {
		return fmt.Errorf("session service is required")
	}
	appName, userID, sessionID := req.AppName, req.UserID, req.SessionID
	if appName == "" || userID == "" || sessionID == "" {
		return fmt.Errorf("app_name, user_id, session_id are required, got app_name: %q, user_id: %q, session_id: %q", appName, userID, sessionID)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("update session %q canceled: %w", sessionID, err)
	}
	resp, err := sessionService.Get(ctx, &session.GetRequest{
		AppName: appName, UserID: userID, SessionID: sessionID,
	})
	if err != nil {
		return fmt.Errorf("get session %q for update failed: %w", sessionID, err)
	}
	if resp == nil || resp.Session == nil {
		return fmt.Errorf("get session %q for update returned no session", sessionID)
	}
	delta := persistentStateDelta(req.StateDelta)
	artifactDelta, err := persistentArtifactDelta(req.ArtifactDelta)
	if err != nil {
		return fmt.Errorf("validate artifact delta for session %q failed: %w", sessionID, err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("update session %q canceled: %w", sessionID, err)
	}
	if len(delta) == 0 && req.Content == nil && len(artifactDelta) == 0 {
		return nil
	}
	if err := validateSessionUpdatePayload(delta, artifactDelta, sessionID); err != nil {
		return err
	}
	content, err := cloneSessionUpdateContent(req.Content, sessionID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("update session %q canceled: %w", sessionID, err)
	}
	author := strings.TrimSpace(req.Author)
	if req.Content != nil && author == "" {
		return fmt.Errorf("author is required when content is set for session %q", sessionID)
	}
	if author == "" {
		author = "system"
	}
	invocationID := req.InvocationID
	if invocationID == "" {
		invocationID = platform.NewUUID(ctx)
	}
	event := session.NewEvent(ctx, invocationID)
	event.Author = author
	event.Content = content
	event.Actions.StateDelta = delta
	event.Actions.ArtifactDelta = artifactDelta
	if err := sessionService.AppendEvent(ctx, resp.Session, event); err != nil {
		return fmt.Errorf("append state update event for session %q failed: %w", sessionID, err)
	}
	return nil
}

func persistentStateDelta(stateDelta map[string]any) map[string]any {
	delta := make(map[string]any, len(stateDelta))
	for key, value := range stateDelta {
		if !strings.HasPrefix(key, session.KeyPrefixTemp) {
			delta[key] = value
		}
	}
	return delta
}

func persistentArtifactDelta(artifactDelta map[string]int64) (map[string]int64, error) {
	delta := make(map[string]int64, len(artifactDelta))
	for filename, version := range artifactDelta {
		if strings.TrimSpace(filename) == "" {
			return nil, fmt.Errorf("artifact filename is required")
		}
		if version < 0 {
			return nil, fmt.Errorf("artifact %q version must be non-negative", filename)
		}
		delta[filename] = version
	}
	return delta, nil
}

func cloneSessionUpdateContent(content *genai.Content, sessionID string) (*genai.Content, error) {
	if content == nil {
		return nil, nil
	}
	data, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("serialize content for session %q failed: %w", sessionID, err)
	}
	var cloned genai.Content
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, fmt.Errorf("clone content for session %q failed: %w", sessionID, err)
	}
	return &cloned, nil
}

func validateSessionUpdatePayload(stateDelta map[string]any, artifactDelta map[string]int64, sessionID string) error {
	if len(stateDelta) > 0 {
		if _, err := json.Marshal(stateDelta); err != nil {
			return fmt.Errorf("serialize state delta for session %q failed: %w", sessionID, err)
		}
	}
	if len(artifactDelta) > 0 {
		if _, err := json.Marshal(artifactDelta); err != nil {
			return fmt.Errorf("serialize artifact delta for session %q failed: %w", sessionID, err)
		}
	}
	return nil
}
