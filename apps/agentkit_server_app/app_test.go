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

package agentkit_server_app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/v2/apps"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
)

var _ apps.BasicApp = (*AgentkitServerApp)(nil)

func TestAgentkitServerRunHTTPBlackbox(t *testing.T) {
	rootAgent, err := agent.New(agent.Config{Name: "http_blackbox_agent"})
	if err != nil {
		t.Fatal(err)
	}
	port := reservePort(t)
	apiConfig := apps.DefaultApiConfig().
		SetHost("127.0.0.1").
		SetPort(port).
		SetApiPathPrefix("/api").
		SetSimpleAPIEnabled(false).
		SetWebUIEnabled(false).
		SetMaxRequestBodyBytes(8).
		SetCORS(&apps.CORSConfig{
			AllowedOrigins: []string{"https://sandbox.example"},
			AllowedMethods: []string{http.MethodGet, http.MethodPost},
			AllowedHeaders: []string{"X-Request-ID"},
		})
	app := NewAgentkitServerApp(
		apiConfig,
		WithMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("X-Extension-Middleware", "enabled")
				next.ServeHTTP(writer, request)
			})
		}),
		WithRouteSetup(func(router *mux.Router, _ *apps.RunConfig) error {
			router.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusNoContent)
			}).Methods(http.MethodGet)
			router.HandleFunc("/upload", func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusCreated)
			}).Methods(http.MethodPost)
			return nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() {
		runErr <- app.Run(ctx, &apps.RunConfig{
			AgentLoader:          agent.NewSingleLoader(rootAgent),
			SessionService:       session.InMemoryService(),
			DisableObservability: true,
		})
	}()
	waitForTCP(t, apiConfig.ListenAddress())

	request, err := http.NewRequest(http.MethodGet, apiConfig.GetWebUrl()+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "https://sandbox.example")
	response, err := (&http.Client{Timeout: time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if got, want := response.StatusCode, http.StatusNoContent; got != want {
		t.Fatalf("health status = %d, want %d", got, want)
	}
	if got, want := response.Header.Get("X-Extension-Middleware"), "enabled"; got != want {
		t.Fatalf("middleware header = %q, want %q", got, want)
	}
	if got, want := response.Header.Get("Access-Control-Allow-Origin"), "https://sandbox.example"; got != want {
		t.Fatalf("CORS origin = %q, want %q", got, want)
	}

	largeRequest, err := http.NewRequest(http.MethodPost, apiConfig.GetWebUrl()+"/upload", strings.NewReader("123456789"))
	if err != nil {
		t.Fatal(err)
	}
	largeResponse, err := (&http.Client{Timeout: time.Second}).Do(largeRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = largeResponse.Body.Close()
	if got, want := largeResponse.StatusCode, http.StatusRequestEntityTooLarge; got != want {
		t.Fatalf("large request status = %d, want %d", got, want)
	}

	cancel()
	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not stop after parent cancellation")
	}
}

func TestSetupRoutersSupportsExtensionsAndStableMiddlewareOrder(t *testing.T) {
	rootAgent, err := agent.New(agent.Config{
		Name:        "extension_agent",
		Description: "tests server extensions",
	})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var order []string
	record := func(value string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, value)
	}
	middleware := func(name string) mux.MiddlewareFunc {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				record(name + "-before")
				next.ServeHTTP(writer, request)
				record(name + "-after")
			})
		}
	}

	apiConfig := apps.DefaultApiConfig().
		SetHost("127.0.0.1").
		SetPort(8024).
		SetApiPathPrefix("/api").
		SetSimpleAPIEnabled(false).
		SetWebUIEnabled(false)
	app := NewAgentkitServerApp(
		apiConfig,
		WithMiddleware(middleware("first"), nil, middleware("second")),
		WithRouteSetup(nil),
		WithRouteSetup(func(router *mux.Router, _ *apps.RunConfig) error {
			router.HandleFunc("/api/custom", func(writer http.ResponseWriter, _ *http.Request) {
				record("handler")
				writer.WriteHeader(218)
			}).Methods(http.MethodGet)
			return nil
		}),
	)

	router := mux.NewRouter()
	err = app.SetupRouters(router, &apps.RunConfig{
		AgentLoader:          agent.NewSingleLoader(rootAgent),
		SessionService:       session.InMemoryService(),
		DisableObservability: true,
	})
	if err != nil {
		t.Fatalf("SetupRouters() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/custom", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if got, want := response.Code, 218; got != want {
		t.Fatalf("custom route status = %d, want %d: %s", got, want, response.Body.String())
	}
	wantOrder := []string{"first-before", "second-before", "handler", "second-after", "first-after"}
	if !slices.Equal(order, wantOrder) {
		t.Fatalf("middleware order = %v, want %v", order, wantOrder)
	}

	invokeRequest := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"prompt":"hello"}`))
	invokeResponse := httptest.NewRecorder()
	router.ServeHTTP(invokeResponse, invokeRequest)
	if got, want := invokeResponse.Code, http.StatusNotFound; got != want {
		t.Fatalf("disabled simple API status = %d, want %d", got, want)
	}

	webUIRequest := httptest.NewRequest(http.MethodGet, "/dev-ui/", nil)
	webUIResponse := httptest.NewRecorder()
	router.ServeHTTP(webUIResponse, webUIRequest)
	if got, want := webUIResponse.Code, http.StatusNotFound; got != want {
		t.Fatalf("disabled WebUI status = %d, want %d", got, want)
	}
}

func TestSetupRoutersKeepsBuiltInRoutesAheadOfExtensions(t *testing.T) {
	rootAgent, err := agent.New(agent.Config{Name: "priority_agent"})
	if err != nil {
		t.Fatal(err)
	}
	app := NewAgentkitServerApp(
		apps.DefaultApiConfig().SetSimpleAPIEnabled(false).SetWebUIEnabled(false),
		WithRouteSetup(func(router *mux.Router, _ *apps.RunConfig) error {
			router.HandleFunc("/", func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(299)
			})
			return nil
		}),
	)
	router := mux.NewRouter()
	if err := app.SetupRouters(router, &apps.RunConfig{
		AgentLoader:          agent.NewSingleLoader(rootAgent),
		SessionService:       session.InMemoryService(),
		DisableObservability: true,
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"jsonrpc":"2.0","id":"1","method":"unknown"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code == 299 {
		t.Fatal("custom route overrode the built-in A2A route")
	}
}

func TestSetupRoutersRedactsExtensionFailure(t *testing.T) {
	rootAgent, err := agent.New(agent.Config{Name: "failure_agent"})
	if err != nil {
		t.Fatal(err)
	}
	sensitive := errors.New("private route configuration token")
	app := NewAgentkitServerApp(
		apps.DefaultApiConfig().SetSimpleAPIEnabled(false).SetWebUIEnabled(false),
		WithRouteSetup(func(*mux.Router, *apps.RunConfig) error { return sensitive }),
	)
	err = app.SetupRouters(mux.NewRouter(), &apps.RunConfig{
		AgentLoader:          agent.NewSingleLoader(rootAgent),
		SessionService:       session.InMemoryService(),
		DisableObservability: true,
	})
	if err == nil {
		t.Fatal("SetupRouters() error = nil")
	}
	if strings.Contains(err.Error(), sensitive.Error()) {
		t.Fatalf("SetupRouters() leaked extension error: %v", err)
	}
	if !errors.Is(err, sensitive) {
		t.Fatalf("errors.Is(SetupRouters(), sensitive) = false: %v", err)
	}
}

func TestSetupRoutersRejectsMissingDependencies(t *testing.T) {
	app := NewAgentkitServerApp(nil)
	if err := app.SetupRouters(nil, &apps.RunConfig{}); err == nil {
		t.Fatal("SetupRouters(nil router) error = nil")
	}
	if err := app.SetupRouters(mux.NewRouter(), nil); err == nil {
		t.Fatal("SetupRouters(nil config) error = nil")
	}
	if err := app.SetupRouters(mux.NewRouter(), &apps.RunConfig{}); err == nil {
		t.Fatal("SetupRouters(missing agent) error = nil")
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
