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
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine/veadk-go/skills"
	"google.golang.org/adk/agent"
	adkllmagent "google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

type localToolContext struct {
	tool.Context
	base      context.Context
	sessionID string
}

func (c *localToolContext) SessionID() string {
	return c.sessionID
}

func newLocalToolContext(ctx context.Context, sessionID string) *localToolContext {
	return &localToolContext{base: ctx, sessionID: sessionID}
}

func (c *localToolContext) Deadline() (time.Time, bool) {
	return c.base.Deadline()
}

func (c *localToolContext) Done() <-chan struct{} {
	return c.base.Done()
}

func (c *localToolContext) Err() error {
	return c.base.Err()
}

func (c *localToolContext) Value(key any) any {
	return c.base.Value(key)
}

func TestLocalRuntimeWorkspaceFileToolsAndLifecycle(t *testing.T) {
	skillsRoot := t.TempDir()
	writeRuntimeSkill(t, skillsRoot, "document-tools", map[string]string{
		"references/usage.md": "usage",
		"scripts/echo.sh":     "printf '%s' \"$1\"",
	})
	discovered, err := skills.DiscoverSkillsFromDir(skillsRoot)
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := t.TempDir()
	toolset, err := NewLocalSkillToolset(discovered, LocalRuntimeConfig{
		WorkspaceRoot: workspaceRoot,
		MaxFileBytes:  1024,
	})
	if err != nil {
		t.Fatal(err)
	}

	tools, err := toolset.Tools(nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, runtimeTool := range tools {
		names = append(names, runtimeTool.Name())
	}
	if got, want := strings.Join(names, ","), "list_skills,load_skill,load_skill_resource,run_skill_script,read_file,write_file,edit_file,bash"; got != want {
		t.Fatalf("tool names = %q, want %q", got, want)
	}

	ctx := newLocalToolContext(context.Background(), "session-a")
	writeResult, err := toolset.localRuntime.writeFile(ctx, writeFileArgs{
		FilePath: "outputs/result.txt",
		Content:  "hello old",
	})
	if err != nil || writeResult["status"] != "success" {
		t.Fatalf("writeFile() = %#v, %v", writeResult, err)
	}
	editResult, err := toolset.localRuntime.editFile(ctx, editFileArgs{
		FilePath:  "outputs/result.txt",
		OldString: "old",
		NewString: "new",
	})
	if err != nil || editResult["status"] != "success" {
		t.Fatalf("editFile() = %#v, %v", editResult, err)
	}
	readResult, err := toolset.localRuntime.readFile(ctx, readFileArgs{FilePath: "outputs/result.txt"})
	if err != nil || readResult["content"] != "     1|hello new" {
		t.Fatalf("readFile() = %#v, %v", readResult, err)
	}
	skillRead, err := toolset.localRuntime.readFile(ctx, readFileArgs{
		FilePath: "skills/document-tools/references/usage.md",
	})
	if err != nil || skillRead["content"] != "     1|usage" {
		t.Fatalf("read skill resource = %#v, %v", skillRead, err)
	}

	for _, invalid := range []string{
		"../escape.txt", "/tmp/escape.txt", `C:\escape.txt`,
		"skills/document-tools/SKILL.md",
	} {
		result, callErr := toolset.localRuntime.writeFile(ctx, writeFileArgs{
			FilePath: invalid,
			Content:  "no",
		})
		if callErr != nil {
			t.Fatal(callErr)
		}
		if result["error_code"] != "INVALID_FILE_PATH" {
			t.Errorf("writeFile(%q) = %#v, want INVALID_FILE_PATH", invalid, result)
		}
	}

	otherCtx := newLocalToolContext(context.Background(), "session-b")
	otherRead, err := toolset.localRuntime.readFile(otherCtx, readFileArgs{FilePath: "outputs/result.txt"})
	if err != nil || otherRead["error_code"] != "FILE_NOT_FOUND" {
		t.Fatalf("second session read = %#v, %v", otherRead, err)
	}

	if err := toolset.WriteUpload(context.Background(), "session-a", "input.txt", strings.NewReader("uploaded")); err != nil {
		t.Fatal(err)
	}
	uploadRead, err := toolset.localRuntime.readFile(ctx, readFileArgs{FilePath: "uploads/input.txt"})
	if err != nil || uploadRead["content"] != "     1|uploaded" {
		t.Fatalf("upload read = %#v, %v", uploadRead, err)
	}
	output, err := toolset.ReadOutput(context.Background(), "session-a", "result.txt")
	if err != nil || string(output) != "hello new" {
		t.Fatalf("ReadOutput() = %q, %v", output, err)
	}
	if err := toolset.WriteUpload(context.Background(), "session-a", "../escape", strings.NewReader("x")); err == nil {
		t.Fatal("WriteUpload traversal error = nil")
	}
	if _, err := toolset.ReadOutput(context.Background(), "session-a", "/absolute"); err == nil {
		t.Fatal("ReadOutput absolute path error = nil")
	}

	workspace, err := toolset.WorkspacePath("session-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := toolset.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace after Close() stat error = %v, want not exist", err)
	}
	if _, err := toolset.WorkspacePath("closed"); err == nil {
		t.Fatal("WorkspacePath after Close() error = nil")
	}
}

func TestLocalRuntimeRejectsSymlinkAndSpecialFileWrites(t *testing.T) {
	toolset := newRuntimeToolset(t, LocalRuntimeConfig{WorkspaceRoot: t.TempDir()})
	defer func() { _ = toolset.Close() }()
	workspace, err := toolset.WorkspacePath("session")
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "outputs", "escape.txt")); err != nil {
		t.Fatal(err)
	}
	ctx := newLocalToolContext(context.Background(), "session")
	result, err := toolset.localRuntime.writeFile(ctx, writeFileArgs{
		FilePath: "outputs/escape.txt",
		Content:  "changed",
	})
	if err != nil || result["error_code"] != "INVALID_FILE_TYPE" {
		t.Fatalf("symlink write = %#v, %v", result, err)
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "outside" {
		t.Fatalf("outside file = %q, %v", data, err)
	}

	if err := os.Mkdir(filepath.Join(workspace, "outputs", "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err = toolset.localRuntime.writeFile(ctx, writeFileArgs{
		FilePath: "outputs/directory",
		Content:  "changed",
	})
	if err != nil || result["error_code"] != "INVALID_FILE_TYPE" {
		t.Fatalf("directory write = %#v, %v", result, err)
	}
}

func TestLocalRuntimeRejectsUnsafeConfiguration(t *testing.T) {
	worldWritable := t.TempDir()
	if err := os.Chmod(worldWritable, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(worldWritable, 0o700) })
	if _, err := newLocalRuntime(
		LocalRuntimeConfig{WorkspaceRoot: worldWritable},
		map[string]*skills.Skill{},
	); err == nil {
		t.Fatal("world-writable WorkspaceRoot error = nil")
	}
	if _, err := newLocalRuntime(
		LocalRuntimeConfig{WorkspaceRoot: t.TempDir(), MaxFileBytes: -1},
		map[string]*skills.Skill{},
	); err == nil {
		t.Fatal("negative size error = nil")
	}
	inMemory := &skills.Skill{
		Frontmatter: &skills.Frontmatter{Name: "memory", Description: "memory"},
	}
	if _, err := NewLocalSkillToolset(
		[]*skills.Skill{inMemory},
		LocalRuntimeConfig{WorkspaceRoot: t.TempDir()},
	); err == nil {
		t.Fatal("in-memory Skill error = nil")
	}
}

func TestLocalRuntimeQuotaAtomicWriteAndConcurrentSessions(t *testing.T) {
	toolset := newRuntimeToolset(t, LocalRuntimeConfig{
		WorkspaceRoot:     t.TempDir(),
		MaxFileBytes:      128,
		MaxWorkspaceBytes: 180,
	})
	defer func() { _ = toolset.Close() }()
	ctx := newLocalToolContext(context.Background(), "quota")
	first, err := toolset.localRuntime.writeFile(ctx, writeFileArgs{
		FilePath: "outputs/first.txt",
		Content:  strings.Repeat("a", 100),
	})
	if err != nil || first["status"] != "success" {
		t.Fatalf("first write = %#v, %v", first, err)
	}
	tooLarge, err := toolset.localRuntime.writeFile(ctx, writeFileArgs{
		FilePath: "outputs/large.txt",
		Content:  strings.Repeat("b", 129),
	})
	if err != nil || tooLarge["error_code"] != "FILE_TOO_LARGE" {
		t.Fatalf("large write = %#v, %v", tooLarge, err)
	}
	quota, err := toolset.localRuntime.writeFile(ctx, writeFileArgs{
		FilePath: "outputs/second.txt",
		Content:  strings.Repeat("c", 100),
	})
	if err != nil || quota["error_code"] != "WORKSPACE_QUOTA_EXCEEDED" {
		t.Fatalf("quota write = %#v, %v", quota, err)
	}
	firstData, err := toolset.ReadOutput(context.Background(), "quota", "first.txt")
	if err != nil || string(firstData) != strings.Repeat("a", 100) {
		t.Fatalf("atomic original = %q, %v", firstData, err)
	}

	const sessions = 16
	var wg sync.WaitGroup
	errs := make(chan error, sessions)
	for index := 0; index < sessions; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("parallel-%d", index)
			sessionCtx := newLocalToolContext(context.Background(), sessionID)
			value := fmt.Sprintf("value-%d", index)
			result, err := toolset.localRuntime.writeFile(sessionCtx, writeFileArgs{
				FilePath: "outputs/value.txt",
				Content:  value,
			})
			if err != nil || result["status"] != "success" {
				errs <- fmt.Errorf("session %s write failed: %#v, %v", sessionID, result, err)
				return
			}
			data, err := toolset.ReadOutput(context.Background(), sessionID, "value.txt")
			if err != nil || string(data) != value {
				errs <- fmt.Errorf("session %s read %q: %v", sessionID, data, err)
			}
		}(index)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestLocalRuntimeEnvironmentExecutionAndAudit(t *testing.T) {
	t.Setenv("MODEL_AGENT_API_KEY", "fixture-parent-secret")
	t.Setenv("SAFE_RUNTIME_VALUE", "parent-safe")
	var (
		auditMu sync.Mutex
		audits  []CommandAuditEvent
	)
	toolset := newRuntimeToolset(t, LocalRuntimeConfig{
		WorkspaceRoot:        t.TempDir(),
		CommandTimeout:       2 * time.Second,
		MaxCommandOutputSize: 32,
		InheritEnvironment:   []string{"PATH", "SAFE_RUNTIME_VALUE"},
		Environment:          map[string]string{"EXPLICIT_TOKEN": "fixture-explicit"},
		Audit: func(event CommandAuditEvent) {
			auditMu.Lock()
			defer auditMu.Unlock()
			audits = append(audits, event)
		},
	})
	defer func() { _ = toolset.Close() }()
	ctx := newLocalToolContext(context.Background(), "exec")
	result, err := toolset.localRuntime.bash(ctx, bashArgs{
		Command:     `printf '%s|%s|%s' "$MODEL_AGENT_API_KEY" "$SAFE_RUNTIME_VALUE" "$EXPLICIT_TOKEN"; printf 'abcdefghijklmnopqrstuvwxyz'`,
		Description: "verify environment",
	})
	if err != nil || result["status"] != "success" {
		t.Fatalf("bash environment = %#v, %v", result, err)
	}
	stdout := result["stdout"].(string)
	if strings.Contains(stdout, "fixture-parent-secret") ||
		!strings.Contains(stdout, "|parent-safe|fixture-explicit") {
		t.Fatalf("filtered environment stdout = %q", stdout)
	}
	if result["truncated"] != true || len(stdout) != 32 {
		t.Fatalf("bounded stdout = %q (%d), truncated=%v", stdout, len(stdout), result["truncated"])
	}
	auditMu.Lock()
	defer auditMu.Unlock()
	if len(audits) != 1 {
		t.Fatalf("audit count = %d, want 1", len(audits))
	}
	event := audits[0]
	if event.SessionIDHash == "exec" || len(event.CommandSHA256) != 64 ||
		len(event.DescriptionSHA256) != 64 || !event.Truncated {
		t.Fatalf("audit event = %#v", event)
	}
	if strings.Contains(fmt.Sprintf("%#v", event), "fixture-explicit") {
		t.Fatal("audit event exposed command content")
	}

	if _, err := newLocalRuntime(LocalRuntimeConfig{
		WorkspaceRoot:      t.TempDir(),
		InheritEnvironment: []string{"MODEL_AGENT_API_KEY"},
	}, map[string]*skills.Skill{}); err == nil {
		t.Fatal("sensitive inherited environment error = nil")
	}
}

func TestLocalRuntimeScriptsTimeoutAndCancellation(t *testing.T) {
	root := t.TempDir()
	writeRuntimeSkill(t, root, "script-tools", map[string]string{
		"scripts/echo.sh":    `printf '%s|%s' "$1" "$PWD"`,
		"scripts/echo.py":    `import os,sys; print(sys.argv[1] + "|" + os.getcwd())`,
		"scripts/echo.mjs":   `console.log(process.argv[2] + "|" + process.cwd())`,
		"scripts/timeout.sh": "sleep 10",
		"scripts/plain.txt":  "not executable",
	})
	discovered, err := skills.DiscoverSkillsFromDir(root)
	if err != nil {
		t.Fatal(err)
	}
	toolset, err := NewLocalSkillToolset(discovered, LocalRuntimeConfig{
		WorkspaceRoot:  t.TempDir(),
		CommandTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = toolset.Close() }()
	ctx := newLocalToolContext(context.Background(), "scripts")
	injectionArg := "; touch outputs/argument-was-executed"
	result, err := toolset.runSkillScriptToolHandler(ctx, runSkillScriptArgs{
		SkillName:  "script-tools",
		ScriptPath: "scripts/echo.sh",
		Args:       []string{injectionArg},
	})
	if err != nil || result["status"] != "success" ||
		!strings.HasPrefix(result["stdout"].(string), injectionArg+"|") {
		t.Fatalf("shell script = %#v, %v", result, err)
	}
	workspace, err := toolset.WorkspacePath("scripts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "outputs", "argument-was-executed")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("script argument was interpreted as shell: %v", err)
	}

	pythonResult, err := toolset.runSkillScriptToolHandler(ctx, runSkillScriptArgs{
		SkillName: "script-tools", ScriptPath: "echo.py", Args: []string{"python"},
	})
	if err != nil || pythonResult["status"] != "success" ||
		!strings.HasPrefix(pythonResult["stdout"].(string), "python|") {
		t.Fatalf("python script = %#v, %v", pythonResult, err)
	}
	if _, lookupErr := exec.LookPath("node"); lookupErr == nil {
		nodeResult, err := toolset.runSkillScriptToolHandler(ctx, runSkillScriptArgs{
			SkillName: "script-tools", ScriptPath: "echo.mjs", Args: []string{"node"},
		})
		if err != nil || nodeResult["status"] != "success" ||
			!strings.HasPrefix(nodeResult["stdout"].(string), "node|") {
			t.Fatalf("node script = %#v, %v", nodeResult, err)
		}
	}
	timeoutToolset, err := NewLocalSkillToolset(discovered, LocalRuntimeConfig{
		WorkspaceRoot:  t.TempDir(),
		CommandTimeout: 80 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = timeoutToolset.Close() }()
	timeoutResult, err := timeoutToolset.runSkillScriptToolHandler(ctx, runSkillScriptArgs{
		SkillName: "script-tools", ScriptPath: "timeout.sh",
	})
	if err != nil || timeoutResult["status"] != "timeout" {
		t.Fatalf("timeout script = %#v, %v", timeoutResult, err)
	}
	unsupported, err := toolset.runSkillScriptToolHandler(ctx, runSkillScriptArgs{
		SkillName: "script-tools", ScriptPath: "plain.txt",
	})
	if err != nil || unsupported["error_code"] != "UNSUPPORTED_SCRIPT_TYPE" {
		t.Fatalf("unsupported script = %#v, %v", unsupported, err)
	}
	invalid, err := toolset.runSkillScriptToolHandler(ctx, runSkillScriptArgs{
		SkillName: "script-tools", ScriptPath: "../outside.sh",
	})
	if err != nil || invalid["error_code"] != "INVALID_SCRIPT_PATH" {
		t.Fatalf("invalid script = %#v, %v", invalid, err)
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancelToolCtx := newLocalToolContext(cancelCtx, "cancel")
	done := make(chan map[string]any, 1)
	go func() {
		result, _ := toolset.localRuntime.bash(cancelToolCtx, bashArgs{Command: "sleep 10"})
		done <- result
	}()
	time.Sleep(40 * time.Millisecond)
	cancel()
	select {
	case canceled := <-done:
		if canceled["status"] != "canceled" {
			t.Fatalf("canceled command = %#v", canceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled command did not stop")
	}
}

func TestLocalRuntimeCloseStopsActiveCommands(t *testing.T) {
	toolset := newRuntimeToolset(t, LocalRuntimeConfig{
		WorkspaceRoot:  t.TempDir(),
		CommandTimeout: 5 * time.Second,
	})
	ctx := newLocalToolContext(context.Background(), "close-active")
	workspace, err := toolset.WorkspacePath("close-active")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = toolset.localRuntime.bash(ctx, bashArgs{Command: "sleep 10"})
	}()
	deadline := time.Now().Add(time.Second)
	for {
		toolset.localRuntime.mu.Lock()
		state := toolset.localRuntime.sessions["close-active"]
		toolset.localRuntime.mu.Unlock()
		if state != nil {
			state.mu.Lock()
			active := len(state.active)
			state.mu.Unlock()
			if active == 1 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("command did not become active")
		}
		time.Sleep(time.Millisecond)
	}

	if err := toolset.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("active command did not stop during Close")
	}
	if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace after Close() stat error = %v, want not exist", err)
	}
}

func TestLocalRuntimeCommandWorkspaceQuota(t *testing.T) {
	toolset := newRuntimeToolset(t, LocalRuntimeConfig{
		WorkspaceRoot:     t.TempDir(),
		MaxWorkspaceBytes: 1024,
		CommandTimeout:    2 * time.Second,
	})
	defer func() { _ = toolset.Close() }()
	ctx := newLocalToolContext(context.Background(), "command-quota")
	result, err := toolset.localRuntime.bash(ctx, bashArgs{
		Command: "dd if=/dev/zero of=outputs/large.bin bs=4096 count=4 2>/dev/null",
	})
	if err != nil || result["status"] != "quota_exceeded" {
		t.Fatalf("quota command = %#v, %v", result, err)
	}
}

func TestLocalRuntimeSixSkillArtifactMatrix(t *testing.T) {
	root := t.TempDir()
	script := `
import os, pathlib, shutil, sys, zipfile
kind = sys.argv[1]
outputs = pathlib.Path(os.environ["VEADK_OUTPUTS_DIR"])
outputs.mkdir(parents=True, exist_ok=True)
if kind == "docx":
    with zipfile.ZipFile(outputs / "result.docx", "w") as z:
        z.writestr("[Content_Types].xml", "<Types/>")
        z.writestr("word/document.xml", "<document/>")
elif kind == "pptx":
    with zipfile.ZipFile(outputs / "result.pptx", "w") as z:
        z.writestr("[Content_Types].xml", "<Types/>")
        z.writestr("ppt/presentation.xml", "<presentation/>")
        z.writestr("ppt/slides/slide1.xml", "<slide/>")
elif kind == "xlsx":
    with zipfile.ZipFile(outputs / "result.xlsx", "w") as z:
        z.writestr("[Content_Types].xml", "<Types/>")
        z.writestr("xl/workbook.xml", "<workbook/>")
        z.writestr("xl/worksheets/sheet1.xml", "<worksheet/>")
elif kind == "pdf":
    (outputs / "result.pdf").write_bytes(b"%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\n%%EOF\n")
elif kind == "skill-creator":
    target = outputs / "generated-skill"
    target.mkdir()
    (target / "SKILL.md").write_text("---\nname: generated-skill\ndescription: generated\n---\n")
elif kind == "tos-file-access":
    shutil.copyfile(pathlib.Path(os.environ["VEADK_UPLOADS_DIR"]) / "input.txt", outputs / "downloaded.txt")
else:
    raise SystemExit("unknown fixture")
`
	names := []string{"docx", "pdf", "pptx", "xlsx", "skill-creator", "tos-file-access"}
	for _, name := range names {
		writeRuntimeSkill(t, root, name, map[string]string{"scripts/fixture.py": script})
	}
	discovered, err := skills.DiscoverSkillsFromDir(root)
	if err != nil {
		t.Fatal(err)
	}
	toolset, err := NewLocalSkillToolset(discovered, LocalRuntimeConfig{
		WorkspaceRoot:  t.TempDir(),
		CommandTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = toolset.Close() }()

	for _, name := range names {
		sessionID := "matrix-" + name
		if name == "tos-file-access" {
			if err := toolset.WriteUpload(context.Background(), sessionID, "input.txt", strings.NewReader("download fixture")); err != nil {
				t.Fatal(err)
			}
		}
		ctx := newLocalToolContext(context.Background(), sessionID)
		result, err := toolset.runSkillScriptToolHandler(ctx, runSkillScriptArgs{
			SkillName: name, ScriptPath: "fixture.py", Args: []string{name},
		})
		if err != nil || result["status"] != "success" {
			t.Fatalf("%s execution = %#v, %v", name, result, err)
		}
	}

	docx, err := toolset.ReadOutput(context.Background(), "matrix-docx", "result.docx")
	if err != nil {
		t.Fatal(err)
	}
	assertZipEntries(t, docx, "[Content_Types].xml", "word/document.xml")
	pptx, err := toolset.ReadOutput(context.Background(), "matrix-pptx", "result.pptx")
	if err != nil {
		t.Fatal(err)
	}
	assertZipEntries(t, pptx, "[Content_Types].xml", "ppt/presentation.xml", "ppt/slides/slide1.xml")
	xlsx, err := toolset.ReadOutput(context.Background(), "matrix-xlsx", "result.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	assertZipEntries(t, xlsx, "[Content_Types].xml", "xl/workbook.xml", "xl/worksheets/sheet1.xml")
	pdf, err := toolset.ReadOutput(context.Background(), "matrix-pdf", "result.pdf")
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) || !bytes.Contains(pdf, []byte("%%EOF")) {
		t.Fatalf("PDF structure invalid: %q, %v", pdf, err)
	}
	generated, err := toolset.ReadOutput(context.Background(), "matrix-skill-creator", "generated-skill/SKILL.md")
	if err != nil || !bytes.Contains(generated, []byte("name: generated-skill")) {
		t.Fatalf("generated Skill invalid: %q, %v", generated, err)
	}
	downloaded, err := toolset.ReadOutput(context.Background(), "matrix-tos-file-access", "downloaded.txt")
	if err != nil || string(downloaded) != "download fixture" {
		t.Fatalf("TOS fixture output = %q, %v", downloaded, err)
	}
}

type runtimeLoopModel struct {
	calls atomic.Int32
}

func (*runtimeLoopModel) Name() string {
	return "runtime-loop-model"
}

func (m *runtimeLoopModel) GenerateContent(
	_ context.Context,
	_ *adkmodel.LLMRequest,
	_ bool,
) iter.Seq2[*adkmodel.LLMResponse, error] {
	call := m.calls.Add(1)
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		if call == 1 {
			yield(&adkmodel.LLMResponse{Content: &genai.Content{
				Role: genai.RoleModel,
				Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
					ID:   "runtime-call",
					Name: "write_file",
					Args: map[string]any{
						"file_path": "outputs/model-loop.txt",
						"content":   "written through ADK",
					},
				}}},
			}}, nil)
			return
		}
		yield(&adkmodel.LLMResponse{
			Content: genai.NewContentFromText("done", genai.RoleModel),
		}, nil)
	}
}

func TestLocalRuntimeADKToolLoop(t *testing.T) {
	toolset := newRuntimeToolset(t, LocalRuntimeConfig{WorkspaceRoot: t.TempDir()})
	defer func() { _ = toolset.Close() }()
	model := &runtimeLoopModel{}
	runtimeAgent, err := adkllmagent.New(adkllmagent.Config{
		Name:     "runtime-agent",
		Model:    model,
		Toolsets: []tool.Toolset{toolset},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtimeRunner, err := runner.New(runner.Config{
		AppName:           "runtime-test",
		Agent:             runtimeAgent,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, runErr := range runtimeRunner.Run(
		context.Background(),
		"user",
		"model-session",
		genai.NewContentFromText("write a file", genai.RoleUser),
		agent.RunConfig{},
	) {
		if runErr != nil {
			t.Fatal(runErr)
		}
	}
	if model.calls.Load() != 2 {
		t.Fatalf("model calls = %d, want 2", model.calls.Load())
	}
	data, err := toolset.ReadOutput(
		context.Background(),
		"model-session",
		"model-loop.txt",
	)
	if err != nil || string(data) != "written through ADK" {
		t.Fatalf("ADK tool output = %q, %v", data, err)
	}
}

func assertZipEntries(t *testing.T, data []byte, names ...string) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	seen := make(map[string]bool, len(reader.File))
	for _, file := range reader.File {
		seen[file.Name] = true
	}
	for _, name := range names {
		if !seen[name] {
			t.Errorf("zip missing %q", name)
		}
	}
}

func TestLocalRuntimeCleanupExpiredAndExplicitSessionCleanup(t *testing.T) {
	workspaceRoot := t.TempDir()
	stale := filepath.Join(workspaceRoot, sessionHash("stale"))
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(stale, workspaceMarker)
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(marker, old, old); err != nil {
		t.Fatal(err)
	}

	toolset := newRuntimeToolset(t, LocalRuntimeConfig{
		WorkspaceRoot:   workspaceRoot,
		WorkspaceTTL:    time.Minute,
		CleanupInterval: time.Nanosecond,
	})
	defer func() { _ = toolset.Close() }()
	if _, err := toolset.WorkspacePath("current"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale workspace stat = %v, want not exist", err)
	}
	current, err := toolset.WorkspacePath("current")
	if err != nil {
		t.Fatal(err)
	}
	if err := toolset.CleanupSession(context.Background(), "current"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(current); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleaned session stat = %v, want not exist", err)
	}
}

func newRuntimeToolset(t *testing.T, config LocalRuntimeConfig) *SkillToolset {
	t.Helper()
	root := t.TempDir()
	writeRuntimeSkill(t, root, "runtime-skill", map[string]string{
		"scripts/noop.sh": "true",
	})
	discovered, err := skills.DiscoverSkillsFromDir(root)
	if err != nil {
		t.Fatal(err)
	}
	toolset, err := NewLocalSkillToolset(discovered, config)
	if err != nil {
		t.Fatal(err)
	}
	return toolset
}

func writeRuntimeSkill(t *testing.T, root, name string, files map[string]string) {
	t.Helper()
	skillDir := filepath.Join(root, name)
	document := fmt.Sprintf("---\nname: %s\ndescription: runtime fixture\n---\nUse the scripts.\n", name)
	writeRuntimeFile(t, filepath.Join(skillDir, "SKILL.md"), document)
	for name, content := range files {
		writeRuntimeFile(t, filepath.Join(skillDir, name), content)
	}
}

func writeRuntimeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
