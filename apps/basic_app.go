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
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/v2/log"
	"github.com/volcengine/veadk-go/v2/observability"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/cmd/launcher/web"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/telemetry"
)

type RunConfig struct {
	SessionService   session.Service
	ArtifactService  artifact.Service
	MemoryService    memory.Service
	AgentLoader      agent.Loader
	A2AOptions       []a2asrv.RequestHandlerOption
	PluginConfig     runner.PluginConfig
	TelemetryOptions []telemetry.Option
	// DisableObservability skips VeADK's default observability plugin and
	// shutdown hook. Embedders that manage telemetry themselves can use this to
	// avoid installing or shutting down global providers.
	DisableObservability bool
}

func (cfg *RunConfig) AppendObservability() {
	if len(cfg.PluginConfig.Plugins) == 0 {
		cfg.PluginConfig = runner.PluginConfig{
			Plugins: []*plugin.Plugin{observability.NewPlugin()},
		}
	} else {
		observabilityPlugin := observability.NewPlugin()
		for _, p := range cfg.PluginConfig.Plugins {
			if p.Name() == observabilityPlugin.Name() {
				log.Info("Plugin already configured")
				return
			}
		}
		cfg.PluginConfig.Plugins = append(cfg.PluginConfig.Plugins, observabilityPlugin)
		log.Info("Plugin configured")
	}

	cfg.TelemetryOptions = append(cfg.TelemetryOptions, observability.ADKTelemetryOptions()...)
}

type ApiConfig struct {
	Host string
	Port int
	// PublicURL is the externally reachable base URL advertised in Agent Cards.
	// It is never inferred from request headers unless AgentCardURLResolver is set.
	PublicURL       string
	WriteTimeout    time.Duration
	ReadTimeout     time.Duration
	IdleTimeout     time.Duration
	SEEWriteTimeout time.Duration
	ShutdownTimeout time.Duration
	ApiPathPrefix   string
	// A2APath is the route mounted on this HTTP server. A2APublicPath is the
	// externally visible route advertised in Agent Cards and may differ when a
	// reverse proxy rewrites paths.
	A2APath              string
	A2APublicPath        string
	AgentCardURLResolver AgentCardURLResolver
	DisableSimpleAPI     bool
	DisableWebUI         bool
	MaxRequestBodyBytes  int64
	DisableCORS          bool
	CORS                 CORSConfig
}

// CORSConfig controls cross-origin requests for every route served by an app.
// An empty AllowedOrigins list allows only the app's own public address.
type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           time.Duration
}

// SetupError reports a router or configuration setup failure without putting
// the underlying error text in logs or user-facing process output. Callers can
// still inspect the cause with errors.Is/errors.As.
type SetupError struct {
	Operation string
	err       error
}

func (e *SetupError) Error() string {
	if e == nil || strings.TrimSpace(e.Operation) == "" {
		return "server setup failed"
	}
	return e.Operation + " failed"
}

func (e *SetupError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// NewSetupError creates a setup error whose public text does not include the
// potentially sensitive cause.
func NewSetupError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return &SetupError{Operation: operation, err: err}
}

type BasicApp interface {
	Run(ctx context.Context, config *RunConfig) error
	SetupRouters(router *mux.Router, config *RunConfig) error
	GetApiConfig() *ApiConfig
	GetServerName() string
}

func DefaultApiConfig() *ApiConfig {
	return &ApiConfig{
		Host:            "",
		Port:            8000,
		WriteTimeout:    time.Second * 60,
		ReadTimeout:     time.Second * 60,
		IdleTimeout:     time.Second * 120,
		SEEWriteTimeout: time.Second * 300,
		ShutdownTimeout: time.Second * 30,
		ApiPathPrefix:   "", // set /api same as ADK-Go
		CORS: CORSConfig{
			AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
			AllowedHeaders: []string{"Content-Type", "Authorization"},
		},
	}
}

func (a *ApiConfig) SetHost(host string) *ApiConfig {
	a.Host = host
	return a
}

func (a *ApiConfig) SetPort(port int) *ApiConfig {
	a.Port = port
	return a
}

func (a *ApiConfig) SetWriteTimeout(t int64) *ApiConfig {
	a.WriteTimeout = time.Second * time.Duration(t)
	return a
}

func (a *ApiConfig) SetReadTimeout(t int64) *ApiConfig {
	a.ReadTimeout = time.Second * time.Duration(t)
	return a
}

func (a *ApiConfig) SetIdleTimeout(t int64) *ApiConfig {
	a.IdleTimeout = time.Second * time.Duration(t)
	return a
}

// SetSEEWriteTimeout is retained for compatibility. Use SetSSEWriteTimeout.
func (a *ApiConfig) SetSEEWriteTimeout(t int64) *ApiConfig {
	return a.SetSSEWriteTimeout(t)
}

// SetSSEWriteTimeout configures the ADK REST streaming response timeout.
func (a *ApiConfig) SetSSEWriteTimeout(t int64) *ApiConfig {
	a.SEEWriteTimeout = time.Second * time.Duration(t)
	return a
}

func (a *ApiConfig) SetShutdownTimeout(t int64) *ApiConfig {
	a.ShutdownTimeout = time.Second * time.Duration(t)
	return a
}

func (a *ApiConfig) SetApiPathPrefix(p string) *ApiConfig {
	a.ApiPathPrefix = normalizePathPrefix(p)
	return a
}

func (a *ApiConfig) SetSimpleAPIEnabled(enabled bool) *ApiConfig {
	a.DisableSimpleAPI = !enabled
	return a
}

func (a *ApiConfig) SetWebUIEnabled(enabled bool) *ApiConfig {
	a.DisableWebUI = !enabled
	return a
}

// IsSimpleAPIEnabled reports whether the built-in /invoke and /health routes
// are enabled. The zero value preserves the historical enabled behavior.
func (a *ApiConfig) IsSimpleAPIEnabled() bool {
	return !a.DisableSimpleAPI
}

// IsWebUIEnabled reports whether ADK WebUI routes are enabled. The zero value
// preserves the historical enabled behavior.
func (a *ApiConfig) IsWebUIEnabled() bool {
	return !a.DisableWebUI
}

func (a *ApiConfig) SetMaxRequestBodyBytes(limit int64) *ApiConfig {
	a.MaxRequestBodyBytes = limit
	return a
}

func (a *ApiConfig) SetCORS(config *CORSConfig) *ApiConfig {
	if config == nil {
		a.DisableCORS = true
		a.CORS = CORSConfig{}
		return a
	}
	a.DisableCORS = false
	a.CORS = *config
	return a
}

func (a *ApiConfig) GetWebUrl() string {
	host := a.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(a.Port))
}

func (a *ApiConfig) GetAPIPath() string {
	return a.GetWebUrl() + normalizePathPrefix(a.ApiPathPrefix)
}

func (a *ApiConfig) ListenAddress() string {
	return net.JoinHostPort(a.Host, strconv.Itoa(a.Port))
}

func normalizePathPrefix(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return ""
	}
	return "/" + strings.Trim(path, "/")
}

func (a *ApiConfig) validate() error {
	if a.Port < 0 || a.Port > 65535 {
		return errors.New("port is outside the valid range")
	}
	if a.WriteTimeout < 0 || a.ReadTimeout < 0 || a.IdleTimeout < 0 || a.SEEWriteTimeout < 0 || a.ShutdownTimeout < 0 {
		return errors.New("HTTP timeout must not be negative")
	}
	if a.MaxRequestBodyBytes < 0 {
		return errors.New("request body limit must not be negative")
	}
	if a.PublicURL != "" && !validPublicURL(a.PublicURL) {
		return errors.New("public URL must be an absolute HTTP or HTTPS URL without user info, query, or fragment")
	}
	if a.A2APath != "" && !strings.HasPrefix(a.A2APath, "/") {
		return errors.New("A2A path must be absolute")
	}
	if a.A2APublicPath != "" && !strings.HasPrefix(a.A2APublicPath, "/") {
		return errors.New("A2A public path must be absolute")
	}
	if !a.DisableCORS && a.CORS.AllowCredentials && slices.Contains(a.CORS.AllowedOrigins, "*") {
		return errors.New("credentialed CORS cannot allow every origin")
	}
	if !a.DisableCORS && a.CORS.MaxAge < 0 {
		return errors.New("CORS max age must not be negative")
	}
	return nil
}

func Run(ctx context.Context, config *RunConfig, app BasicApp) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if config == nil {
		return errors.New("run config is required")
	}
	if app == nil || app.GetApiConfig() == nil {
		return errors.New("app with API config is required")
	}
	apiConfig := app.GetApiConfig()
	if err := apiConfig.validate(); err != nil {
		return NewSetupError("validate server configuration", err)
	}
	runCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", apiConfig.ListenAddress())
	if err != nil {
		return fmt.Errorf("%s listen failed: %w", app.GetServerName(), err)
	}
	defer listener.Close()

	router := web.BuildBaseRouter()

	if config.SessionService == nil {
		config.SessionService = session.InMemoryService()
	}

	if !config.DisableObservability {
		config.AppendObservability()
		defer shutdownObservability()
	}

	log.Infof("Web servers starts on %s", app.GetApiConfig().GetWebUrl())
	err = app.SetupRouters(router, config)
	if err != nil {
		return NewSetupError("setup "+app.GetServerName()+" routers", err)
	}

	handler := buildServerHandler(router, apiConfig, app.GetServerName())
	srv := &http.Server{
		Addr:         apiConfig.ListenAddress(),
		WriteTimeout: apiConfig.WriteTimeout,
		ReadTimeout:  apiConfig.ReadTimeout,
		IdleTimeout:  apiConfig.IdleTimeout,
		Handler:      handler,
		BaseContext: func(net.Listener) context.Context {
			// Preserve parent values without canceling active requests before the
			// configured graceful-shutdown window has elapsed.
			return context.WithoutCancel(ctx)
		},
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Serve(listener)
	}()

	select {
	case err = <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("%s failed: %w", app.GetServerName(), err)
		}
	case <-runCtx.Done():
		log.Infof("Received shutdown request, gracefully stopping %s...", app.GetServerName())
		if err := shutdownServer(srv, serverErr, apiConfig.ShutdownTimeout); err != nil {
			return fmt.Errorf("shutdown %s failed: %w", app.GetServerName(), err)
		}
	}

	log.Infof("%s stopped gracefully", app.GetServerName())
	return nil
}

func shutdownObservability() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := observability.Shutdown(ctx); err != nil {
		log.Errorf("shutting down observability failed")
		return
	}
	log.Info("observability stopped")
}

func shutdownServer(server *http.Server, serverErr <-chan error, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		closeErr := server.Close()
		serveErr := waitForServer(serverErr, 2*time.Second)
		return errors.Join(shutdownErr, closeErr, serveErr)
	}
	return waitForServer(serverErr, 2*time.Second)
}

func waitForServer(serverErr <-chan error, timeout time.Duration) error {
	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-time.After(timeout):
		return errors.New("server did not stop after close")
	}
}

func buildServerHandler(next http.Handler, config *ApiConfig, serverName string) http.Handler {
	handler := next
	if config.MaxRequestBodyBytes > 0 {
		handler = requestBodyLimitMiddleware(config.MaxRequestBodyBytes, handler)
	}
	if !config.DisableCORS {
		handler = corsMiddleware(config, handler)
	}
	return recoveryMiddleware(serverName, handler)
}

func recoveryMiddleware(serverName string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recover() != nil {
				log.Errorf("%s recovered a panic while serving a request", serverName)
				http.Error(writer, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(writer, request)
	})
}

func requestBodyLimitMiddleware(limit int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.ContentLength > limit {
			http.Error(writer, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, limit)
		next.ServeHTTP(writer, request)
	})
}

func corsMiddleware(apiConfig *ApiConfig, next http.Handler) http.Handler {
	config := &apiConfig.CORS
	allowedOrigins := slices.Clone(config.AllowedOrigins)
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{apiConfig.GetWebUrl()}
	}
	allowedMethods := slices.Clone(config.AllowedMethods)
	if len(allowedMethods) == 0 {
		allowedMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions}
	}
	allowedHeaders := slices.Clone(config.AllowedHeaders)
	if len(allowedHeaders) == 0 {
		allowedHeaders = []string{"Content-Type", "Authorization"}
	}
	exposedHeaders := slices.Clone(config.ExposedHeaders)
	allowCredentials := config.AllowCredentials
	maxAge := config.MaxAge

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(writer, request)
			return
		}
		writer.Header().Add("Vary", "Origin")
		if !containsFold(allowedOrigins, origin) && !slices.Contains(allowedOrigins, "*") {
			http.Error(writer, "origin not allowed", http.StatusForbidden)
			return
		}
		allowOrigin := origin
		if slices.Contains(allowedOrigins, "*") {
			allowOrigin = "*"
		}
		writer.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		writer.Header().Set("Access-Control-Allow-Methods", strings.Join(allowedMethods, ", "))
		writer.Header().Set("Access-Control-Allow-Headers", strings.Join(allowedHeaders, ", "))
		if len(exposedHeaders) > 0 {
			writer.Header().Set("Access-Control-Expose-Headers", strings.Join(exposedHeaders, ", "))
		}
		if allowCredentials {
			writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if maxAge > 0 {
			writer.Header().Set("Access-Control-Max-Age", strconv.FormatInt(int64(maxAge/time.Second), 10))
		}
		if request.Method == http.MethodOptions && request.Header.Get("Access-Control-Request-Method") != "" {
			writer.Header().Add("Vary", "Access-Control-Request-Method")
			writer.Header().Add("Vary", "Access-Control-Request-Headers")
			if !containsFold(allowedMethods, request.Header.Get("Access-Control-Request-Method")) {
				http.Error(writer, "method not allowed", http.StatusForbidden)
				return
			}
			for _, requested := range strings.Split(request.Header.Get("Access-Control-Request-Headers"), ",") {
				requested = strings.TrimSpace(requested)
				if requested != "" && !containsFold(allowedHeaders, requested) {
					http.Error(writer, "header not allowed", http.StatusForbidden)
					return
				}
			}
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
