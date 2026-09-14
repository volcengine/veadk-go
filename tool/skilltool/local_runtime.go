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

package skilltool

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/volcengine/veadk-go/skills"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const (
	defaultWorkspaceRoot        = "/tmp/veadk"
	defaultCommandTimeout       = 10 * time.Minute
	defaultMaxCommandOutputSize = 4 * 1024 * 1024
	defaultMaxWorkspaceBytes    = 512 * 1024 * 1024
	defaultWorkspaceTTL         = 24 * time.Hour
	defaultCleanupInterval      = 5 * time.Minute
	maxReadLines                = 10_000
	maxScriptArguments          = 256
	maxScriptArgumentBytes      = 64 * 1024
	workspaceMarker             = ".veadk-last-used"
	localRuntimeInstruction     = "Local skill execution uses a session-specific workspace containing skills/, uploads/, tmp/, and outputs/.\n" +
		"Use read_file, write_file, and edit_file with paths relative to that workspace. Skill files under skills/ are read-only through these tools.\n" +
		"Use run_skill_script for a skill script and bash only when its instructions require a shell workflow. Write final artifacts under outputs/.\n"
)

var defaultInheritedEnvironment = []string{
	"LANG", "LC_ALL", "LC_CTYPE", "NODE_PATH", "PATH", "PYTHONPATH",
	"SSL_CERT_DIR", "SSL_CERT_FILE", "TZ",
}

// LocalRuntimeConfig configures local process execution for trusted skills.
// It is not an OS sandbox: commands can access the container filesystem and
// network with the privileges of the current process. The containing runtime
// is the security boundary.
type LocalRuntimeConfig struct {
	// WorkspaceRoot must be a dedicated, non-symlink directory which is not
	// group- or world-writable. Empty uses /tmp/veadk.
	WorkspaceRoot string
	// A zero duration or size selects the documented default. Negative
	// WorkspaceTTL or CleanupInterval disables that automatic cleanup mode.
	CommandTimeout       time.Duration
	MaxCommandOutputSize int
	MaxFileBytes         int64
	MaxWorkspaceBytes    int64
	WorkspaceTTL         time.Duration
	CleanupInterval      time.Duration
	InheritEnvironment   []string
	// Environment is explicit opt-in process environment. Values may be read
	// by the executed command and must not contain credentials unless the
	// containing runtime intentionally grants them.
	Environment           map[string]string
	Audit                 func(CommandAuditEvent)
	KeepWorkspacesOnClose bool
}

// CommandAuditEvent contains non-sensitive command metadata. Command and
// argument contents are represented only by a digest.
type CommandAuditEvent struct {
	SessionIDHash     string
	Kind              string
	Program           string
	DescriptionSHA256 string
	CommandSHA256     string
	ArgumentCount     int
	StartedAt         time.Time
	Duration          time.Duration
	Status            string
	ExitCode          int
	Truncated         bool
}

// LocalRuntime backs the file and process tools of a local skill toolset.
type LocalRuntime struct {
	workspaceRoot        string
	commandTimeout       time.Duration
	maxCommandOutputSize int
	maxFileBytes         int64
	maxWorkspaceBytes    int64
	workspaceTTL         time.Duration
	cleanupInterval      time.Duration
	inheritedEnvironment []string
	environment          map[string]string
	audit                func(CommandAuditEvent)
	keepOnClose          bool
	skills               map[string]*skills.Skill

	mu          sync.Mutex
	closed      bool
	lastCleanup time.Time
	sessions    map[string]*sessionState
}

type sessionState struct {
	mu      sync.Mutex
	owned   bool
	active  map[*exec.Cmd]struct{}
	closing bool
}

// NewLocalSkillToolset explicitly enables workspace file operations and local
// process execution for disk-backed skills. Call Close when the toolset is no
// longer used so workspaces created by this instance are removed.
func NewLocalSkillToolset(skillList []*skills.Skill, config LocalRuntimeConfig) (*SkillToolset, error) {
	toolset, err := newSkillToolset(skillList, nil, true)
	if err != nil {
		return nil, err
	}
	runtime, err := newLocalRuntime(config, toolset.skills)
	if err != nil {
		return nil, err
	}
	toolset.localRuntime = runtime
	localTools, err := runtime.tools()
	if err != nil {
		_ = runtime.Close()
		return nil, err
	}
	toolset.tools = append(toolset.tools, localTools...)
	return toolset, nil
}

func newLocalRuntime(config LocalRuntimeConfig, skillMap map[string]*skills.Skill) (*LocalRuntime, error) {
	workspaceRoot := strings.TrimSpace(config.WorkspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = defaultWorkspaceRoot
	}
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create workspace root: %w", err)
	}
	rootInfo, err := os.Lstat(absRoot)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("workspace root must be a real directory")
	}
	if rootInfo.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("workspace root must not be group- or world-writable")
	}

	commandTimeout := config.CommandTimeout
	if commandTimeout == 0 {
		commandTimeout = defaultCommandTimeout
	}
	maxOutput := config.MaxCommandOutputSize
	if maxOutput == 0 {
		maxOutput = defaultMaxCommandOutputSize
	}
	maxFileBytes := config.MaxFileBytes
	if maxFileBytes == 0 {
		maxFileBytes = skills.DefaultMaxResourceBytes
	}
	maxWorkspaceBytes := config.MaxWorkspaceBytes
	if maxWorkspaceBytes == 0 {
		maxWorkspaceBytes = defaultMaxWorkspaceBytes
	}
	workspaceTTL := config.WorkspaceTTL
	if workspaceTTL == 0 {
		workspaceTTL = defaultWorkspaceTTL
	}
	cleanupInterval := config.CleanupInterval
	if cleanupInterval == 0 {
		cleanupInterval = defaultCleanupInterval
	}
	if commandTimeout < 0 || maxOutput < 0 || maxFileBytes < 0 || maxWorkspaceBytes < 0 {
		return nil, errors.New("runtime timeouts and size limits must not be negative")
	}

	validatedSkills := make(map[string]*skills.Skill, len(skillMap))
	for name, skill := range skillMap {
		if skill == nil || strings.TrimSpace(skill.SkillMDPath) == "" {
			return nil, fmt.Errorf("local runtime requires disk-backed skill %q", name)
		}
		root, rootErr := filepath.EvalSymlinks(skill.GetSkillPath())
		if rootErr != nil {
			return nil, fmt.Errorf("resolve skill %q root: %w", name, rootErr)
		}
		info, statErr := os.Stat(root)
		if statErr != nil || !info.IsDir() {
			return nil, fmt.Errorf("skill %q root must be a directory", name)
		}
		validatedSkills[name] = skill
	}

	inherited := defaultInheritedEnvironment
	if config.InheritEnvironment != nil {
		inherited = config.InheritEnvironment
	}
	inherited, err = validateInheritedEnvironment(inherited)
	if err != nil {
		return nil, err
	}
	environment, err := validateExplicitEnvironment(config.Environment)
	if err != nil {
		return nil, err
	}

	return &LocalRuntime{
		workspaceRoot:        absRoot,
		commandTimeout:       commandTimeout,
		maxCommandOutputSize: maxOutput,
		maxFileBytes:         maxFileBytes,
		maxWorkspaceBytes:    maxWorkspaceBytes,
		workspaceTTL:         workspaceTTL,
		cleanupInterval:      cleanupInterval,
		inheritedEnvironment: inherited,
		environment:          environment,
		audit:                config.Audit,
		keepOnClose:          config.KeepWorkspacesOnClose,
		skills:               validatedSkills,
		sessions:             make(map[string]*sessionState),
	}, nil
}

func (r *LocalRuntime) tools() ([]tool.Tool, error) {
	result := make([]tool.Tool, 0, 4)
	readTool, err := functiontool.New(functiontool.Config{
		Name: "read_file", Description: "Read a UTF-8 text file from the current session workspace with line numbers.",
	}, r.readFile)
	if err != nil {
		return nil, fmt.Errorf("create read_file tool: %w", err)
	}
	result = append(result, readTool)
	writeTool, err := functiontool.New(functiontool.Config{
		Name: "write_file", Description: "Atomically write a UTF-8 text file in the current session workspace. Skill source files are read-only.",
	}, r.writeFile)
	if err != nil {
		return nil, fmt.Errorf("create write_file tool: %w", err)
	}
	result = append(result, writeTool)
	editTool, err := functiontool.New(functiontool.Config{
		Name: "edit_file", Description: "Atomically replace exact text in a file in the current session workspace.",
	}, r.editFile)
	if err != nil {
		return nil, fmt.Errorf("create edit_file tool: %w", err)
	}
	result = append(result, editTool)
	bashTool, err := functiontool.New(functiontool.Config{
		Name: "bash", Description: "Run a bounded bash command in the current session workspace for a trusted skill workflow.",
	}, r.bash)
	if err != nil {
		return nil, fmt.Errorf("create bash tool: %w", err)
	}
	return append(result, bashTool), nil
}

type readFileArgs struct {
	FilePath string `json:"file_path" jsonschema:"Relative path inside the session workspace."`
	Offset   int    `json:"offset,omitempty" jsonschema:"Optional first line to read, starting at 1."`
	Limit    int    `json:"limit,omitempty" jsonschema:"Optional maximum number of lines."`
}

type writeFileArgs struct {
	FilePath string `json:"file_path" jsonschema:"Relative writable path inside the session workspace."`
	Content  string `json:"content" jsonschema:"UTF-8 text to write."`
}

type editFileArgs struct {
	FilePath   string `json:"file_path" jsonschema:"Relative writable path inside the session workspace."`
	OldString  string `json:"old_string" jsonschema:"Exact text to replace."`
	NewString  string `json:"new_string" jsonschema:"Replacement text."`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"Replace every occurrence when true."`
}

type bashArgs struct {
	Command     string  `json:"command" jsonschema:"Bash command to execute."`
	Description string  `json:"description,omitempty" jsonschema:"Short non-sensitive description of the command."`
	Timeout     float64 `json:"timeout,omitempty" jsonschema:"Optional timeout in seconds, capped by the runtime maximum."`
}

func (r *LocalRuntime) readFile(ctx tool.Context, args readFileArgs) (map[string]any, error) {
	sessionID, err := toolSessionID(ctx)
	if err != nil {
		return toolError("WORKSPACE_ERROR", err), nil
	}
	state, workspace, err := r.lockSession(sessionID)
	if err != nil {
		return toolError("WORKSPACE_ERROR", err), nil
	}
	defer state.mu.Unlock()

	cleaned, skill, resource, err := r.resolveToolPath(args.FilePath, false)
	if err != nil {
		return toolError("INVALID_FILE_PATH", err), nil
	}
	var data []byte
	if skill != nil {
		data, err = skill.ReadResourceContext(ctx, resource, r.maxFileBytes)
	} else {
		data, err = readWorkspaceFile(ctx, workspace, cleaned, r.maxFileBytes)
	}
	if err != nil {
		return localReadError(err), nil
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return toolError("BINARY_FILE", errors.New("read_file supports UTF-8 text files only")), nil
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	start := args.Offset
	if start <= 0 {
		start = 1
	}
	limit := args.Limit
	if limit <= 0 || limit > maxReadLines {
		limit = maxReadLines
	}
	startIndex := start - 1
	if startIndex >= len(lines) {
		return map[string]any{"content": "", "line_count": len(lines)}, nil
	}
	end := min(startIndex+limit, len(lines))
	var output strings.Builder
	for index, line := range lines[startIndex:end] {
		if len(line) > 2_000 {
			cut := 2_000
			for cut > 0 && !utf8.ValidString(line[:cut]) {
				cut--
			}
			line = line[:cut] + "..."
		}
		_, _ = fmt.Fprintf(&output, "%6d|%s", startIndex+index+1, line)
		if index < end-startIndex-1 {
			output.WriteByte('\n')
		}
	}
	return map[string]any{"content": output.String(), "line_count": len(lines)}, nil
}

func (r *LocalRuntime) writeFile(ctx tool.Context, args writeFileArgs) (map[string]any, error) {
	if int64(len(args.Content)) > r.maxFileBytes {
		return toolError("FILE_TOO_LARGE", fmt.Errorf("content exceeds %d bytes", r.maxFileBytes)), nil
	}
	sessionID, err := toolSessionID(ctx)
	if err != nil {
		return toolError("WORKSPACE_ERROR", err), nil
	}
	state, workspace, err := r.lockSession(sessionID)
	if err != nil {
		return toolError("WORKSPACE_ERROR", err), nil
	}
	defer state.mu.Unlock()
	cleaned, _, _, err := r.resolveToolPath(args.FilePath, true)
	if err != nil {
		return toolError("INVALID_FILE_PATH", err), nil
	}
	if err := r.atomicWrite(workspace, cleaned, []byte(args.Content)); err != nil {
		return localWriteError(err), nil
	}
	return map[string]any{
		"status": "success", "path": filepath.ToSlash(cleaned), "bytes_written": len(args.Content),
	}, nil
}

func (r *LocalRuntime) editFile(ctx tool.Context, args editFileArgs) (map[string]any, error) {
	if args.OldString == args.NewString || args.OldString == "" {
		return toolError("INVALID_REPLACEMENT", errors.New("old_string must be non-empty and different from new_string")), nil
	}
	sessionID, err := toolSessionID(ctx)
	if err != nil {
		return toolError("WORKSPACE_ERROR", err), nil
	}
	state, workspace, err := r.lockSession(sessionID)
	if err != nil {
		return toolError("WORKSPACE_ERROR", err), nil
	}
	defer state.mu.Unlock()
	cleaned, _, _, err := r.resolveToolPath(args.FilePath, true)
	if err != nil {
		return toolError("INVALID_FILE_PATH", err), nil
	}
	data, err := readWorkspaceFile(ctx, workspace, cleaned, r.maxFileBytes)
	if err != nil {
		return localReadError(err), nil
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return toolError("BINARY_FILE", errors.New("edit_file supports UTF-8 text files only")), nil
	}
	content := string(data)
	count := strings.Count(content, args.OldString)
	if count == 0 {
		return toolError("TEXT_NOT_FOUND", errors.New("old_string was not found")), nil
	}
	if count > 1 && !args.ReplaceAll {
		return toolError("TEXT_NOT_UNIQUE", fmt.Errorf("old_string appears %d times", count)), nil
	}
	replacements := 1
	if args.ReplaceAll {
		replacements = -1
	}
	updated := strings.Replace(content, args.OldString, args.NewString, replacements)
	if int64(len(updated)) > r.maxFileBytes {
		return toolError("FILE_TOO_LARGE", fmt.Errorf("edited file exceeds %d bytes", r.maxFileBytes)), nil
	}
	if err := r.atomicWrite(workspace, cleaned, []byte(updated)); err != nil {
		return localWriteError(err), nil
	}
	replaced := 1
	if args.ReplaceAll {
		replaced = count
	}
	return map[string]any{
		"status": "success", "path": filepath.ToSlash(cleaned), "replacements": replaced,
	}, nil
}

func (r *LocalRuntime) bash(ctx tool.Context, args bashArgs) (map[string]any, error) {
	if strings.TrimSpace(args.Command) == "" {
		return toolError("MISSING_COMMAND", errors.New("command is required")), nil
	}
	sessionID, err := toolSessionID(ctx)
	if err != nil {
		return toolError("WORKSPACE_ERROR", err), nil
	}
	timeout := r.commandTimeout
	if math.IsNaN(args.Timeout) || math.IsInf(args.Timeout, 0) || args.Timeout < 0 {
		return toolError("INVALID_TIMEOUT", errors.New("timeout must be a non-negative finite number")), nil
	}
	if args.Timeout > 0 {
		if args.Timeout < timeout.Seconds() {
			timeout = time.Duration(args.Timeout * float64(time.Second))
		}
	}
	return r.runCommand(
		ctx, sessionID, "bash", "bash", []string{"-lc", args.Command},
		timeout, args.Description, nil,
	), nil
}

func (r *LocalRuntime) runSkillScript(
	ctx tool.Context,
	skill *skills.Skill,
	requested string,
	args []string,
) map[string]any {
	sessionID, err := toolSessionID(ctx)
	if err != nil {
		return toolError("WORKSPACE_ERROR", err)
	}
	if len(args) > maxScriptArguments {
		return toolError("TOO_MANY_ARGUMENTS", fmt.Errorf("script accepts at most %d arguments", maxScriptArguments))
	}
	for _, arg := range args {
		if len(arg) > maxScriptArgumentBytes || strings.IndexByte(arg, 0) >= 0 {
			return toolError(
				"ARGUMENT_TOO_LARGE",
				fmt.Errorf("each script argument must not exceed %d bytes or contain NUL", maxScriptArgumentBytes),
			)
		}
	}
	name, err := cleanScriptPath(requested)
	if err != nil {
		return toolError("INVALID_SCRIPT_PATH", err)
	}
	resource := filepath.ToSlash(filepath.Join("scripts", name))
	if _, err := skill.ReadResourceContext(ctx, resource, r.maxFileBytes); err != nil {
		return toolError("SCRIPT_NOT_FOUND", errors.New("script is unavailable or invalid"))
	}
	program, err := scriptInterpreter(name)
	if err != nil {
		return toolError("UNSUPPORTED_SCRIPT_TYPE", err)
	}
	scriptArg := filepath.Join("skills", skill.Name(), "scripts", name)
	commandArgs := append([]string{scriptArg}, args...)
	return r.runCommand(
		ctx, sessionID, "skill_script", program, commandArgs, r.commandTimeout,
		skill.Name()+":"+filepath.ToSlash(name), []string{skill.GetSkillPath()},
	)
}

func cleanScriptPath(requested string) (string, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(requested), "\\", "/")
	normalized = strings.TrimPrefix(normalized, "scripts/")
	if normalized == "" || filepath.IsAbs(filepath.FromSlash(normalized)) || hasWindowsAbsolutePrefix(normalized) {
		return "", errors.New("script path must be relative to scripts/")
	}
	cleaned := filepath.Clean(filepath.FromSlash(normalized))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errors.New("script path escapes the skill scripts directory")
	}
	return cleaned, nil
}

func scriptInterpreter(scriptPath string) (string, error) {
	switch strings.ToLower(filepath.Ext(scriptPath)) {
	case ".py":
		return "python3", nil
	case ".js", ".mjs", ".cjs":
		return "node", nil
	case ".sh", ".bash":
		return "bash", nil
	default:
		return "", fmt.Errorf("script type %q is not supported", filepath.Ext(scriptPath))
	}
}

func (r *LocalRuntime) runCommand(
	ctx context.Context,
	sessionID, kind, program string,
	args []string,
	timeout time.Duration,
	description string,
	pythonPaths []string,
) map[string]any {
	state, workspace, err := r.lockSession(sessionID)
	if err != nil {
		return toolError("WORKSPACE_ERROR", err)
	}
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(commandContext, program, args...)
	command.Dir = workspace
	command.Env = r.commandEnvironment(workspace, pythonPaths...)
	configureProcessGroup(command)
	command.WaitDelay = 2 * time.Second
	command.Cancel = func() error { return killProcessGroup(command) }
	stdout := &limitedBuffer{limit: r.maxCommandOutputSize}
	stderr := &limitedBuffer{limit: r.maxCommandOutputSize}
	command.Stdout = stdout
	command.Stderr = stderr
	startedAt := time.Now()
	runErr := command.Start()
	if runErr == nil {
		state.active[command] = struct{}{}
	}
	state.mu.Unlock()

	quotaExceeded := false
	if runErr == nil {
		runErr, quotaExceeded = r.waitCommand(commandContext, command, workspace)
	}

	state.mu.Lock()
	delete(state.active, command)
	_ = r.touchWorkspace(workspace)
	state.mu.Unlock()

	exitCode := 0
	status := "success"
	if quotaExceeded {
		status = "quota_exceeded"
	}
	if runErr != nil {
		status = "error"
		exitCode = -1
		var exitError *exec.ExitError
		if errors.As(runErr, &exitError) {
			exitCode = exitError.ExitCode()
		}
		switch {
		case quotaExceeded:
			status = "quota_exceeded"
		case errors.Is(commandContext.Err(), context.DeadlineExceeded):
			status = "timeout"
		case errors.Is(commandContext.Err(), context.Canceled):
			status = "canceled"
		}
	}
	result := map[string]any{
		"status": status, "exit_code": exitCode, "stdout": stdout.String(),
		"stderr": stderr.String(), "truncated": stdout.Truncated() || stderr.Truncated(),
	}
	if runErr != nil && status == "error" && stderr.Len() == 0 {
		result["error"] = "command execution failed"
	}
	r.emitAudit(CommandAuditEvent{
		SessionIDHash:     sessionHash(sessionID),
		Kind:              kind,
		Program:           program,
		DescriptionSHA256: textDigest(description),
		CommandSHA256:     commandDigest(program, args),
		ArgumentCount:     len(args),
		StartedAt:         startedAt,
		Duration:          time.Since(startedAt),
		Status:            status,
		ExitCode:          exitCode,
		Truncated:         stdout.Truncated() || stderr.Truncated(),
	})
	return result
}

func (r *LocalRuntime) waitCommand(
	ctx context.Context,
	command *exec.Cmd,
	workspace string,
) (error, bool) {
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			size, sizeErr := workspaceSize(workspace)
			return err, sizeErr == nil && size > r.maxWorkspaceBytes
		case <-ctx.Done():
			_ = killProcessGroup(command)
			return <-done, false
		case <-ticker.C:
			size, err := workspaceSize(workspace)
			if err == nil && size > r.maxWorkspaceBytes {
				_ = killProcessGroup(command)
				return <-done, true
			}
		}
	}
}

func (r *LocalRuntime) commandEnvironment(workspace string, pythonPaths ...string) []string {
	values := make(map[string]string, len(r.inheritedEnvironment)+len(r.environment)+8)
	for _, name := range r.inheritedEnvironment {
		if value, ok := os.LookupEnv(name); ok {
			values[name] = value
		}
	}
	for name, value := range r.environment {
		values[name] = value
	}
	paths := make([]string, 0, len(pythonPaths)+1)
	for _, path := range pythonPaths {
		if strings.TrimSpace(path) != "" {
			paths = append(paths, path)
		}
	}
	if current := values["PYTHONPATH"]; current != "" {
		paths = append(paths, current)
	}
	values["PYTHONPATH"] = strings.Join(paths, string(os.PathListSeparator))
	values["HOME"] = workspace
	values["TMPDIR"] = filepath.Join(workspace, "tmp")
	values["VEADK_WORKSPACE"] = workspace
	values["VEADK_UPLOADS_DIR"] = filepath.Join(workspace, "uploads")
	values["VEADK_OUTPUTS_DIR"] = filepath.Join(workspace, "outputs")
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name+"="+values[name])
	}
	return result
}

func validateInheritedEnvironment(names []string) ([]string, error) {
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if !validEnvironmentName(name) {
			return nil, fmt.Errorf("invalid inherited environment name %q", raw)
		}
		if sensitiveEnvironmentName(name) {
			return nil, fmt.Errorf("sensitive environment %q must be provided explicitly", name)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

func validateExplicitEnvironment(values map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for name, value := range values {
		if !validEnvironmentName(name) || strings.IndexByte(value, 0) >= 0 {
			return nil, fmt.Errorf("invalid explicit environment entry %q", name)
		}
		result[name] = value
	}
	return result, nil
}

func validEnvironmentName(name string) bool {
	if name == "" || name[0] == '=' || strings.ContainsAny(name, "=\x00") {
		return false
	}
	for index, char := range name {
		if (char >= 'A' && char <= 'Z') || char == '_' || (index > 0 && char >= '0' && char <= '9') {
			continue
		}
		return false
	}
	return true
}

func sensitiveEnvironmentName(name string) bool {
	upper := strings.ToUpper(name)
	for _, marker := range []string{
		"ACCESS_KEY", "API_KEY", "CREDENTIAL", "PASSWORD",
		"PRIVATE_KEY", "SECRET", "SESSION_TOKEN", "TIP_TOKEN",
	} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return strings.HasSuffix(upper, "_TOKEN")
}

func textDigest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func commandDigest(program string, args []string) string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, program)
	for _, arg := range args {
		hash.Write([]byte{0})
		_, _ = io.WriteString(hash, arg)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (r *LocalRuntime) emitAudit(event CommandAuditEvent) {
	if r.audit != nil {
		r.audit(event)
	}
}

func (r *LocalRuntime) resolveToolPath(
	requested string,
	write bool,
) (string, *skills.Skill, string, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(requested), "\\", "/")
	if normalized == "" || filepath.IsAbs(filepath.FromSlash(normalized)) || hasWindowsAbsolutePrefix(normalized) {
		return "", nil, "", errors.New("path must be relative to the session workspace")
	}
	cleaned := filepath.Clean(filepath.FromSlash(normalized))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", nil, "", errors.New("path escapes the session workspace")
	}
	parts := strings.Split(filepath.ToSlash(cleaned), "/")
	if parts[0] != "skills" {
		return cleaned, nil, "", nil
	}
	if write {
		return "", nil, "", errors.New("skill source files are read-only")
	}
	if len(parts) < 3 {
		return "", nil, "", errors.New("path must identify a file inside a skill")
	}
	skill, ok := r.skills[parts[1]]
	if !ok {
		return "", nil, "", fmt.Errorf("skill %q is not available", parts[1])
	}
	return cleaned, skill, strings.Join(parts[2:], "/"), nil
}

func hasWindowsAbsolutePrefix(path string) bool {
	return len(path) >= 3 &&
		((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) &&
		path[1] == ':' && path[2] == '/'
}

func toolSessionID(ctx tool.Context) (string, error) {
	if ctx == nil || strings.TrimSpace(ctx.SessionID()) == "" {
		return "", errors.New("session ID is required")
	}
	return ctx.SessionID(), nil
}

func sessionHash(sessionID string) string {
	hash := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(hash[:16])
}

func (r *LocalRuntime) session(sessionID string) (*sessionState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("local runtime is closed")
	}
	state := r.sessions[sessionID]
	if state == nil {
		state = &sessionState{active: make(map[*exec.Cmd]struct{})}
		r.sessions[sessionID] = state
	}
	return state, nil
}

func (r *LocalRuntime) lockSession(sessionID string) (*sessionState, string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, "", errors.New("session ID is required")
	}
	state, err := r.session(sessionID)
	if err != nil {
		return nil, "", err
	}
	if err := r.maybeCleanupExpired(); err != nil {
		return nil, "", err
	}
	state.mu.Lock()
	workspace, err := r.ensureWorkspaceLocked(sessionID, state)
	if err != nil {
		state.mu.Unlock()
		return nil, "", err
	}
	return state, workspace, nil
}

func (r *LocalRuntime) ensureWorkspaceLocked(sessionID string, state *sessionState) (string, error) {
	if state.closing {
		return "", errors.New("session workspace is closing")
	}
	workspace := filepath.Join(r.workspaceRoot, sessionHash(sessionID))
	if err := ensureDirectory(workspace, 0o700); err != nil {
		return "", fmt.Errorf("create session workspace: %w", err)
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return "", fmt.Errorf("open session workspace: %w", err)
	}
	defer func() { _ = root.Close() }()
	for _, subdir := range []string{"skills", "uploads", "outputs", "tmp"} {
		if err := root.MkdirAll(subdir, 0o700); err != nil {
			return "", fmt.Errorf("create workspace directory: %w", err)
		}
	}
	for name, skill := range r.skills {
		target, err := filepath.EvalSymlinks(skill.GetSkillPath())
		if err != nil {
			return "", fmt.Errorf("resolve skill %q: %w", name, err)
		}
		link := filepath.Join("skills", name)
		current, readErr := root.Readlink(link)
		switch {
		case readErr == nil && current != target:
			return "", fmt.Errorf("workspace skill link %q points to an unexpected target", name)
		case readErr == nil:
			continue
		case !errors.Is(readErr, fs.ErrNotExist):
			return "", fmt.Errorf("inspect workspace skill link %q: %w", name, readErr)
		}
		if err := root.Symlink(target, link); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("link skill %q: %w", name, err)
		}
	}
	state.owned = true
	if err := r.touchWorkspace(workspace); err != nil {
		return "", err
	}
	return workspace, nil
}

func ensureDirectory(path string, mode fs.FileMode) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(path, mode); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path is not a real directory")
	}
	return nil
}

func (r *LocalRuntime) touchWorkspace(workspace string) error {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	marker, err := root.OpenFile(workspaceMarker, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	return marker.Close()
}

func (r *LocalRuntime) maybeCleanupExpired() error {
	if r.workspaceTTL < 0 || r.cleanupInterval < 0 {
		return nil
	}
	r.mu.Lock()
	now := time.Now()
	if !r.lastCleanup.IsZero() && now.Sub(r.lastCleanup) < r.cleanupInterval {
		r.mu.Unlock()
		return nil
	}
	r.lastCleanup = now
	r.mu.Unlock()
	return r.cleanupExpired(now)
}

func (r *LocalRuntime) cleanupExpired(now time.Time) error {
	entries, err := os.ReadDir(r.workspaceRoot)
	if err != nil {
		return fmt.Errorf("read workspace root: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !isSessionDirectoryName(entry.Name()) {
			continue
		}
		workspace := filepath.Join(r.workspaceRoot, entry.Name())
		info, statErr := os.Stat(filepath.Join(workspace, workspaceMarker))
		if errors.Is(statErr, fs.ErrNotExist) {
			info, statErr = entry.Info()
		}
		if statErr != nil || now.Sub(info.ModTime()) <= r.workspaceTTL {
			continue
		}
		if r.sessionHashActive(entry.Name()) {
			continue
		}
		if err := os.RemoveAll(workspace); err != nil {
			return fmt.Errorf("remove expired workspace: %w", err)
		}
	}
	return nil
}

func isSessionDirectoryName(name string) bool {
	if len(name) != 32 {
		return false
	}
	_, err := hex.DecodeString(name)
	return err == nil
}

func (r *LocalRuntime) sessionHashActive(hash string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for sessionID, state := range r.sessions {
		if sessionHash(sessionID) != hash {
			continue
		}
		state.mu.Lock()
		active := len(state.active) != 0
		state.mu.Unlock()
		return active
	}
	return false
}

func (r *LocalRuntime) atomicWrite(workspace, name string, data []byte) error {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	parent := filepath.Dir(name)
	if err := root.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	if info, statErr := root.Lstat(name); statErr == nil &&
		(!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return errInvalidLocalFileType
	} else if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return statErr
	}
	currentSize, err := workspaceSize(workspace)
	if err != nil {
		return err
	}
	oldSize := int64(0)
	if info, statErr := root.Stat(name); statErr == nil {
		oldSize = info.Size()
	}
	if currentSize-oldSize+int64(len(data)) > r.maxWorkspaceBytes {
		return errWorkspaceQuota
	}
	tempName, err := randomTempName(parent)
	if err != nil {
		return err
	}
	temp, err := root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = temp.Close()
		if !ok {
			_ = root.Remove(tempName)
		}
	}()
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := root.Rename(tempName, name); err != nil {
		return err
	}
	ok = true
	return nil
}

func randomTempName(parent string) (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return filepath.Join(parent, ".veadk-write-"+hex.EncodeToString(value[:])), nil
}

var (
	errLocalFileTooLarge    = errors.New("local file is too large")
	errInvalidLocalFileType = errors.New("local file is not a regular file")
	errLocalFileChanged     = errors.New("local file changed while reading")
	errWorkspaceQuota       = errors.New("workspace quota exceeded")
)

func readWorkspaceFile(ctx context.Context, workspace, name string, maxBytes int64) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	before, err := root.Stat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errInvalidLocalFileType
	}
	if before.Size() > maxBytes {
		return nil, errLocalFileTooLarge
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	afterOpen, err := file.Stat()
	if err != nil || !afterOpen.Mode().IsRegular() {
		return nil, errInvalidLocalFileType
	}
	if !os.SameFile(before, afterOpen) {
		return nil, errLocalFileChanged
	}
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: file}, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errLocalFileTooLarge
	}
	afterRead, err := file.Stat()
	if err != nil ||
		!os.SameFile(afterOpen, afterRead) ||
		afterRead.Size() != afterOpen.Size() ||
		!afterRead.ModTime().Equal(afterOpen.ModTime()) {
		return nil, errLocalFileChanged
	}
	return data, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	count, err := r.reader.Read(data)
	if err == nil {
		err = r.ctx.Err()
	}
	return count, err
}

func workspaceSize(workspace string) (int64, error) {
	var size int64
	err := filepath.WalkDir(workspace, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func localReadError(err error) map[string]any {
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, skills.ErrResourceNotFound):
		return toolError("FILE_NOT_FOUND", errors.New("file was not found"))
	case errors.Is(err, errLocalFileTooLarge), errors.Is(err, skills.ErrResourceTooLarge):
		return toolError("FILE_TOO_LARGE", errors.New("file exceeds the configured size limit"))
	case errors.Is(err, errInvalidLocalFileType), errors.Is(err, skills.ErrResourceNotRegular):
		return toolError("INVALID_FILE_TYPE", errors.New("path is not a regular file"))
	case errors.Is(err, errLocalFileChanged), errors.Is(err, skills.ErrResourceChanged):
		return toolError("FILE_CHANGED", errors.New("file changed while it was being read"))
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return toolError("FILE_READ_CANCELED", errors.New("file read was canceled"))
	default:
		return toolError("READ_FILE_ERROR", errors.New("file could not be read"))
	}
}

func localWriteError(err error) map[string]any {
	switch {
	case errors.Is(err, errWorkspaceQuota):
		return toolError("WORKSPACE_QUOTA_EXCEEDED", errors.New("workspace quota would be exceeded"))
	case errors.Is(err, errInvalidLocalFileType):
		return toolError("INVALID_FILE_TYPE", errors.New("destination is not a regular file"))
	default:
		return toolError("WRITE_FILE_ERROR", errors.New("file could not be written"))
	}
}

func toolError(code string, err error) map[string]any {
	return map[string]any{"error_code": code, "error": err.Error()}
}

type limitedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	originalLength := len(data)
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return originalLength, nil
	}
	if len(data) > remaining {
		_, _ = b.buffer.Write(data[:remaining])
		b.truncated = true
		return originalLength, nil
	}
	_, _ = b.buffer.Write(data)
	return originalLength, nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func (b *limitedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Len()
}

func (b *limitedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// WorkspacePath prepares and returns a session workspace path.
func (s *SkillToolset) WorkspacePath(sessionID string) (string, error) {
	if s == nil || s.localRuntime == nil {
		return "", errors.New("local runtime is not enabled")
	}
	state, workspace, err := s.localRuntime.lockSession(sessionID)
	if err != nil {
		return "", err
	}
	state.mu.Unlock()
	return workspace, nil
}

// WriteUpload atomically stages an upload under uploads/ for a session.
func (s *SkillToolset) WriteUpload(
	ctx context.Context,
	sessionID, name string,
	source io.Reader,
) error {
	if s == nil || s.localRuntime == nil {
		return errors.New("local runtime is not enabled")
	}
	if source == nil {
		return errors.New("upload source is required")
	}
	cleaned, err := scopedPath("uploads", name)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(
		&contextReader{ctx: ctx, reader: source},
		s.localRuntime.maxFileBytes+1,
	))
	if err != nil {
		return err
	}
	if int64(len(data)) > s.localRuntime.maxFileBytes {
		return errLocalFileTooLarge
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	state, workspace, err := s.localRuntime.lockSession(sessionID)
	if err != nil {
		return err
	}
	defer state.mu.Unlock()
	return s.localRuntime.atomicWrite(workspace, cleaned, data)
}

// ReadOutput reads a regular output file with the configured file limit.
func (s *SkillToolset) ReadOutput(
	ctx context.Context,
	sessionID, name string,
) ([]byte, error) {
	if s == nil || s.localRuntime == nil {
		return nil, errors.New("local runtime is not enabled")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cleaned, err := scopedPath("outputs", name)
	if err != nil {
		return nil, err
	}
	state, workspace, err := s.localRuntime.lockSession(sessionID)
	if err != nil {
		return nil, err
	}
	defer state.mu.Unlock()
	return readWorkspaceFile(ctx, workspace, cleaned, s.localRuntime.maxFileBytes)
}

func scopedPath(scope, name string) (string, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	if normalized == "" || filepath.IsAbs(filepath.FromSlash(normalized)) || hasWindowsAbsolutePrefix(normalized) {
		return "", fmt.Errorf("path must stay under %s/", scope)
	}
	cleaned := filepath.Clean(filepath.FromSlash(normalized))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path must stay under %s/", scope)
	}
	return filepath.Join(scope, cleaned), nil
}

// CleanupSession kills active commands and removes one session workspace.
func (s *SkillToolset) CleanupSession(ctx context.Context, sessionID string) error {
	if s == nil || s.localRuntime == nil {
		return errors.New("local runtime is not enabled")
	}
	return s.localRuntime.cleanupSession(ctx, sessionID)
}

// CleanupExpired removes inactive workspaces older than WorkspaceTTL.
func (s *SkillToolset) CleanupExpired(ctx context.Context) error {
	if s == nil || s.localRuntime == nil {
		return errors.New("local runtime is not enabled")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.localRuntime.workspaceTTL < 0 {
		return nil
	}
	return s.localRuntime.cleanupExpired(time.Now())
}

func (r *LocalRuntime) cleanupSession(ctx context.Context, sessionID string) error {
	state, err := r.session(sessionID)
	if err != nil {
		return err
	}
	state.mu.Lock()
	state.closing = true
	for command := range state.active {
		_ = killProcessGroup(command)
	}
	state.mu.Unlock()

	deadline := time.NewTicker(10 * time.Millisecond)
	defer deadline.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		state.mu.Lock()
		active := len(state.active)
		state.mu.Unlock()
		if active == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
		}
	}
	state.mu.Lock()
	if state.owned {
		if err := os.RemoveAll(filepath.Join(r.workspaceRoot, sessionHash(sessionID))); err != nil {
			state.mu.Unlock()
			return err
		}
		state.owned = false
	}
	state.mu.Unlock()
	r.mu.Lock()
	delete(r.sessions, sessionID)
	r.mu.Unlock()
	return nil
}

// Close stops commands and removes workspaces created by this runtime unless
// KeepWorkspacesOnClose was configured.
func (s *SkillToolset) Close() error {
	if s == nil || s.localRuntime == nil {
		return nil
	}
	return s.localRuntime.Close()
}

// Close stops all commands and closes the runtime.
func (r *LocalRuntime) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	sessions := make(map[string]*sessionState, len(r.sessions))
	for sessionID, state := range r.sessions {
		sessions[sessionID] = state
	}
	r.mu.Unlock()
	var errs []error
	for _, state := range sessions {
		state.mu.Lock()
		state.closing = true
		for command := range state.active {
			_ = killProcessGroup(command)
		}
		state.mu.Unlock()
	}
	deadline := time.Now().Add(2 * time.Second)
	for sessionID, state := range sessions {
		for {
			state.mu.Lock()
			active := len(state.active)
			owned := state.owned
			state.mu.Unlock()
			if active == 0 {
				if owned && !r.keepOnClose {
					if err := os.RemoveAll(filepath.Join(r.workspaceRoot, sessionHash(sessionID))); err != nil {
						errs = append(errs, err)
					}
				}
				break
			}
			if time.Now().After(deadline) {
				errs = append(errs, fmt.Errorf("session %s did not stop before close deadline", sessionHash(sessionID)))
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	return errors.Join(errs...)
}
