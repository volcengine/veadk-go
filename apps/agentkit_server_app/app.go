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
	"github.com/volcengine/veadk-go/apps"
	"github.com/volcengine/veadk-go/apps/a2a_app"
	"github.com/volcengine/veadk-go/apps/simple_app"
	"github.com/volcengine/veadk-go/log"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/web/webui"
	"google.golang.org/adk/server/adkrest"
)

const serverName = "agentkit server"

type agentkitServerApp struct {
	*apps.ApiConfig
}

func NewAgentkitServerApp(config *apps.ApiConfig) apps.BasicApp {
	return &agentkitServerApp{
		ApiConfig: config,
	}
}

func (a *agentkitServerApp) Run(ctx context.Context, config *apps.RunConfig) error {
	return apps.Run(ctx, config, a)
}

func (a *agentkitServerApp) SetupRouters(router *mux.Router, config *apps.RunConfig) error {
	var err error

	//setup simple app routers
	simpleApp := simple_app.NewAgentkitSimpleApp(a.ApiConfig)
	err = simpleApp.SetupRouters(router, config)
	if err != nil {
		return fmt.Errorf("setup simple app routers failed: %w", err)
	}

	//setup a2a routers
	a2aApp := a2a_app.NewAgentkitA2AServerApp(a.ApiConfig)
	err = a2aApp.SetupRouters(router, config)
	if err != nil {
		return fmt.Errorf("setup simple app routers failed: %w", err)
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

	// setup webui routers
	webuiLauncher := webui.NewLauncher()
	_, err = webuiLauncher.Parse([]string{
		"--api_server_address", a.GetAPIPath(),
	})

	if err != nil {
		return fmt.Errorf("webuiLauncher parse parames failed: %w", err)
	}

	//webuiLauncher.AddSubrouter(router, w.config.pathPrefix, w.config.backendAddress)
	err = webuiLauncher.SetupSubrouters(router, launchConfig)
	if err != nil {
		return fmt.Errorf("setup webui routers failed: %w", err)
	}

	webuiLauncher.UserMessage(a.GetWebUrl(), log.Println)

	// setup web api routers
	// Create the ADK REST API handler
	apiHandler := adkrest.NewHandler(launchConfig, a.SEEWriteTimeout)

	// Wrap it with CORS middleware
	corsHandler := corsWithArgs(a.GetWebUrl())(apiHandler)

	router.Methods("GET", "POST", "DELETE", "OPTIONS").PathPrefix(fmt.Sprintf("%s/", a.ApiPathPrefix)).Handler(
		http.StripPrefix(a.ApiPathPrefix, corsHandler),
	)

	log.Infof("       api:  you can access API using %s", a.GetAPIPath())
	log.Infof("       api:      for instance: %s/list-apps", a.GetAPIPath())

	return nil
}

func (a *agentkitServerApp) GetApiConfig() *apps.ApiConfig {
	return a.ApiConfig
}

func (a *agentkitServerApp) GetServerName() string {
	return serverName
}

func corsWithArgs(frontendAddress string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", frontendAddress)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
