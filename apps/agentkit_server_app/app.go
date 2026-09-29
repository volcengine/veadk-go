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

	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/v2/apps"
	"github.com/volcengine/veadk-go/v2/apps/a2a_app"
	"github.com/volcengine/veadk-go/v2/apps/simple_app"
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
	routeSetups []RouteSetup
	middleware  []mux.MiddlewareFunc
}

// RouteSetup adds application-specific routes after VeADK's exact built-in
// routes and before the ADK REST/WebUI fallback routes.
type RouteSetup func(router *mux.Router, config *apps.RunConfig) error

// Option configures AgentkitServerApp extensions.
type Option func(*AgentkitServerApp)

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
	return apps.Run(ctx, config, a)
}

func (a *AgentkitServerApp) SetupRouters(router *mux.Router, config *apps.RunConfig) error {
	if router == nil {
		return apps.NewSetupError("validate router", fmt.Errorf("router is required"))
	}
	if config == nil || config.AgentLoader == nil || config.AgentLoader.RootAgent() == nil {
		return apps.NewSetupError("validate agent loader", fmt.Errorf("root agent is required"))
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

	//setup a2a routers
	a2aApp := a2a_app.NewAgentkitA2AServerApp(a.ApiConfig)
	err = a2aApp.SetupRouters(router, config)
	if err != nil {
		return apps.NewSetupError("setup A2A routes", err)
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
	if !config.DisableObservability {
		wrappedHandler = observability.HTTPMiddleware(wrappedHandler)
	}
	router.Methods("GET", "POST", "DELETE", "OPTIONS").PathPrefix(fmt.Sprintf("%s/", a.ApiPathPrefix)).Handler(wrappedHandler)

	log.Infof("       api:  you can access API using %s", a.GetAPIPath())
	log.Infof("       api:      for instance: %s/list-apps", a.GetAPIPath())

	return nil
}

func (a *AgentkitServerApp) GetApiConfig() *apps.ApiConfig {
	return a.ApiConfig
}

func (a *AgentkitServerApp) GetServerName() string {
	return serverName
}
