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

package apps

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

func TestRunRejectsInvalidInputs(t *testing.T) {
	if err := Run(context.Background(), nil, nil); err == nil {
		t.Fatal("Run(nil config) error = nil")
	}
	if err := Run(context.Background(), &RunConfig{}, nil); err == nil {
		t.Fatal("Run(nil app) error = nil")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(canceled, &RunConfig{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run(canceled context) error = %v, want context.Canceled", err)
	}
}

func TestAPIConfigAddressesAndFlags(t *testing.T) {
	zeroValue := &ApiConfig{}
	if !zeroValue.IsSimpleAPIEnabled() || !zeroValue.IsWebUIEnabled() || zeroValue.DisableCORS {
		t.Fatal("ApiConfig zero value did not preserve historically enabled HTTP features")
	}

	config := DefaultApiConfig().
		SetHost("127.0.0.1").
		SetPort(8024).
		SetWriteTimeout(11).
		SetReadTimeout(12).
		SetIdleTimeout(13).
		SetSEEWriteTimeout(14).
		SetShutdownTimeout(15).
		SetApiPathPrefix("/api/").
		SetSimpleAPIEnabled(false).
		SetWebUIEnabled(false).
		SetMaxRequestBodyBytes(1024)

	if got, want := config.ListenAddress(), "127.0.0.1:8024"; got != want {
		t.Fatalf("ListenAddress() = %q, want %q", got, want)
	}
	if got, want := config.GetWebUrl(), "http://127.0.0.1:8024"; got != want {
		t.Fatalf("GetWebUrl() = %q, want %q", got, want)
	}
	if got, want := config.GetAPIPath(), "http://127.0.0.1:8024/api"; got != want {
		t.Fatalf("GetAPIPath() = %q, want %q", got, want)
	}
	if config.IsSimpleAPIEnabled() || config.IsWebUIEnabled() {
		t.Fatal("simple API or WebUI remained enabled")
	}
	if got, want := config.MaxRequestBodyBytes, int64(1024); got != want {
		t.Fatalf("MaxRequestBodyBytes = %d, want %d", got, want)
	}
	if config.WriteTimeout != 11*time.Second || config.ReadTimeout != 12*time.Second ||
		config.IdleTimeout != 13*time.Second || config.SEEWriteTimeout != 14*time.Second ||
		config.ShutdownTimeout != 15*time.Second {
		t.Fatalf("configured timeouts were not preserved: %+v", config)
	}

	config.SetHost("0.0.0.0")
	if got, want := config.GetWebUrl(), "http://localhost:8024"; got != want {
		t.Fatalf("wildcard GetWebUrl() = %q, want %q", got, want)
	}
	config.SetCORS(nil)
	if !config.DisableCORS {
		t.Fatal("SetCORS(nil) did not disable CORS")
	}
}

func TestAPIConfigA2ARoutingAndAgentCardURL(t *testing.T) {
	config := DefaultApiConfig().
		SetHost("127.0.0.1").
		SetPort(8024).
		SetPublicURL("https://public.example/sandbox/").
		SetA2APath("internal/a2a/").
		SetA2APublicPath("public/a2a/")

	if got, want := config.GetWebUrl(), "http://127.0.0.1:8024"; got != want {
		t.Fatalf("GetWebUrl() = %q, want %q", got, want)
	}
	if got, want := config.GetA2APath(), "/internal/a2a"; got != want {
		t.Fatalf("GetA2APath() = %q, want %q", got, want)
	}
	if got, want := config.GetA2APublicURL(), "https://public.example/sandbox/public/a2a"; got != want {
		t.Fatalf("GetA2APublicURL() = %q, want %q", got, want)
	}

	request := httptest.NewRequest(http.MethodGet, "http://attacker.invalid/.well-known/agent-card.json", nil)
	request.Host = "attacker.invalid"
	request.Header.Set("Forwarded", `for=192.0.2.1;proto=http;host="attacker.invalid"`)
	request.Header.Set("X-Forwarded-Proto", "http")
	request.Header.Set("X-Forwarded-Host", "attacker.invalid")
	if got, want := config.ResolveAgentCardURL(request), "https://public.example/sandbox/public/a2a"; got != want {
		t.Fatalf("default ResolveAgentCardURL() = %q, want configured URL %q", got, want)
	}
	staticConfig := DefaultApiConfig().SetHost("127.0.0.1").SetPort(8024).SetA2APath("/a2a")
	if got, want := staticConfig.ResolveAgentCardURL(request), "http://127.0.0.1:8024/a2a"; got != want {
		t.Fatalf("default resolver without PublicURL = %q, want listener-derived URL %q", got, want)
	}

	config.SetAgentCardURLResolver(ForwardedAgentCardURL)
	request.Header.Set("Forwarded", `for=192.0.2.1;proto=https;host="proxy.example:8443"`)
	if got, want := config.ResolveAgentCardURL(request), "https://proxy.example:8443/public/a2a"; got != want {
		t.Fatalf("forwarded ResolveAgentCardURL() = %q, want %q", got, want)
	}

	request.Header.Set("Forwarded", "")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "legacy-proxy.example")
	if got, want := config.ResolveAgentCardURL(request), "https://legacy-proxy.example/public/a2a"; got != want {
		t.Fatalf("X-Forwarded ResolveAgentCardURL() = %q, want %q", got, want)
	}

	request.Host = "bad host"
	request.Header.Set("X-Forwarded-Host", "user@example.invalid")
	if got, want := config.ResolveAgentCardURL(request), config.GetA2APublicURL(); got != want {
		t.Fatalf("invalid forwarded host resolved to %q, want fallback %q", got, want)
	}

	config.SetA2APublicPath("")
	if got, want := config.GetA2APublicURL(), "https://public.example/sandbox/internal/a2a"; got != want {
		t.Fatalf("inherited A2A public URL = %q, want %q", got, want)
	}
}

type lifecycleTestApp struct {
	config *ApiConfig
	setup  func(*mux.Router) error
}

func (a *lifecycleTestApp) Run(ctx context.Context, config *RunConfig) error {
	return Run(ctx, config, a)
}

func (a *lifecycleTestApp) SetupRouters(router *mux.Router, _ *RunConfig) error {
	if a.setup != nil {
		return a.setup(router)
	}
	return nil
}

func (a *lifecycleTestApp) GetApiConfig() *ApiConfig { return a.config }
func (a *lifecycleTestApp) GetServerName() string    { return "lifecycle-test" }

func TestRunReportsPortConflict(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	setupCalled := false
	app := &lifecycleTestApp{
		config: DefaultApiConfig().SetHost("127.0.0.1").SetPort(port),
		setup: func(*mux.Router) error {
			setupCalled = true
			return nil
		},
	}
	err = app.Run(context.Background(), &RunConfig{DisableObservability: true})
	if err == nil || !strings.Contains(err.Error(), "listen failed") {
		t.Fatalf("Run() error = %v, want listen failure", err)
	}
	if setupCalled {
		t.Fatal("router setup ran before detecting the port conflict")
	}
}

func TestRunGracefulShutdownWaitsForActiveRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	app := newNetworkTestApp(t, func(router *mux.Router) error {
		router.HandleFunc("/slow", func(writer http.ResponseWriter, _ *http.Request) {
			startedOnce.Do(func() { close(started) })
			<-release
			writer.WriteHeader(http.StatusNoContent)
		})
		return nil
	})
	app.config.ShutdownTimeout = time.Second

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- app.Run(ctx, &RunConfig{DisableObservability: true}) }()
	waitForTCP(t, app.config.ListenAddress())

	requestErr := make(chan error, 1)
	go func() {
		response, err := (&http.Client{Timeout: 2 * time.Second}).Get(app.config.GetWebUrl() + "/slow")
		if err == nil {
			_ = response.Body.Close()
		}
		requestErr <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("slow request did not start")
	}

	cancel()
	select {
	case err := <-runErr:
		t.Fatalf("Run() returned before active request completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-runErr; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := <-requestErr; err != nil {
		t.Fatalf("slow request error = %v", err)
	}
}

func TestRunForcesCloseAfterShutdownTimeout(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	var startedOnce sync.Once
	var stoppedOnce sync.Once
	app := newNetworkTestApp(t, func(router *mux.Router) error {
		router.HandleFunc("/block", func(_ http.ResponseWriter, request *http.Request) {
			startedOnce.Do(func() { close(started) })
			<-request.Context().Done()
			stoppedOnce.Do(func() { close(stopped) })
		})
		return nil
	})
	app.config.ShutdownTimeout = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- app.Run(ctx, &RunConfig{DisableObservability: true}) }()
	waitForTCP(t, app.config.ListenAddress())
	go func() {
		response, err := (&http.Client{Timeout: 2 * time.Second}).Get(app.config.GetWebUrl() + "/block")
		if err == nil {
			_ = response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("blocking request did not start")
	}

	cancel()
	select {
	case err := <-runErr:
		if err == nil || !strings.Contains(err.Error(), "shutdown lifecycle-test failed") {
			t.Fatalf("Run() error = %v, want shutdown timeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after forced close")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("forced close did not cancel the active request")
	}
}

func TestServerHandlerCORSBodyLimitAndRecovery(t *testing.T) {
	router := mux.NewRouter()
	router.HandleFunc("/echo", func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			http.Error(writer, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		_, _ = writer.Write(body)
	}).Methods(http.MethodPost)
	router.HandleFunc("/panic", func(http.ResponseWriter, *http.Request) {
		panic("sensitive-panic-value")
	}).Methods(http.MethodGet)

	config := DefaultApiConfig().SetMaxRequestBodyBytes(4).SetCORS(&CORSConfig{
		AllowedOrigins:   []string{"https://allowed.example"},
		AllowedMethods:   []string{http.MethodPost},
		AllowedHeaders:   []string{"X-Request-ID"},
		ExposedHeaders:   []string{"X-Response-ID"},
		AllowCredentials: true,
		MaxAge:           time.Minute,
	})
	handler := buildServerHandler(router, config, "test-server")

	preflight := httptest.NewRequest(http.MethodOptions, "/echo", nil)
	preflight.Header.Set("Origin", "https://allowed.example")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflight.Header.Set("Access-Control-Request-Headers", "x-request-id")
	preflightResponse := httptest.NewRecorder()
	handler.ServeHTTP(preflightResponse, preflight)
	if got, want := preflightResponse.Code, http.StatusNoContent; got != want {
		t.Fatalf("preflight status = %d, want %d: %s", got, want, preflightResponse.Body.String())
	}
	if got, want := preflightResponse.Header().Get("Access-Control-Allow-Origin"), "https://allowed.example"; got != want {
		t.Fatalf("allow origin = %q, want %q", got, want)
	}
	if got, want := preflightResponse.Header().Get("Access-Control-Allow-Credentials"), "true"; got != want {
		t.Fatalf("allow credentials = %q, want %q", got, want)
	}

	disallowed := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader("ok"))
	disallowed.Header.Set("Origin", "https://denied.example")
	disallowedResponse := httptest.NewRecorder()
	handler.ServeHTTP(disallowedResponse, disallowed)
	if got, want := disallowedResponse.Code, http.StatusForbidden; got != want {
		t.Fatalf("disallowed origin status = %d, want %d", got, want)
	}

	large := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader("12345"))
	largeResponse := httptest.NewRecorder()
	handler.ServeHTTP(largeResponse, large)
	if got, want := largeResponse.Code, http.StatusRequestEntityTooLarge; got != want {
		t.Fatalf("large body status = %d, want %d", got, want)
	}

	chunked := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader("12345"))
	chunked.ContentLength = -1
	chunked.TransferEncoding = []string{"chunked"}
	chunkedResponse := httptest.NewRecorder()
	handler.ServeHTTP(chunkedResponse, chunked)
	if got, want := chunkedResponse.Code, http.StatusRequestEntityTooLarge; got != want {
		t.Fatalf("chunked large body status = %d, want %d", got, want)
	}

	panicRequest := httptest.NewRequest(http.MethodGet, "/panic", nil)
	panicResponse := httptest.NewRecorder()
	handler.ServeHTTP(panicResponse, panicRequest)
	if got, want := panicResponse.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("panic status = %d, want %d", got, want)
	}
	if strings.Contains(panicResponse.Body.String(), "sensitive-panic-value") {
		t.Fatalf("panic response leaked the recovered value: %q", panicResponse.Body.String())
	}
}

func TestRunRedactsSetupAndConfigurationErrors(t *testing.T) {
	sensitive := errors.New("credential secret must not be exposed")
	app := &lifecycleTestApp{
		config: DefaultApiConfig().SetHost("127.0.0.1").SetPort(reservePort(t)),
		setup:  func(*mux.Router) error { return sensitive },
	}
	err := app.Run(context.Background(), &RunConfig{DisableObservability: true})
	if err == nil {
		t.Fatal("Run() error = nil")
	}
	if strings.Contains(err.Error(), sensitive.Error()) {
		t.Fatalf("Run() leaked setup error: %v", err)
	}
	if !errors.Is(err, sensitive) {
		t.Fatalf("errors.Is(Run(), sensitive) = false: %v", err)
	}

	invalid := &lifecycleTestApp{config: DefaultApiConfig().SetCORS(&CORSConfig{
		AllowedOrigins:   []string{"*"},
		AllowCredentials: true,
	})}
	err = invalid.Run(context.Background(), &RunConfig{DisableObservability: true})
	if err == nil || err.Error() != "validate server configuration failed" {
		t.Fatalf("invalid CORS error = %v", err)
	}

	invalidPublicURL := &lifecycleTestApp{config: DefaultApiConfig().SetPublicURL("https://user:secret@example.test/a2a")}
	err = invalidPublicURL.Run(context.Background(), &RunConfig{DisableObservability: true})
	if err == nil || err.Error() != "validate server configuration failed" {
		t.Fatalf("invalid public URL error = %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("invalid public URL error leaked user info: %v", err)
	}
}

func TestRunHandlesRepeatedSIGTERMInRealProcess(t *testing.T) {
	port := reservePort(t)
	command := exec.Command(os.Args[0], "-test.run=^TestServerProcessHelper$")
	command.Env = []string{
		"VEADK_SERVER_PROCESS_HELPER=1",
		"VEADK_SERVER_PROCESS_PORT=" + strconv.Itoa(port),
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
		}
	}()

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	waitForTCP(t, address)
	response, err := (&http.Client{Timeout: time.Second}).Get("http://" + address + "/block")
	if err != nil {
		t.Fatalf("start blocking request: %v; process output: %s", err, output.String())
	}
	_ = response.Body.Close()

	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("first SIGTERM: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("second SIGTERM: %v", err)
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- command.Wait() }()
	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("server process error = %v; output: %s", err, output.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("server process did not stop after SIGTERM; output: %s", output.String())
	}
}

func TestServerProcessHelper(t *testing.T) {
	if os.Getenv("VEADK_SERVER_PROCESS_HELPER") != "1" {
		return
	}
	port, err := strconv.Atoi(os.Getenv("VEADK_SERVER_PROCESS_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	app := &lifecycleTestApp{
		config: DefaultApiConfig().SetHost("127.0.0.1").SetPort(port),
		setup: func(router *mux.Router) error {
			router.HandleFunc("/block", func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusOK)
				if flusher, ok := writer.(http.Flusher); ok {
					flusher.Flush()
				}
				time.Sleep(150 * time.Millisecond)
			})
			return nil
		},
	}
	app.config.ShutdownTimeout = time.Second
	if err := app.Run(context.Background(), &RunConfig{DisableObservability: true}); err != nil {
		t.Fatal(err)
	}
}

func newNetworkTestApp(t *testing.T, setup func(*mux.Router) error) *lifecycleTestApp {
	t.Helper()
	return &lifecycleTestApp{
		config: DefaultApiConfig().SetHost("127.0.0.1").SetPort(reservePort(t)),
		setup:  setup,
	}
}

func reservePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func waitForTCP(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		connection, err := net.DialTimeout("tcp", address, 20*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not listen on %s: %v", address, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
