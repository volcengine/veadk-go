// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package agentkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
)

const AIOSandboxStateKey = "_veadk_aio_sandbox"

const aioSessionTTL = 1800

// AIOBinding contains only references; endpoints and credentials are resolved
// afresh from the control plane for each invocation.
type AIOBinding struct {
	ToolID        string `json:"tool_id"`
	SessionID     string `json:"session_id"`
	Identity      string `json:"identity"`
	SkillSpaceIDs string `json:"skill_space_ids,omitempty"`
}

type aioSession struct {
	ID, UserID, Status, Endpoint, InternalEndpoint, ExpireAt, CreatedAt string
}

var aioSessionLocks [64]sync.Mutex

func aioIdentity(appName, userID, sessionID string) string {
	data, _ := json.Marshal([]string{appName, userID, sessionID})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func aioPhysicalPrefix(identity, skillSpaceIDs string) string {
	prefix := "veadk-aio-" + identity
	if skillSpaceIDs != "" {
		sum := sha256.Sum256([]byte(skillSpaceIDs))
		prefix += "_s_" + hex.EncodeToString(sum[:6])
	}
	return prefix
}

func bindingFromState(state session.ReadonlyState) (*AIOBinding, error) {
	v, err := state.Get(AIOSandboxStateKey)
	if errors.Is(err, session.ErrStateKeyNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("invalid AIO sandbox binding")
	}
	var binding AIOBinding
	if err := json.Unmarshal(data, &binding); err != nil || binding.ToolID == "" || binding.SessionID == "" || binding.Identity == "" {
		return nil, fmt.Errorf("invalid AIO sandbox binding")
	}
	return &binding, nil
}

func bindingValue(binding AIOBinding) map[string]string {
	return map[string]string{
		"tool_id": binding.ToolID, "session_id": binding.SessionID,
		"identity": binding.Identity, "skill_space_ids": binding.SkillSpaceIDs,
	}
}

func isMissingAIOSession(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == "InvalidResource.NotFound"
}

func aioToolID() string { return strings.TrimSpace(os.Getenv("AGENTKIT_TOOL_ID")) }

// EnsureAIOSandbox binds an ADK session before any model or tool execution.
// Concurrent binding attempts for the same session are serialized locally.
func EnsureAIOSandbox(ctx agent.Context) (*AIOBinding, error) {
	return EnsureAIOSandboxWithSkills(ctx, nil)
}

// EnsureAIOSandboxWithSkills passes the Agent's pinned Skill Space selection
// to a newly created sandbox and rejects reuse with a different selection.
func EnsureAIOSandboxWithSkills(ctx agent.Context, skillSpaceIDs []string) (*AIOBinding, error) {
	if ctx == nil {
		return nil, errors.New("AIO sandbox requires app, user and session identity")
	}
	if aioToolID() == "" {
		return nil, errors.New("AGENTKIT_TOOL_ID is required for aio_sandbox")
	}
	client, err := NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve AgentKit client: %w", err)
	}
	return EnsureAIOBindingWithSkills(ctx, client, ctx.State(), ctx.AppName(), ctx.UserID(), ctx.SessionID(), skillSpaceIDs)
}

// EnsureAIOBinding is shared by Runner callbacks and session-creation handlers.
// The caller owns persisting state in its configured ADK SessionService.
func EnsureAIOBinding(ctx context.Context, client *Client, state session.State, appName, userID, sessionID string) (*AIOBinding, error) {
	return EnsureAIOBindingWithSkills(ctx, client, state, appName, userID, sessionID, nil)
}

// EnsureAIOBindingWithSkills is also available to session-creation handlers.
func EnsureAIOBindingWithSkills(ctx context.Context, client *Client, state session.State, appName, userID, sessionID string, skillSpaceIDs []string) (*AIOBinding, error) {
	return ensureAIOBindingWithSkillsTTL(ctx, client, state, appName, userID, sessionID, skillSpaceIDs, aioSessionTTL)
}

func ensureAIOBindingWithSkillsTTL(ctx context.Context, client *Client, state session.State, appName, userID, sessionID string, skillSpaceIDs []string, ttlSeconds int) (*AIOBinding, error) {
	if ctx == nil || client == nil || state == nil || appName == "" || userID == "" || sessionID == "" {
		return nil, errors.New("AIO sandbox requires client, state, app, user and session identity")
	}
	toolID := aioToolID()
	if toolID == "" {
		return nil, errors.New("AGENTKIT_TOOL_ID is required for aio_sandbox")
	}
	identity := aioIdentity(appName, userID, sessionID)
	skillSpaceValue := strings.Join(skillSpaceIDs, ",")
	sum := sha256.Sum256([]byte(toolID + "\x00" + identity))
	lock := &aioSessionLocks[int(sum[0])%len(aioSessionLocks)]
	lock.Lock()
	defer lock.Unlock()
	var staleID string
	if existing, err := bindingFromState(state); err != nil {
		return nil, err
	} else if existing != nil {
		if existing.Identity != identity || existing.ToolID != toolID || existing.SkillSpaceIDs != skillSpaceValue {
			return nil, errors.New("AIO sandbox binding does not match this session")
		}
		if _, err := readyAIOSessionTTL(ctx, client, *existing, ttlSeconds); err == nil {
			return existing, nil
		} else if !isMissingAIOSession(err) && !errors.Is(err, errExpiredAIOSession) {
			return nil, err
		}
		staleID = existing.SessionID
	}
	userSessionID := aioPhysicalPrefix(identity, skillSpaceValue)
	candidates, err := listAIOSessions(ctx, client, toolID, userSessionID)
	if err != nil {
		return nil, err
	}
	var chosen *aioSession
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.ID == staleID {
			continue
		}
		if candidate.UserID != userSessionID && !strings.HasPrefix(candidate.UserID, userSessionID+"_r_") {
			continue
		}
		status := strings.ToLower(candidate.Status)
		if status != "ready" && status != "starting" && status != "paused" && status != "pausing" {
			continue
		}
		if expired(candidate.ExpireAt) {
			continue
		}
		if chosen == nil || candidate.CreatedAt > chosen.CreatedAt {
			chosen = candidate
		}
	}
	if chosen == nil {
		physicalID := userSessionID
		if len(candidates) != 0 {
			var ids []string
			for _, candidate := range candidates {
				ids = append(ids, candidate.ID)
			}
			data, _ := json.Marshal(ids)
			hash := sha256.Sum256(data)
			physicalID += "_r_" + hex.EncodeToString(hash[:6])
		}
		body := map[string]any{"ToolId": toolID, "UserSessionId": physicalID, "Ttl": ttlSeconds}
		if skillSpaceValue != "" {
			body["Envs"] = []map[string]string{{"Key": "SKILL_SPACE_ID", "Value": skillSpaceValue}}
		}
		created, createErr := client.DoContext(ctx, "CreateSession", body)
		if createErr == nil {
			value := parseAIOSession(created)
			chosen = &value
		} else {
			// The response may have been lost after creation. Recover by the exact
			// physical ID; never retry the mutation blindly.
			recovered, recoveryErr := listAIOSessions(ctx, client, toolID, userSessionID)
			if recoveryErr != nil {
				return nil, fmt.Errorf("CreateSession result unknown: %w", createErr)
			}
			for i := range recovered {
				if recovered[i].UserID == physicalID && (strings.EqualFold(recovered[i].Status, "ready") || strings.EqualFold(recovered[i].Status, "starting") || strings.EqualFold(recovered[i].Status, "paused")) {
					chosen = &recovered[i]
					break
				}
			}
			if chosen == nil {
				return nil, fmt.Errorf("CreateSession failed or result unknown: %w", createErr)
			}
		}
	}
	if chosen.ID == "" {
		return nil, errors.New("AgentKit session response has no SessionId")
	}
	binding := &AIOBinding{ToolID: toolID, SessionID: chosen.ID, Identity: identity, SkillSpaceIDs: skillSpaceValue}
	if _, err := readyAIOSessionTTL(ctx, client, *binding, ttlSeconds); err != nil {
		return nil, err
	}
	if err := state.Set(AIOSandboxStateKey, bindingValue(*binding)); err != nil {
		return nil, fmt.Errorf("persist AIO sandbox binding: %w", err)
	}
	return binding, nil
}

var errExpiredAIOSession = errors.New("bound AIO sandbox has expired")

// EnsureAIOToolEndpoint lazily prepares a sandbox when an AIO tool executes.
// Managed invocations share a binding under the lifecycle lease. Tools persist
// through ADK state deltas; the lifecycle flushes any remaining binding when
// execution ends, without invalidating ADK's active database session snapshot.
func EnsureAIOToolEndpoint(ctx agent.Context, skillSpaceIDs []string) (string, error) {
	if ctx == nil {
		return "", errors.New("AIO sandbox requires invocation context")
	}
	if lc, ok := ctx.Value(lifecycleContextKey{}).(lifecycleContext); ok {
		if lc.entry.app != ctx.AppName() || lc.entry.user != ctx.UserID() || lc.entry.id != ctx.SessionID() {
			return "", errors.New("AIO lifecycle context does not match tool session")
		}
		if strings.Join(skillSpaceIDs, ",") != strings.Join(lc.manager.cfg.SkillSpaceIDs, ",") {
			return "", errors.New("AIO tool Skill Space IDs do not match lifecycle configuration")
		}
		lc.tools.mu.Lock()
		defer lc.tools.mu.Unlock()
		if lc.tools.state == nil {
			s, err := GetADKSession(ctx, lc.manager.cfg.SessionService, ctx.AppName(), ctx.UserID(), ctx.SessionID())
			if err != nil {
				return "", err
			}
			state := &aioMapState{values: map[string]any{}}
			if binding, err := bindingFromState(s.State()); err != nil {
				return "", err
			} else if binding != nil {
				state.values[AIOSandboxStateKey] = bindingValue(*binding)
			}
			lc.tools.state = state
		}
		client, err := lc.manager.client()
		if err != nil {
			return "", err
		}
		binding, err := ensureAIOBindingWithSkillsTTL(ctx, client, lc.tools.state, ctx.AppName(), ctx.UserID(), ctx.SessionID(), skillSpaceIDs, lc.manager.sessionTTLSeconds())
		if err != nil {
			return "", err
		}
		if err := ctx.State().Set(AIOSandboxStateKey, bindingValue(*binding)); err != nil {
			return "", err
		}
		lc.entry.pausedAt = time.Time{}
		lc.entry.deleted = false
		lc.entry.lastActivity = time.Now()
		return ResolveAIOEndpointFor(ctx, client, lc.tools.state, ctx.AppName(), ctx.UserID(), ctx.SessionID())
	}
	if _, err := EnsureAIOSandboxWithSkills(ctx, skillSpaceIDs); err != nil {
		return "", err
	}
	client, err := NewClient(ctx)
	if err != nil {
		return "", err
	}
	return ResolveAIOEndpointFor(ctx, client, ctx.State(), ctx.AppName(), ctx.UserID(), ctx.SessionID())
}

// ResolveAIOEndpoint validates an existing binding. Missing or expired
// bindings never create a new empty sandbox.
func ResolveAIOEndpoint(ctx agent.Context) (string, error) {
	if ctx == nil {
		return "", errors.New("AIO sandbox requires invocation context")
	}
	client, err := NewClient(ctx)
	if err != nil {
		return "", err
	}
	return ResolveAIOEndpointFor(ctx, client, ctx.ReadonlyState(), ctx.AppName(), ctx.UserID(), ctx.SessionID())
}

// ResolveAIOEndpointFor reads and validates a stored binding without creating
// a replacement sandbox. It can be used by non-Agent request handlers.
func ResolveAIOEndpointFor(ctx context.Context, client *Client, state session.ReadonlyState, appName, userID, sessionID string) (string, error) {
	if ctx == nil || client == nil || state == nil || appName == "" || userID == "" || sessionID == "" {
		return "", errors.New("AIO sandbox requires client, state, app, user and session identity")
	}
	binding, err := bindingFromState(state)
	if err != nil {
		return "", err
	}
	if binding == nil || binding.Identity != aioIdentity(appName, userID, sessionID) || binding.ToolID != aioToolID() {
		return "", errors.New("AIO sandbox is not bound to this session")
	}
	info, err := readyAIOSession(ctx, client, *binding)
	if err != nil {
		return "", err
	}
	endpoint := info.Endpoint
	if endpoint == "" {
		endpoint = info.InternalEndpoint
	}
	if endpoint == "" {
		return "", errors.New("bound AIO sandbox has no endpoint")
	}
	return endpoint, nil
}

func checkedAIOBinding(state session.ReadonlyState, appName, userID, sessionID string) (*AIOBinding, error) {
	binding, err := bindingFromState(state)
	if err != nil {
		return nil, err
	}
	if binding == nil || binding.Identity != aioIdentity(appName, userID, sessionID) || binding.ToolID != aioToolID() {
		return nil, errors.New("AIO sandbox is not bound to this session")
	}
	return binding, nil
}

// PauseAIOBinding pauses a physical sandbox and confirms its final state.
func PauseAIOBinding(ctx context.Context, client *Client, state session.ReadonlyState, appName, userID, sessionID string) error {
	binding, err := checkedAIOBinding(state, appName, userID, sessionID)
	if err != nil {
		return err
	}
	check := func(checkCtx context.Context) (string, error) {
		response, err := client.DoContext(checkCtx, "GetSession", map[string]any{"ToolId": binding.ToolID, "SessionId": binding.SessionID})
		if err != nil {
			return "", err
		}
		info := parseAIOSession(response)
		if info.ID != binding.SessionID || info.UserID != aioPhysicalPrefix(binding.Identity, binding.SkillSpaceIDs) && !strings.HasPrefix(info.UserID, aioPhysicalPrefix(binding.Identity, binding.SkillSpaceIDs)+"_r_") {
			return "", errors.New("bound AIO sandbox belongs to another ADK session")
		}
		return strings.ToLower(info.Status), nil
	}
	status, err := check(ctx)
	if err != nil {
		return err
	}
	if status == "paused" {
		return nil
	}
	if status != "ready" && status != "pausing" {
		return fmt.Errorf("AIO sandbox cannot pause from %s", status)
	}
	if status == "ready" {
		_, err = client.DoContext(ctx, "PauseSession", map[string]any{"ToolId": binding.ToolID, "SessionId": binding.SessionID})
	}
	waitCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	for {
		status, checkErr := check(waitCtx)
		if checkErr == nil && status == "paused" {
			return nil
		}
		if checkErr != nil || (status != "ready" && status != "pausing") {
			if err != nil {
				return fmt.Errorf("pause AIO sandbox: %w", err)
			}
			if checkErr != nil {
				return checkErr
			}
			return fmt.Errorf("AIO sandbox changed to %s while pausing", status)
		}
		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-time.After(time.Second):
		}
	}
}

// DeleteAIOBinding confirms deletion of the physical sandbox. It does not
// delete the ADK session or its conversation history.
func DeleteAIOBinding(ctx context.Context, client *Client, state session.ReadonlyState, appName, userID, sessionID string) error {
	binding, err := checkedAIOBinding(state, appName, userID, sessionID)
	if err != nil {
		return err
	}
	// Observe before every attempt, including retries after an uncertain result.
	current, getErr := client.DoContext(ctx, "GetSession", map[string]any{"ToolId": binding.ToolID, "SessionId": binding.SessionID})
	if isMissingAIOSession(getErr) {
		return nil
	}
	if getErr != nil {
		return getErr
	}
	info := parseAIOSession(current)
	if strings.EqualFold(info.Status, "deleted") {
		return nil
	}
	expected := aioPhysicalPrefix(binding.Identity, binding.SkillSpaceIDs)
	if info.ID != binding.SessionID || (info.UserID != expected && !strings.HasPrefix(info.UserID, expected+"_r_")) {
		return errors.New("bound AIO sandbox belongs to another ADK session")
	}
	if !strings.EqualFold(info.Status, "terminating") {
		_, err = client.DoContext(ctx, "DeleteSession", map[string]any{"ToolId": binding.ToolID, "SessionId": binding.SessionID})
	}
	if isMissingAIOSession(err) {
		return nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	for {
		result, checkErr := client.DoContext(waitCtx, "GetSession", map[string]any{"ToolId": binding.ToolID, "SessionId": binding.SessionID})
		if isMissingAIOSession(checkErr) || checkErr == nil && strings.EqualFold(parseAIOSession(result).Status, "deleted") {
			return nil
		}
		if checkErr != nil {
			return checkErr
		}
		if err != nil && !strings.EqualFold(parseAIOSession(result).Status, "terminating") {
			return fmt.Errorf("delete AIO sandbox result unknown: %w", err)
		}
		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-time.After(time.Second):
		}
	}
}

func readyAIOSession(ctx context.Context, client *Client, binding AIOBinding) (*aioSession, error) {
	return readyAIOSessionTTL(ctx, client, binding, aioSessionTTL)
}

func readyAIOSessionTTL(ctx context.Context, client *Client, binding AIOBinding, ttlSeconds int) (*aioSession, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	resumeRequested := false
	for {
		result, err := client.DoContext(waitCtx, "GetSession", map[string]any{"ToolId": binding.ToolID, "SessionId": binding.SessionID})
		if err != nil {
			return nil, fmt.Errorf("resolve bound AIO sandbox: %w", err)
		}
		info := parseAIOSession(result)
		if info.ID != "" && info.ID != binding.SessionID {
			return nil, errors.New("AgentKit returned another sandbox session")
		}
		expectedUserSessionID := aioPhysicalPrefix(binding.Identity, binding.SkillSpaceIDs)
		if info.UserID != expectedUserSessionID && !strings.HasPrefix(info.UserID, expectedUserSessionID+"_r_") {
			return nil, errors.New("bound AIO sandbox belongs to another ADK session")
		}
		if expired(info.ExpireAt) {
			return nil, errExpiredAIOSession
		}
		switch strings.ToLower(info.Status) {
		case "ready":
			if info.Endpoint == "" && info.InternalEndpoint == "" {
				return nil, errors.New("bound AIO sandbox is Ready but has no endpoint")
			}
			return &info, nil
		case "starting":
		case "paused":
			if resumeRequested {
				break
			}
			resumeRequested = true
			_, resumeErr := client.DoContext(waitCtx, "ResumeSession", map[string]any{
				"ToolId": binding.ToolID, "SessionId": binding.SessionID,
				"Ttl": ttlSeconds, "TtlUnit": "second",
			})
			if resumeErr != nil {
				// A timed-out write may have succeeded. Observe the resource before
				// deciding whether the operation failed.
				result, getErr := client.DoContext(waitCtx, "GetSession", map[string]any{"ToolId": binding.ToolID, "SessionId": binding.SessionID})
				if getErr != nil || strings.EqualFold(parseAIOSession(result).Status, "paused") {
					return nil, fmt.Errorf("resume bound AIO sandbox: %w", resumeErr)
				}
			}
		case "pausing", "resuming":
		default:
			return nil, fmt.Errorf("bound AIO sandbox is %s", info.Status)
		}
		select {
		case <-waitCtx.Done():
			return nil, waitCtx.Err()
		case <-time.After(time.Second):
		}
	}
}

func listAIOSessions(ctx context.Context, client *Client, toolID, userID string) ([]aioSession, error) {
	var out []aioSession
	seen := map[string]bool{}
	var next string
	for {
		body := map[string]any{"ToolId": toolID, "Filters": []map[string]any{{"NameContains": "UserSessionId", "Values": []string{userID}}}, "MaxResults": 100}
		if next != "" {
			body["NextToken"] = next
		}
		response, err := client.DoContext(ctx, "ListSessions", body)
		if err != nil {
			return nil, fmt.Errorf("list AIO sandbox sessions: %w", err)
		}
		container := resultMap(response)
		items, ok := firstAny(container, "SessionInfos", "session_infos", "Items", "items").([]any)
		if !ok {
			return nil, errors.New("AgentKit ListSessions returned invalid session list")
		}
		for _, raw := range items {
			if item, ok := raw.(map[string]any); ok {
				out = append(out, sessionFromMap(item))
			}
		}
		next = firstString(container, "NextToken", "next_token")
		if next == "" {
			return out, nil
		}
		if seen[next] {
			return nil, errors.New("AgentKit ListSessions repeated NextToken")
		}
		seen[next] = true
	}
}

func resultMap(response map[string]any) map[string]any {
	if v, ok := response["Result"].(map[string]any); ok {
		return v
	}
	if v, ok := response["result"].(map[string]any); ok {
		return v
	}
	return response
}

func firstAny(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key].(string); ok {
			return v
		}
	}
	return ""
}

func parseAIOSession(response map[string]any) aioSession { return sessionFromMap(resultMap(response)) }

func sessionFromMap(m map[string]any) aioSession {
	return aioSession{
		ID: firstString(m, "SessionId", "session_id"), UserID: firstString(m, "UserSessionId", "user_session_id"),
		Status: firstString(m, "Status", "status"), Endpoint: firstString(m, "Endpoint", "endpoint"),
		InternalEndpoint: firstString(m, "InternalEndpoint", "internal_endpoint"),
		ExpireAt:         firstString(m, "ExpireAt", "expire_at"), CreatedAt: firstString(m, "CreatedAt", "created_at"),
	}
}

func expired(value string) bool {
	if value == "" {
		return false
	}
	when, err := time.Parse(time.RFC3339Nano, value)
	return err != nil || !when.After(time.Now())
}
