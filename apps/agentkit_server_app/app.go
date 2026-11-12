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
	"fmt"
	"net/http"
	"reflect"
	"slices"

	"github.com/gorilla/mux"
	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"github.com/volcengine/veadk-go/v2/apps"
	"github.com/volcengine/veadk-go/v2/apps/a2a_app"
	"github.com/volcengine/veadk-go/v2/apps/simple_app"
	"github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"github.com/volcengine/veadk-go/v2/log"
	"github.com/volcengine/veadk-go/v2/observability"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/web/webui"
	"google.golang.org/adk/v2/server/adkrest"
)

const serverName = "agentkit server"

// AgentkitServerApp combines the simple API, A2A and ADK REST server while
// allowing embedders to add routes and middleware around the shared router.
type AgentkitServerApp struct {
	*apps.ApiConfig
	routeSetups  []RouteSetup
	middleware   []mux.MiddlewareFunc
	aioLifecycle *agentkit.AIOLifecycle
}

// RouteSetup adds application-specific routes after VeADK's exact built-in
// routes and before the ADK REST/WebUI fallback routes.
type RouteSetup func(router *mux.Router, config *apps.RunConfig) error

// Option configures AgentkitServerApp extensions.
type Option func(*AgentkitServerApp)

// WithAIOLifecycle enables session-bound AIO sandbox management for this App.
// Local-mode apps leave it unset and use the session extensions without a sandbox.
func WithAIOLifecycle(manager *agentkit.AIOLifecycle) Option {
	return func(app *AgentkitServerApp) { app.aioLifecycle = manager }
}

// WithRouteSetup adds one non-nil route setup callback.
func WithRouteSetup(setup RouteSetup) Option {
	return func(app *AgentkitServerApp) {
		if setup != nil {
			app.routeSetups = append(app.routeSetups, setup)
		}
	}
}

// WithMiddleware installs middleware in declaration order. Nil middleware is
// ignored.
func WithMiddleware(middleware ...mux.MiddlewareFunc) Option {
	return func(app *AgentkitServerApp) {
		for _, item := range middleware {
			if item != nil {
				app.middleware = append(app.middleware, item)
			}
		}
	}
}

func NewAgentkitServerApp(config *apps.ApiConfig, options ...Option) *AgentkitServerApp {
	if config == nil {
		config = apps.DefaultApiConfig()
	}
	app := &AgentkitServerApp{
		ApiConfig: config,
	}
	for _, option := range options {
		if option != nil {
			option(app)
		}
	}
	return app
}

func (a *AgentkitServerApp) Run(ctx context.Context, config *apps.RunConfig) error {
	workerCtx, stop := context.WithCancel(ctx)
	defer stop()
	return apps.Run(ctx, config, &runningServerApp{AgentkitServerApp: a, workerCtx: workerCtx})
}

// Start background mutations only after router/configuration validation succeeds.
type runningServerApp struct {
	*AgentkitServerApp
	workerCtx context.Context
}

func (a *runningServerApp) SetupRouters(router *mux.Router, config *apps.RunConfig) error {
	if err := a.AgentkitServerApp.SetupRouters(router, config); err != nil {
		return err
	}
	if a.aioLifecycle != nil {
		a.aioLifecycle.Start(a.workerCtx)
	}
	return nil
}

func (a *AgentkitServerApp) SetupRouters(router *mux.Router, config *apps.RunConfig) error {
	if router == nil {
		return apps.NewSetupError("validate router", fmt.Errorf("router is required"))
	}
	if config == nil || config.AgentLoader == nil || config.AgentLoader.RootAgent() == nil {
		return apps.NewSetupError("validate agent loader", fmt.Errorf("root agent is required"))
	}
	if a.aioLifecycle != nil && a.IsSimpleAPIEnabled() {
		return apps.NewSetupError("validate Simple API", fmt.Errorf("aio_sandbox cannot enable Simple API"))
	}
	if mode, ids, known := veagent.SkillsModeFor(config.AgentLoader.RootAgent()); known {
		if mode == "aio_sandbox" && a.IsSimpleAPIEnabled() {
			return apps.NewSetupError("validate Simple API", fmt.Errorf("aio_sandbox cannot enable Simple API"))
		}
		if mode == "aio_sandbox" && a.aioLifecycle == nil {
			return apps.NewSetupError("validate AIO lifecycle", fmt.Errorf("aio_sandbox Agent requires WithAIOLifecycle"))
		}
		if a.aioLifecycle != nil && (mode != "aio_sandbox" || !slices.Equal(ids, a.aioLifecycle.SkillSpaceIDs())) {
			return apps.NewSetupError("validate AIO lifecycle", fmt.Errorf("Agent Skill mode or Skill Space IDs do not match lifecycle configuration"))
		}
	}
	if a.aioLifecycle != nil && !sameSessionService(a.aioLifecycle.SessionService(), config.SessionService) {
		return apps.NewSetupError("validate AIO lifecycle", fmt.Errorf("App and AIO lifecycle must share SessionService"))
	}
	if a.aioLifecycle != nil {
		for _, name := range config.AgentLoader.ListAgents() {
			a.aioLifecycle.RegisterApp(name)
		}
	}
	for _, middleware := range a.middleware {
		router.Use(middleware)
	}

	var err error

	//setup simple app routers
	if a.IsSimpleAPIEnabled() {
		simpleApp := simple_app.NewAgentkitSimpleApp(a.ApiConfig)
		err = simpleApp.SetupRouters(router, config)
		if err != nil {
			return apps.NewSetupError("setup simple API routes", err)
		}
	}

	// A2A has its own session creation path. Until it participates in the
	// lifecycle lock, exposing it for AIO would bypass the sandbox contract.
	if a.aioLifecycle == nil {
		a2aApp := a2a_app.NewAgentkitA2AServerApp(a.ApiConfig)
		err = a2aApp.SetupRouters(router, config)
		if err != nil {
			return apps.NewSetupError("setup A2A routes", err)
		}
	}
	if err := a.setupSessionRoutes(router, config); err != nil {
		return apps.NewSetupError("setup session routes", err)
	}

	for _, setup := range a.routeSetups {
		if err = setup(router, config); err != nil {
			return apps.NewSetupError("setup custom routes", err)
		}
	}

	launchConfig := &launcher.Config{
		SessionService:   config.SessionService,
		ArtifactService:  config.ArtifactService,
		MemoryService:    config.MemoryService,
		AgentLoader:      config.AgentLoader,
		A2AOptions:       config.A2AOptions,
		PluginConfig:     config.PluginConfig,
		TelemetryOptions: config.TelemetryOptions,
	}

	if a.IsWebUIEnabled() {
		// setup webui routers
		webuiLauncher := webui.NewLauncher()
		_, err = webuiLauncher.Parse([]string{
			"--api_server_address", a.GetAPIPath(),
		})

		if err != nil {
			return apps.NewSetupError("parse WebUI parameters", err)
		}

		err = webuiLauncher.SetupSubrouters(router, launchConfig)
		if err != nil {
			return apps.NewSetupError("setup WebUI routes", err)
		}

		webuiLauncher.UserMessage(a.GetWebUrl(), log.Println)
	}

	// setup web api routers
	// Create the ADK REST API handler
	apiHandler, err := adkrest.NewServer(adkrest.ServerConfig{
		SessionService:  config.SessionService,
		MemoryService:   config.MemoryService,
		AgentLoader:     config.AgentLoader,
		ArtifactService: config.ArtifactService,
		SSEWriteTimeout: a.SEEWriteTimeout,
		PluginConfig:    config.PluginConfig,
	})
	if err != nil {
		return apps.NewSetupError("create ADK REST server", err)
	}

	var wrappedHandler http.Handler = http.StripPrefix(a.ApiPathPrefix, apiHandler)
	wrappedHandler = a.aioRunHandler(wrappedHandler, config.AgentLoader)
	if !config.DisableObservability {
		wrappedHandler = observability.HTTPMiddleware(wrappedHandler)
	}
	router.Methods("GET", "POST", "DELETE", "OPTIONS").PathPrefix(fmt.Sprintf("%s/", a.ApiPathPrefix)).Handler(wrappedHandler)

	log.Infof("       api:  you can access API using %s", a.GetAPIPath())
	log.Infof("       api:      for instance: %s/list-apps", a.GetAPIPath())

	return nil
}

func sameSessionService(a, b any) bool {
	if a == nil || b == nil {
		return false
	}
	left, right := reflect.ValueOf(a), reflect.ValueOf(b)
	return left.Type() == right.Type() && left.Type().Comparable() && left.Interface() == right.Interface()
}

func (a *AgentkitServerApp) GetApiConfig() *apps.ApiConfig {
	return a.ApiConfig
}

func (a *AgentkitServerApp) GetServerName() string {
	return serverName
}
