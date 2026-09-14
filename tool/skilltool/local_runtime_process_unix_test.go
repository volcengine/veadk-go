//go:build unix

package skilltool

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLocalRuntimeCancellationKillsProcessGroup(t *testing.T) {
	toolset := newRuntimeToolset(t, LocalRuntimeConfig{
		WorkspaceRoot:  t.TempDir(),
		CommandTimeout: 5 * time.Second,
	})
	defer func() { _ = toolset.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	toolCtx := newLocalToolContext(ctx, "process-group")
	done := make(chan map[string]any, 1)
	go func() {
		result, _ := toolset.localRuntime.bash(toolCtx, bashArgs{
			Command: "sleep 30 & child=$!; printf '%s' \"$child\" > outputs/child.pid; wait",
		})
		done <- result
	}()

	var pid int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := toolset.ReadOutput(context.Background(), "process-group", "child.pid")
		if err == nil {
			pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("child pid was not written")
	}
	cancel()
	select {
	case result := <-done:
		if result["status"] != "canceled" {
			t.Fatalf("canceled process group = %#v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("process group did not stop")
	}

	for deadline = time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	process, err := os.FindProcess(pid)
	if err == nil {
		_ = process.Kill()
	}
	t.Fatalf("child process %d survived cancellation", pid)
}
