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
)

// UpdateSession merges stateDelta into an existing session using the runner's
// configured app name and session service, without executing the agent.
// See [UpdateSession] for state scopes, validation, and concurrency semantics.
func (r *Runner) UpdateSession(ctx context.Context, userID, sessionID string, stateDelta map[string]any) error {
	if r == nil {
		return fmt.Errorf("runner is required")
	}
	return UpdateSession(ctx, r.sessionService, r.appName, userID, sessionID, stateDelta)
}

// UpdateSession merges stateDelta into an existing app/user/session through
// sessionService.Get and a single sessionService.AppendEvent. Pass the same
// service instance used by the runtime. It never creates a session or runs an
// agent, even if the runtime enables AutoCreateSession.
//
// Keys are merged at the top level; a value replaces the entire previous value,
// and nil stores null rather than deleting a key. ADK app: and user: keys retain
// their shared scopes. temp: keys are discarded before JSON validation and do
// not enter state or history. The input map is not modified. A nil, empty, or
// temp-only delta verifies that the session exists but appends no event.
//
// Persistent values must be JSON serializable. Nonempty updates append one
// non-partial ADK event with author "system" and no message content. Errors wrap
// the underlying cause; storage conflicts are returned without retrying.
// Serialize updates and agent runs for the same session: Get plus AppendEvent
// does not provide compare-and-swap or cross-request deduplication. The caller
// is responsible for authorization and keeping input values stable during use.
func UpdateSession(ctx context.Context, sessionService session.Service, appName, userID, sessionID string, stateDelta map[string]any) error {
	if sessionService == nil {
		return fmt.Errorf("session service is required")
	}
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
	delta := make(map[string]any, len(stateDelta))
	for key, value := range stateDelta {
		if !strings.HasPrefix(key, session.KeyPrefixTemp) {
			delta[key] = value
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("update session %q canceled: %w", sessionID, err)
	}
	if len(delta) == 0 {
		return nil
	}
	if _, err := json.Marshal(delta); err != nil {
		return fmt.Errorf("serialize state delta for session %q failed: %w", sessionID, err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("update session %q canceled: %w", sessionID, err)
	}
	event := session.NewEvent(ctx, platform.NewUUID(ctx))
	event.Author = "system"
	event.Actions.StateDelta = delta
	if err := sessionService.AppendEvent(ctx, resp.Session, event); err != nil {
		return fmt.Errorf("append state update event for session %q failed: %w", sessionID, err)
	}
	return nil
}
