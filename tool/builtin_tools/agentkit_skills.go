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

package builtin_tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/volcengine/veadk-go/v2/common"
	"github.com/volcengine/veadk-go/v2/configs"
	agentkitclient "github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"github.com/volcengine/veadk-go/v2/integrations/ve_sign"
	"github.com/volcengine/veadk-go/v2/utils"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

const (
	defaultSkillTimeoutSeconds = 1800
	a2aHistoryLength           = 20
	a2aRequestTimeout          = 60 * time.Second
	a2aPollInterval            = 2 * time.Second
	a2aMaxPollInterval         = 16 * time.Second
	sessionReadyTimeout        = 120 * time.Second
	sessionPollInterval        = time.Second
)

var (
	errMissingToolContext = errors.New("tool context is required")
	errInvalidTimeout     = errors.New("timeout must be between 1 and 1800 seconds")

	a2aTerminalStates = map[string]bool{
		"completed":      true,
		"failed":         true,
		"canceled":       true,
		"rejected":       true,
		"input-required": true,
		"auth-required":  true,
	}
	sessionTerminalStatuses = map[string]bool{
		"failed":      true,
		"terminating": true,
		"terminated":  true,
	}
	retryableA2AStatuses = map[int]bool{
		http.StatusBadGateway:         true,
		http.StatusServiceUnavailable: true,
		http.StatusGatewayTimeout:     true,
	}
	agentkitDoRequest = func(req ve_sign.VeRequest) ([]byte, error) {
		return req.DoRequest()
	}
)

type agentkitSession struct {
	SessionID        string
	UserSessionID    string
	Status           string
	Endpoint         string
	InternalEndpoint string
	CreatedAt        string
}

type executeSkillsArgs struct {
	WorkflowPrompt string            `json:"workflow_prompt" jsonschema:"The workflow instruction to execute in the Skills Sandbox."`
	EnvVars        map[string]string `json:"env_vars,omitempty" jsonschema:"Unsupported for Skills Sandbox A2A execution."`
	Timeout        int               `json:"timeout,omitempty" jsonschema:"Maximum execution time in seconds. Defaults to 1800."`
}

type invokeSkillArgs struct {
	WorkflowPrompt string `json:"workflow_prompt" jsonschema:"The workflow instruction to execute in the Skills Sandbox."`
	Timeout        int    `json:"timeout,omitempty" jsonschema:"Maximum time in seconds to create the A2A task. Defaults to 1800."`
}

type pollSkillArgs struct {
	TaskID  string `json:"task_id" jsonschema:"The A2A task ID returned by invoke_skill."`
	Timeout int    `json:"timeout,omitempty" jsonschema:"Maximum time in seconds to fetch the task snapshot. Defaults to 1800."`
}

// NewExecuteSkillsSandboxTool creates the execute_skills tool. It delegates the
// user workflow to an AgentKit Skills Sandbox and waits for the final A2A task
// result, matching veadk-python's execute_skills behavior.
func NewExecuteSkillsSandboxTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:          "execute_skills",
		Description:   "Execute skills in an AgentKit Skills Sandbox and return the final result.",
		IsLongRunning: true,
	}, executeSkillsHandler)
}

// NewInvokeSkillSandboxTool creates the non-blocking invoke_skill tool. It
// starts a Skills Sandbox A2A task and returns the initial task object.
func NewInvokeSkillSandboxTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:          "invoke_skill",
		Description:   "Create a non-blocking AgentKit Skills Sandbox A2A task.",
		IsLongRunning: true,
	}, invokeSkillHandler)
}

// NewPollSkillSandboxTool creates the poll_skill tool. It fetches one snapshot
// of an A2A task created by invoke_skill.
func NewPollSkillSandboxTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "poll_skill",
		Description: "Fetch one AgentKit Skills Sandbox A2A task snapshot by task_id.",
	}, pollSkillHandler)
}

func executeSkillsHandler(ctx agent.Context, args executeSkillsArgs) (map[string]any, error) {
	if ctx == nil {
		return nil, errMissingToolContext
	}
	if args.EnvVars != nil {
		return nil, errors.New("env_vars is not supported for execute_skills A2A execution")
	}
	timeout, err := normalizeSkillTimeout(args.Timeout)
	if err != nil {
		return nil, err
	}
	endpoint, err := ensureAgentkitSessionEndpoint(ctx, timeout)
	if err != nil {
		return nil, fmt.Errorf("AgentKit session endpoint is not available: %w", err)
	}
	task, err := sendA2AMessage(ctx, endpoint, args.WorkflowPrompt, timeout)
	if err != nil {
		return nil, err
	}
	taskID, err := a2aTaskID(task)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	pollInterval := a2aPollInterval
	for !a2aTerminalStates[a2aTaskState(task)] {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out while waiting for A2A task %s", taskID)
		}
		sleep := minDuration(pollInterval, time.Until(deadline))
		if sleep > 0 {
			time.Sleep(sleep)
		}
		task, err = getA2ATask(ctx, endpoint, taskID, deadline)
		if err != nil {
			return nil, err
		}
		pollInterval = minDuration(pollInterval*2, a2aMaxPollInterval)
	}
	state := a2aTaskState(task)
	if state != "completed" {
		return nil, fmt.Errorf("A2A task %s ended with state %s: %s", taskID, state, mustJSON(task))
	}
	text := a2aTaskResultText(task)
	if text == "" {
		text = mustJSON(task)
	}
	return map[string]any{
		"result":  text,
		"task_id": taskID,
		"task":    task,
	}, nil
}

func invokeSkillHandler(ctx agent.Context, args invokeSkillArgs) (map[string]any, error) {
	if ctx == nil {
		return nil, errMissingToolContext
	}
	timeout, err := normalizeSkillTimeout(args.Timeout)
	if err != nil {
		return nil, err
	}
	endpoint, err := ensureAgentkitSessionEndpoint(ctx, timeout)
	if err != nil {
		return nil, fmt.Errorf("AgentKit session endpoint is not available: %w", err)
	}
	task, err := sendA2AMessage(ctx, endpoint, args.WorkflowPrompt, timeout)
	if err != nil {
		return nil, err
	}
	return task, nil
}

func pollSkillHandler(ctx agent.Context, args pollSkillArgs) (map[string]any, error) {
	if ctx == nil {
		return nil, errMissingToolContext
	}
	if strings.TrimSpace(args.TaskID) == "" {
		return nil, errors.New("task_id is required for poll_skill")
	}
	timeout, err := normalizeSkillTimeout(args.Timeout)
	if err != nil {
		return nil, err
	}
	endpoint, err := ensureAgentkitSessionEndpoint(ctx, timeout)
	if err != nil {
		return nil, fmt.Errorf("AgentKit session endpoint is not available: %w", err)
	}
	return getA2ATask(ctx, endpoint, strings.TrimSpace(args.TaskID), time.Now().Add(time.Duration(timeout)*time.Second))
}

func normalizeSkillTimeout(timeout int) (int, error) {
	if timeout == 0 {
		return defaultSkillTimeoutSeconds, nil
	}
	if timeout < 1 || timeout > defaultSkillTimeoutSeconds {
		return 0, errInvalidTimeout
	}
	return timeout, nil
}

func resolveSkillsToolID() string {
	if toolID := strings.TrimSpace(os.Getenv(common.AGENTKIT_TOOL_ID_SKILLS)); toolID != "" {
		return toolID
	}
	if toolID := strings.TrimSpace(os.Getenv(common.AGENTKIT_TOOL_ID)); toolID != "" {
		return toolID
	}
	if cfg := configs.GetGlobalConfig(); cfg != nil && cfg.Tool != nil && cfg.Tool.RunCode != nil {
		return strings.TrimSpace(cfg.Tool.RunCode.ToolID)
	}
	return ""
}

func ensureAgentkitSessionEndpoint(ctx agent.Context, timeout int) (string, error) {
	toolID := resolveSkillsToolID()
	if toolID == "" {
		return "", ErrInvalidToolID
	}
	userSessionID := toolUserSessionID(ctx)
	session, _ := reusableAgentkitSession(ctx, toolID, userSessionID)
	if session == nil {
		created, err := createAgentkitSession(ctx, toolID, userSessionID, max(timeout, defaultSkillTimeoutSeconds))
		if err != nil {
			return "", err
		}
		session = created
	}
	if endpoint := selectSessionEndpoint(session); endpoint != "" && strings.EqualFold(session.Status, "ready") {
		return endpoint, nil
	}
	if session.SessionID == "" {
		return "", errors.New("AgentKit CreateSession response is missing SessionId")
	}
	deadline := time.Now().Add(sessionReadyTimeout)
	for {
		current, err := getAgentkitSession(ctx, toolID, session.SessionID)
		if err != nil {
			return "", err
		}
		status := strings.ToLower(strings.TrimSpace(current.Status))
		if status == "ready" {
			endpoint := selectSessionEndpoint(current)
			if endpoint == "" {
				return "", fmt.Errorf("AgentKit session %s is Ready but has no endpoint", session.SessionID)
			}
			return endpoint, nil
		}
		if sessionTerminalStatuses[status] {
			return "", fmt.Errorf("AgentKit session %s entered terminal status %s", session.SessionID, current.Status)
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out waiting for AgentKit session %s to become Ready; last status: %s", session.SessionID, current.Status)
		}
		time.Sleep(minDuration(sessionPollInterval, time.Until(deadline)))
	}
}

func reusableAgentkitSession(ctx agent.Context, toolID, userSessionID string) (*agentkitSession, error) {
	body := map[string]any{
		"ToolId": toolID,
		"Filters": []map[string]any{{
			"Name":   "UserSessionId",
			"Values": []string{userSessionID},
		}},
		"PageSize": 20,
	}
	raw, err := signedAgentkitRequest(ctx, "ListSessions", body)
	if err != nil {
		return nil, err
	}
	sessions := parseAgentkitSessions(raw)
	var best *agentkitSession
	for i := range sessions {
		session := sessions[i]
		if session.UserSessionID != "" && session.UserSessionID != userSessionID {
			continue
		}
		if sessionTerminalStatuses[strings.ToLower(strings.TrimSpace(session.Status))] {
			continue
		}
		if best == nil || session.CreatedAt > best.CreatedAt {
			copy := session
			best = &copy
		}
	}
	return best, nil
}

func createAgentkitSession(ctx agent.Context, toolID, userSessionID string, ttl int) (*agentkitSession, error) {
	body := map[string]any{
		"ToolId":        toolID,
		"UserSessionId": userSessionID,
		"Ttl":           ttl,
	}
	if policy := strings.TrimSpace(os.Getenv("SKILL_SPACE_POLICY")); policy != "" {
		body["Envs"] = []map[string]string{{
			"Key":   "SKILL_SPACE_POLICY",
			"Value": policy,
		}}
	}
	raw, err := signedAgentkitRequest(ctx, "CreateSession", body)
	if err != nil {
		return nil, err
	}
	session := parseAgentkitSession(raw)
	if session.SessionID == "" {
		return nil, errors.New("AgentKit CreateSession response is missing SessionId")
	}
	if session.UserSessionID == "" {
		session.UserSessionID = userSessionID
	}
	return session, nil
}

func getAgentkitSession(ctx agent.Context, toolID, sessionID string) (*agentkitSession, error) {
	raw, err := signedAgentkitRequest(ctx, "GetSession", map[string]any{
		"ToolId":    toolID,
		"SessionId": sessionID,
	})
	if err != nil {
		return nil, err
	}
	session := parseAgentkitSession(raw)
	if session.SessionID == "" {
		session.SessionID = sessionID
	}
	return session, nil
}

func signedAgentkitRequest(ctx agent.Context, action string, body map[string]any) (map[string]any, error) {
	client, err := agentkitclient.NewClient(ctx, agentkitclient.WithDoRequest(agentkitDoRequest))
	if err != nil {
		return nil, err
	}
	return client.Do(action, body)
}

func agentkitEndpointConfig() (service, region, host, scheme string) {
	return agentkitclient.EndpointConfig()
}

func toolUserSessionID(ctx agent.Context) string {
	if ctx == nil || strings.TrimSpace(ctx.SessionID()) == "" {
		return uuid.NewString()
	}
	return fmt.Sprintf("%s_%s_%s", ctx.AgentName(), ctx.UserID(), ctx.SessionID())
}

func selectSessionEndpoint(session *agentkitSession) string {
	if session == nil {
		return ""
	}
	if strings.TrimSpace(session.Endpoint) != "" {
		return session.Endpoint
	}
	return session.InternalEndpoint
}

func sendA2AMessage(ctx agent.Context, endpoint, prompt string, timeout int) (map[string]any, error) {
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      uuid.NewString(),
		"method":  "message/send",
		"params": map[string]any{
			"message": map[string]any{
				"kind":      "message",
				"messageId": uuid.NewString(),
				"role":      "user",
				"parts": []map[string]any{{
					"kind": "text",
					"text": prompt,
				}},
			},
			"metadata": map[string]any{
				"user_id":    ctx.UserID(),
				"session_id": ctx.SessionID(),
			},
			"configuration": map[string]any{
				"blocking":      false,
				"historyLength": a2aHistoryLength,
			},
		},
	}
	response, err := postA2AJSONRPC(ctx, endpoint, payload, deadline)
	return a2aResultTask("A2ASendMessage", response, err)
}

func getA2ATask(ctx agent.Context, endpoint, taskID string, deadline time.Time) (map[string]any, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      uuid.NewString(),
		"method":  "tasks/get",
		"params": map[string]any{
			"id":            taskID,
			"historyLength": a2aHistoryLength,
		},
	}
	response, err := postA2AJSONRPC(ctx, endpoint, payload, deadline)
	return a2aResultTask("A2AGetTask", response, err)
}

func postA2AJSONRPC(ctx agent.Context, endpoint string, payload map[string]any, deadline time.Time) (map[string]any, error) {
	endpointURL, err := a2aJSONRPCURL(endpoint)
	if err != nil {
		return nil, err
	}
	for {
		if time.Now().After(deadline) {
			return nil, errors.New("timed out while waiting for A2A endpoint")
		}
		body, _ := json.Marshal(payload)
		requestTimeout := minDuration(a2aRequestTimeout, time.Until(deadline))
		reqCtx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpointURL, bytes.NewReader(body))
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if inboundAuth := inboundAuthToken(ctx); inboundAuth != "" {
			req.Header.Set("inbound_auth", inboundAuth)
		}
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("AgentKit Skill /a2a endpoint is not reachable: %w", err)
		}
		raw, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var result map[string]any
			if err := json.Unmarshal(raw, &result); err != nil {
				return nil, errors.New("A2A JSON-RPC response is not valid JSON")
			}
			return result, nil
		}
		if retryableA2AStatuses[resp.StatusCode] && time.Now().Before(deadline) {
			time.Sleep(minDuration(a2aPollInterval, time.Until(deadline)))
			continue
		}
		return nil, fmt.Errorf("AgentKit Skill /a2a request failed with HTTP %d: %s", resp.StatusCode, string(raw))
	}
}

func a2aJSONRPCURL(endpoint string) (string, error) {
	if strings.TrimSpace(endpoint) == "" {
		return "", errors.New("AgentKit session endpoint is empty")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(path, "/a2a") {
		path += "/a2a"
	}
	if path == "" {
		path = "/a2a"
	}
	parsed.Path = path
	return parsed.String(), nil
}

func inboundAuthToken(ctx agent.Context) string {
	if ctx == nil {
		return ""
	}
	return utils.GetStringFromToolContext(ctx, "inbound_auth")
}

func a2aResultTask(operation string, response map[string]any, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	if value := response["error"]; value != nil {
		return nil, fmt.Errorf("%s error: %s", operation, mustJSON(value))
	}
	result, ok := response["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s response does not contain result task", operation)
	}
	if _, hasStatus := result["status"]; result["kind"] != "task" && !hasStatus {
		return nil, fmt.Errorf("%s response result is not an A2A task", operation)
	}
	return result, nil
}

func a2aTaskID(task map[string]any) (string, error) {
	id, ok := task["id"].(string)
	if !ok || id == "" {
		return "", errors.New("A2A message/send response task does not contain id")
	}
	return id, nil
}

func a2aTaskState(task map[string]any) string {
	status, _ := task["status"].(map[string]any)
	state, _ := status["state"].(string)
	return state
}

func a2aTaskResultText(task map[string]any) string {
	if artifacts, ok := task["artifacts"].([]any); ok {
		var chunks []string
		for _, item := range artifacts {
			artifact, _ := item.(map[string]any)
			if text := extractA2ATextParts(artifact["parts"]); text != "" {
				chunks = append(chunks, text)
			}
		}
		if len(chunks) > 0 {
			return strings.Join(chunks, "\n")
		}
	}
	if status, ok := task["status"].(map[string]any); ok {
		if message, ok := status["message"].(map[string]any); ok {
			if text := extractA2ATextParts(message["parts"]); text != "" {
				return text
			}
		}
	}
	if history, ok := task["history"].([]any); ok {
		for i := len(history) - 1; i >= 0; i-- {
			message, _ := history[i].(map[string]any)
			role, _ := message["role"].(string)
			if role == "agent" || role == "assistant" {
				if text := extractA2ATextParts(message["parts"]); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

func extractA2ATextParts(parts any) string {
	list, ok := parts.([]any)
	if !ok {
		return ""
	}
	var chunks []string
	for _, item := range list {
		part, _ := item.(map[string]any)
		if metadata, _ := part["metadata"].(map[string]any); metadata["adk_thought"] == true {
			continue
		}
		if text, ok := part["text"].(string); ok {
			chunks = append(chunks, text)
			continue
		}
		if textPart, ok := part["textPart"].(map[string]any); ok {
			if text, ok := textPart["text"].(string); ok {
				chunks = append(chunks, text)
			}
		}
	}
	return strings.Join(chunks, "")
}

func parseAgentkitSession(response map[string]any) *agentkitSession {
	if result, ok := response["Result"].(map[string]any); ok {
		return sessionFromMap(result)
	}
	if result, ok := response["result"].(map[string]any); ok {
		return sessionFromMap(result)
	}
	return sessionFromMap(response)
}

func parseAgentkitSessions(response map[string]any) []agentkitSession {
	var out []agentkitSession
	for _, container := range []map[string]any{response, mapValue(response, "Result"), mapValue(response, "result")} {
		for _, key := range []string{"SessionInfos", "session_infos", "Items", "items"} {
			items, ok := container[key].([]any)
			if !ok {
				continue
			}
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					out = append(out, *sessionFromMap(m))
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return out
}

func sessionFromMap(m map[string]any) *agentkitSession {
	return &agentkitSession{
		SessionID:        firstString(m, "SessionId", "SessionID", "session_id", "id"),
		UserSessionID:    firstString(m, "UserSessionId", "UserSessionID", "user_session_id"),
		Status:           firstString(m, "Status", "status"),
		Endpoint:         firstString(m, "Endpoint", "endpoint"),
		InternalEndpoint: firstString(m, "InternalEndpoint", "internal_endpoint"),
		CreatedAt:        firstString(m, "CreatedAt", "created_at"),
	}
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key].(string); ok {
			return value
		}
	}
	return ""
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
