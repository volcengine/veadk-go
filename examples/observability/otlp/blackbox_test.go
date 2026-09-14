package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

// Each case starts a real binary in a private, empty working directory with
// synthetic environment only. No model service or credentials are needed.
func TestOTLPServerBlackbox(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM contract")
	}
	bin := filepath.Join(t.TempDir(), "otlp-server")
	build := exec.Command("go", "build", "-o", bin, ".")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)
	for _, mode := range []string{"batch", "shutdown", "disabled", "none", "unconfigured", "unavailable", "slow", "bad-config"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			var requests []*collector.ExportTraceServiceRequest
			var paths, headers []string
			collectorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, err := io.ReadAll(r.Body)
				if err != nil {
					return
				}
				if mode == "slow" {
					<-r.Context().Done()
					return
				}
				req := new(collector.ExportTraceServiceRequest)
				if err := proto.Unmarshal(b, req); err != nil {
					http.Error(w, "invalid protobuf", 400)
					return
				}
				mu.Lock()
				requests = append(requests, req)
				paths = append(paths, r.URL.Path)
				headers = append(headers, r.Header.Get("Authorization"))
				mu.Unlock()
				w.Header().Set("Content-Type", "application/x-protobuf")
			}))
			defer collectorServer.Close()
			endpoint := collectorServer.URL
			if mode == "unavailable" {
				collectorServer.Close()
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			port := listener.Addr().(*net.TCPAddr).Port
			require.NoError(t, listener.Close())
			cmd := exec.Command(bin, "-port", strconv.Itoa(port))
			cmd.Dir = t.TempDir()
			cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + cmd.Dir, "OTEL_SERVICE_NAME=blackbox-agent", "OTEL_RESOURCE_ATTRIBUTES=deployment.environment.name=blackbox", "OTEL_BSP_SCHEDULE_DELAY=60000", "OTEL_EXPORTER_OTLP_TRACES_HEADERS=Authorization=synthetic-blackbox", "OTEL_EXPORTER_OTLP_TRACES_TIMEOUT=10000"}
			if mode != "unconfigured" {
				cmd.Env = append(cmd.Env, "OTEL_TRACES_EXPORTER=otlp", "OTEL_EXPORTER_OTLP_ENDPOINT="+endpoint)
			}
			switch mode {
			case "batch":
				cmd.Env = append(cmd.Env, "OTEL_BSP_SCHEDULE_DELAY=50")
			case "disabled":
				cmd.Env = append(cmd.Env, "OTEL_SDK_DISABLED=true")
			case "none":
				cmd.Env = append(cmd.Env, "OTEL_TRACES_EXPORTER=none")
			case "bad-config":
				cmd.Env = append(cmd.Env, "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=invalid-synthetic-secret")
			}
			var logs bytes.Buffer
			cmd.Stdout, cmd.Stderr = &logs, &logs
			start := time.Now()
			require.NoError(t, cmd.Start())
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			stopped := false
			defer func() {
				if !stopped {
					_ = cmd.Process.Kill()
					<-done
				}
			}()
			client := &http.Client{Timeout: 2 * time.Second}
			address := fmt.Sprintf("http://127.0.0.1:%d/invoke", port)
			// The first successful real invocation is our ready and execution probe.
			ready := false
			for time.Since(start) < 5*time.Second {
				resp, err := client.Post(address, "application/json", strings.NewReader(`{"prompt":"synthetic request"}`))
				if err == nil {
					body, readErr := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					require.NoError(t, readErr)
					require.Equal(t, 200, resp.StatusCode, "%s", body)
					var result struct {
						Data string `json:"data"`
					}
					require.NoError(t, json.Unmarshal(body, &result))
					require.Equal(t, "otlp-agent-ok", result.Data)
					ready = true
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			require.True(t, ready, "server did not become ready")
			require.Less(t, time.Since(start), 5*time.Second)
			if mode == "batch" {
				require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(requests) > 0 }, 3*time.Second, 20*time.Millisecond)
			} else {
				mu.Lock()
				n := len(requests)
				mu.Unlock()
				require.Zero(t, n, "export must remain queued until shutdown")
			}
			stopStart := time.Now()
			require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
			select {
			case err := <-done:
				stopped = true
				require.NoError(t, err, "%s", logs.String())
			case <-time.After(7 * time.Second):
				t.Fatal("SIGTERM exceeded bounded shutdown")
			}
			require.Less(t, time.Since(stopStart), 7*time.Second)
			require.NotContains(t, logs.String(), "invalid-synthetic-secret")
			require.NotContains(t, logs.String(), "synthetic-blackbox")
			mu.Lock()
			defer mu.Unlock()
			if mode == "batch" || mode == "shutdown" {
				require.NotEmpty(t, requests)
				foundModel := false
				foundService := false
				foundResource := false
				for i, req := range requests {
					require.Equal(t, "/v1/traces", paths[i])
					require.Equal(t, "synthetic-blackbox", headers[i])
					for _, rs := range req.ResourceSpans {
						for _, a := range rs.Resource.Attributes {
							if a.Key == "service.name" && a.Value.GetStringValue() == "blackbox-agent" {
								foundService = true
							}
							if a.Key == "deployment.environment.name" && a.Value.GetStringValue() == "blackbox" {
								foundResource = true
							}
						}
						for _, ss := range rs.ScopeSpans {
							for _, s := range ss.Spans {
								if strings.Contains(s.Name, "llm") || strings.Contains(s.Name, "model") {
									foundModel = true
								}
							}
						}
					}
				}
				require.True(t, foundModel, "actual Agent/model span must reach collector")
				require.True(t, foundService)
				require.True(t, foundResource)
			} else {
				require.Empty(t, requests)
			}
			t.Logf("ready+invoke %s; SIGTERM %s", time.Since(start)-time.Since(stopStart), time.Since(stopStart))
		})
	}
}
