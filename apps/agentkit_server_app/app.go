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
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/apps"
	"github.com/volcengine/veadk-go/apps/a2a_app"
	"github.com/volcengine/veadk-go/apps/simple_app"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/web"
	"google.golang.org/adk/cmd/launcher/web/api"
	"google.golang.org/adk/cmd/launcher/web/webui"
	"google.golang.org/adk/session"
)

type agentkitServerApp struct {
	apps.ApiConfig
}

func NewAgentkitA2AServerApp(config apps.ApiConfig) apps.BasicApp {
	return &agentkitServerApp{
		ApiConfig: config,
	}
}

func (a *agentkitServerApp) Run(ctx context.Context, config *apps.RunConfig) error {
	router := web.BuildBaseRouter()

	if config.SessionService == nil {
		config.SessionService = session.InMemoryService()
	}

	log.Printf("Web servers starts on %s", a.GetWebUrl())
	err := a.SetupRouters(router, config)
	if err != nil {
		return fmt.Errorf("setup agentkit server routers failed: %w", err)
	}

	srv := http.Server{
		Addr:         fmt.Sprintf(":%v", fmt.Sprint(a.Port)),
		WriteTimeout: a.WriteTimeout,
		ReadTimeout:  a.ReadTimeout,
		IdleTimeout:  a.IdleTimeout,
		Handler:      router,
	}

	err = srv.ListenAndServe()
	if err != nil {
		return fmt.Errorf("server failed: %v", err)
	}

	return nil
}

func (a *agentkitServerApp) SetupRouters(router *mux.Router, config *apps.RunConfig) error {
	var err error
	// setup simple app routers
	simpleApp := simple_app.NewAgentkitSimpleApp(a.ApiConfig)
	err = simpleApp.SetupRouters(router, config)
	if err != nil {
		return fmt.Errorf("setup simple app routers failed: %w", err)
	}

	// setup a2a routers
	a2aApp := a2a_app.NewAgentkitA2AServerApp(a.ApiConfig)
	err = a2aApp.SetupRouters(router, config)
	if err != nil {
		return fmt.Errorf("setup simple app routers failed: %w", err)
	}

	// setup web api routers
	apiLauncher := api.NewLauncher()
	_, err = apiLauncher.Parse([]string{
		"--webui_address", fmt.Sprintf("localhost:%v", fmt.Sprint(a.Port)),
		"--sse-write-timeout", "5m",
	})

	if err != nil {
		return fmt.Errorf("apiLauncher parse parames failed: %w", err)
	}

	err = apiLauncher.SetupSubrouters(router, &launcher.Config{
		SessionService:  config.SessionService,
		ArtifactService: config.ArtifactService,
		MemoryService:   config.MemoryService,
		AgentLoader:     config.AgentLoader,
		A2AOptions:      config.A2AOptions,
	})
	if err != nil {
		return fmt.Errorf("setup api routers failed: %w", err)
	}

	// setup webui routers
	webuiLauncher := webui.NewLauncher()
	_, err = webuiLauncher.Parse([]string{
		"--api_server_address", fmt.Sprintf("http://localhost:%v/api", fmt.Sprint(a.Port)),
	})

	if err != nil {
		return fmt.Errorf("webuiLauncher parse parames failed: %w", err)
	}

	err = webuiLauncher.SetupSubrouters(router, &launcher.Config{
		SessionService:  config.SessionService,
		ArtifactService: config.ArtifactService,
		MemoryService:   config.MemoryService,
		AgentLoader:     config.AgentLoader,
		A2AOptions:      config.A2AOptions,
	})
	if err != nil {
		return fmt.Errorf("setup webui routers failed: %w", err)
	}

	apiLauncher.UserMessage(a.GetWebUrl(), log.Println)
	webuiLauncher.UserMessage(a.GetWebUrl(), log.Println)

	return nil
}
