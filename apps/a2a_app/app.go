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

package a2a_app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"

	a2acore "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/apps"
	"google.golang.org/adk/cmd/launcher/web"
	"google.golang.org/adk/cmd/launcher/web/a2a"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/server/adka2a"
	"google.golang.org/adk/session"
)

const apiPath = "/"

type agentkitA2AServerApp struct {
	apps.ApiConfig
	a2aAgentUrl string
}

func (a *agentkitA2AServerApp) Run(ctx context.Context, config *apps.RunConfig) error {
	router := web.BuildBaseRouter()

	if config.SessionService == nil {
		config.SessionService = session.InMemoryService()
	}

	log.Printf("Web servers starts on %s", a.GetWebUrl())
	err := a.SetupRouters(router, config)
	if err != nil {
		return fmt.Errorf("setup a2a routers failed: %w", err)
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

func (a *agentkitA2AServerApp) SetupRouters(router *mux.Router, config *apps.RunConfig) error {
	publicURL, err := url.JoinPath(a.a2aAgentUrl, apiPath)
	if err != nil {
		return err
	}

	rootAgent := config.AgentLoader.RootAgent()
	agentCard := &a2acore.AgentCard{
		Name:                              rootAgent.Name(),
		Description:                       rootAgent.Description(),
		DefaultInputModes:                 []string{"text/plain"},
		DefaultOutputModes:                []string{"text/plain"},
		URL:                               publicURL,
		PreferredTransport:                a2acore.TransportProtocolJSONRPC,
		Skills:                            adka2a.BuildAgentSkills(rootAgent),
		Capabilities:                      a2acore.AgentCapabilities{Streaming: true},
		SupportsAuthenticatedExtendedCard: false,
	}
	router.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(agentCard))

	agent := config.AgentLoader.RootAgent()
	executor := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig: runner.Config{
			AppName:         agent.Name(),
			Agent:           agent,
			SessionService:  config.SessionService,
			ArtifactService: config.ArtifactService,
			MemoryService:   config.MemoryService,
		},
	})
	reqHandler := a2asrv.NewHandler(executor, config.A2AOptions...)
	router.Handle(apiPath, a2asrv.NewJSONRPCHandler(reqHandler))

	a2aLauncher := a2a.NewLauncher()
	a2aLauncher.UserMessage(a.GetWebUrl()+apiPath, log.Println)

	return nil
}

func NewAgentkitA2AServerApp(config apps.ApiConfig) apps.BasicApp {
	return &agentkitA2AServerApp{
		ApiConfig:   config,
		a2aAgentUrl: config.GetWebUrl(),
	}
}
