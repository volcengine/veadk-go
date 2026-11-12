// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package skilltool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	agentkit "github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"github.com/volcengine/veadk-go/v2/skills"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

const aioMaxResponse = 4 << 20
const aioMaxOutput = 50_000
const AIOSandboxSkillRoot = "/workspace/skills"

// AIOSandboxToolset keeps immutable Agent-level Skill metadata and prepares
// the session sandbox lazily when a bash or file tool executes.
type AIOSandboxToolset struct {
	skillSpaceIDs []string
	instruction   string
	tools         []tool.Tool
}

// NewAIOSandboxToolset accepts a metadata snapshot discovered before the
// Agent starts. The sandbox must materialize those Skills under
// AIOSandboxSkillRoot/<name> from skillSpaceIDs during its lazy creation.
func NewAIOSandboxToolset(metadata []skills.RemoteSkill, skillSpaceIDs ...string) (*AIOSandboxToolset, error) {
	s := &AIOSandboxToolset{skillSpaceIDs: append([]string(nil), skillSpaceIDs...)}
	catalog := make(map[string]aioSkill, len(metadata))
	for _, item := range metadata {
		front := &skills.Frontmatter{Name: item.Name, Description: item.Description}
		if err := front.Validate(); err != nil {
			return nil, fmt.Errorf("invalid AIO Skill metadata: %w", err)
		}
		if _, exists := catalog[item.Name]; exists {
			return nil, fmt.Errorf("duplicate AIO Skill name %q", item.Name)
		}
		catalog[item.Name] = aioSkill{Name: item.Name, Description: item.Description, Path: path.Join(AIOSandboxSkillRoot, item.Name)}
	}
	var xml strings.Builder
	xml.WriteString("<available_skills>\n")
	names := make([]string, 0, len(catalog))
	for name := range catalog {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sk := catalog[name]
		xml.WriteString("<skill><name>")
		xml.WriteString(html.EscapeString(sk.Name))
		xml.WriteString("</name><description>")
		xml.WriteString(html.EscapeString(sk.Description))
		xml.WriteString("</description><path>")
		xml.WriteString(html.EscapeString(sk.Path))
		xml.WriteString("</path></skill>\n")
	}
	xml.WriteString("</available_skills>")
	s.instruction = "Skills are installed in the current session sandbox. Each path below is a Skill directory containing SKILL.md. When a Skill is relevant, use read_file on <path>/SKILL.md before following its instructions. Use read_file for other Skill files and bash to run scripts. The tools access the sandbox, not the Runtime host.\n\n" + xml.String()
	add := func(t tool.Tool, err error) error {
		if err != nil {
			return err
		}
		s.tools = append(s.tools, t)
		return nil
	}
	if err := add(functiontool.New(functiontool.Config{Name: "bash", Description: "Runs a bash command in the current sandbox."}, s.bash)); err != nil {
		return nil, err
	}
	if err := add(functiontool.New(functiontool.Config{Name: "read_file", Description: "Reads a UTF-8 file in the current sandbox with line numbers."}, s.readFile)); err != nil {
		return nil, err
	}
	if err := add(functiontool.New(functiontool.Config{Name: "write_file", Description: "Writes a UTF-8 file in the current sandbox."}, s.writeFile)); err != nil {
		return nil, err
	}
	if err := add(functiontool.New(functiontool.Config{Name: "edit_file", Description: "Replaces exact text in a file in the current sandbox."}, s.editFile)); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *AIOSandboxToolset) Name() string { return "AIOSandboxToolset" }
func (s *AIOSandboxToolset) Tools(agent.ReadonlyContext) ([]tool.Tool, error) {
	return append([]tool.Tool(nil), s.tools...), nil
}

// Instruction is complete at Agent construction and never depends on a
// session-bound sandbox or a per-invocation catalog request.
func (s *AIOSandboxToolset) Instruction() string { return s.instruction }

type aioSkill struct{ Name, Description, Path string }

func (s *AIOSandboxToolset) bash(ctx agent.Context, args bashArgs) (map[string]any, error) {
	client, err := aioClientFor(ctx, s.skillSpaceIDs...)
	if err != nil {
		return nil, err
	}
	if math.IsNaN(args.Timeout) || math.IsInf(args.Timeout, 0) || args.Timeout < 0 {
		return nil, errors.New("invalid bash timeout")
	}
	timeout := 600 * time.Second
	if args.Timeout > 0 && args.Timeout < timeout.Seconds() {
		timeout = time.Duration(args.Timeout * float64(time.Second))
	}
	return client.bash(ctx, args.Command, timeout)
}

func aioFilePath(input string) (string, error) {
	if input == "" || strings.ContainsAny(input, "\x00\\") {
		return "", errors.New("invalid sandbox file path")
	}
	if path.IsAbs(input) {
		return path.Clean(input), nil
	}
	return path.Join("/workspace", input), nil
}

func (s *AIOSandboxToolset) readFile(ctx agent.Context, args readFileArgs) (map[string]any, error) {
	client, err := aioClientFor(ctx, s.skillSpaceIDs...)
	if err != nil {
		return nil, err
	}
	file, err := aioFilePath(args.FilePath)
	if err != nil {
		return nil, err
	}
	if args.Offset < 0 || args.Limit < 0 {
		return nil, errors.New("offset and limit must be nonnegative")
	}
	start := max(args.Offset-1, 0)
	body := map[string]any{"file": file, "start_line": start}
	if args.Limit > 0 {
		body["end_line"] = start + min(args.Limit, maxReadLines)
	}
	data, err := client.call(ctx, "POST", "/v1/file/read", body)
	if err != nil {
		return nil, err
	}
	content, ok := data["content"].(string)
	if !ok || !utf8.ValidString(content) {
		return nil, errors.New("invalid AIO file content")
	}
	var lines []string
	for i, line := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		if i >= maxReadLines {
			break
		}
		if len(line) > 2000 {
			cut := 2000
			for cut > 0 && !utf8.ValidString(line[:cut]) {
				cut--
			}
			line = line[:cut] + "..."
		}
		lines = append(lines, fmt.Sprintf("%6d|%s", start+i+1, line))
	}
	return map[string]any{"content": strings.Join(lines, "\n"), "line_count": len(lines)}, nil
}

func (s *AIOSandboxToolset) writeFile(ctx agent.Context, args writeFileArgs) (map[string]any, error) {
	client, err := aioClientFor(ctx, s.skillSpaceIDs...)
	if err != nil {
		return nil, err
	}
	file, err := aioFilePath(args.FilePath)
	if err != nil {
		return nil, err
	}
	_, err = client.call(ctx, "POST", "/v1/file/write", map[string]any{"file": file, "content": args.Content})
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "path": file, "bytes_written": len(args.Content)}, nil
}

func (s *AIOSandboxToolset) editFile(ctx agent.Context, args editFileArgs) (map[string]any, error) {
	client, err := aioClientFor(ctx, s.skillSpaceIDs...)
	if err != nil {
		return nil, err
	}
	file, err := aioFilePath(args.FilePath)
	if err != nil {
		return nil, err
	}
	if args.OldString == "" || args.OldString == args.NewString {
		return nil, errors.New("invalid replacement")
	}
	body := map[string]any{"command": "str_replace", "path": file, "old_str": args.OldString, "new_str": args.NewString}
	if args.ReplaceAll {
		body["replace_mode"] = "ALL"
	}
	data, err := client.call(ctx, "POST", "/v1/file/str_replace_editor", body)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "path": file, "output": data["output"]}, nil
}

type aioClient struct {
	base    *url.URL
	client  *http.Client
	headers http.Header
}

var aioHTTPClient = func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Transport: transport, Timeout: 35 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}()

func aioClientFor(ctx agent.Context, skillSpaceIDs ...string) (*aioClient, error) {
	endpoint, err := agentkit.EnsureAIOToolEndpoint(ctx, skillSpaceIDs)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("invalid AIO sandbox endpoint")
	}
	headers := http.Header{}
	if key := os.Getenv("SANDBOX_API_KEY"); key != "" {
		headers.Set("X-API-Key", key)
	}
	return &aioClient{base: u, client: aioHTTPClient, headers: headers}, nil
}

func (c *aioClient) call(ctx context.Context, method, endpoint string, body any) (map[string]any, error) {
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + endpoint
	query := u.Query()
	u.RawQuery = query.Encode()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), payload)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, values := range c.headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	response, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AIO sandbox transport failure; operation may have completed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AIO sandbox HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, aioMaxResponse+1))
	if err != nil {
		return nil, fmt.Errorf("AIO sandbox response read failed: %w", err)
	}
	if len(raw) > aioMaxResponse {
		return nil, errors.New("AIO sandbox response exceeds size limit")
	}
	var envelope struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || !envelope.Success {
		return nil, errors.New("AIO sandbox operation failed or returned invalid response")
	}
	if envelope.Data == nil {
		return map[string]any{}, nil
	}
	if value := envelope.Data["error"]; value != nil && value != "" && value != false {
		return nil, errors.New("AIO sandbox operation failed")
	}
	return envelope.Data, nil
}

func aioString(data map[string]any, key string) string { value, _ := data[key].(string); return value }

func (c *aioClient) bash(ctx context.Context, command string, timeout time.Duration) (result map[string]any, err error) {
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("bash command must not be empty")
	}
	if timeout <= 0 {
		return nil, errors.New("bash timeout must be positive")
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	shellID := "veadk-" + uuid.NewString()
	created := false
	terminal := false
	defer func() {
		if !created {
			return
		}
		cleanupCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if !terminal {
			if _, cleanupErr := c.call(cleanupCtx, "POST", "/v1/bash/kill", map[string]any{"session_id": shellID, "signal": "SIGKILL"}); cleanupErr != nil && err == nil {
				err = fmt.Errorf("AIO bash cleanup state unknown: %w", cleanupErr)
			}
		}
		if _, cleanupErr := c.call(cleanupCtx, "POST", "/v1/bash/sessions/"+shellID+"/close", map[string]any{}); cleanupErr != nil && err == nil {
			err = fmt.Errorf("AIO bash cleanup state unknown: %w", cleanupErr)
		}
	}()
	created = true // creation may have succeeded even if its response is lost
	_, err = c.call(commandCtx, "POST", "/v1/bash/sessions/create", map[string]any{"session_id": shellID, "exec_dir": "/workspace"})
	if err != nil {
		return nil, err
	}
	info, err := c.call(commandCtx, "POST", "/v1/bash/exec", map[string]any{"session_id": shellID, "command": command, "async_mode": true, "hard_timeout": timeout.Seconds(), "max_output_length": aioMaxOutput})
	if err != nil {
		return nil, err
	}
	var stdout, stderr strings.Builder
	truncated := false
	appendOutput := func(data map[string]any) {
		for _, pair := range []struct {
			key    string
			target *strings.Builder
		}{{"stdout", &stdout}, {"stderr", &stderr}} {
			value := aioString(data, pair.key)
			if pair.target.Len()+len(value) > aioMaxOutput {
				value = value[:max(0, aioMaxOutput-pair.target.Len())]
				truncated = true
			}
			pair.target.WriteString(value)
		}
	}
	appendOutput(info)
	for {
		status := aioString(info, "status")
		if status == "completed" || status == "killed" || status == "timed_out" {
			terminal = true
			break
		}
		commandID := aioString(info, "command_id")
		if commandID == "" {
			return nil, errors.New("invalid AIO bash command response")
		}
		data, pollErr := c.call(commandCtx, "POST", "/v1/bash/output", map[string]any{"session_id": shellID, "command_id": commandID, "offset": info["offset"], "stderr_offset": info["stderr_offset"], "wait": true, "wait_timeout": 1})
		if pollErr != nil {
			return nil, pollErr
		}
		if data["stdout_gap"] == true || data["stderr_gap"] == true {
			return nil, errors.New("AIO bash output was lost")
		}
		appendOutput(data)
		commandInfo, ok := data["command"].(map[string]any)
		if !ok {
			return nil, errors.New("invalid AIO bash output response")
		}
		for key, value := range commandInfo {
			info[key] = value
		}
		info["offset"] = data["offset"]
		info["stderr_offset"] = data["stderr_offset"]
	}
	exitCode := float64(-1)
	if raw := info["exit_code"]; raw != nil {
		value, ok := raw.(float64)
		if !ok {
			return nil, errors.New("invalid AIO bash exit code")
		}
		exitCode = value
	}
	status := "success"
	if aioString(info, "status") != "completed" || exitCode != 0 {
		status = "error"
	}
	return map[string]any{"status": status, "exit_code": int(exitCode), "stdout": stdout.String(), "stderr": stderr.String(), "truncated": truncated}, nil
}
