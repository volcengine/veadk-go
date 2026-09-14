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
	"encoding/json"
	"errors"
	"net/http"

	"github.com/volcengine/veadk-go/log"

	a2acore "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/apps"
	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher/web/a2a"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/server/adka2a"
)

const (
	serverName          = "agentkit a2a server"
	legacyAgentCardPath = "/.well-known/agent.json"
)

type agentkitA2AServerApp struct {
	*apps.ApiConfig
}

func (a *agentkitA2AServerApp) Run(ctx context.Context, config *apps.RunConfig) error {
	return apps.Run(ctx, config, a)
}

func (a *agentkitA2AServerApp) SetupRouters(router *mux.Router, config *apps.RunConfig) error {
	if router == nil {
		return errors.New("router is required")
	}
	if config == nil || config.AgentLoader == nil || config.AgentLoader.RootAgent() == nil {
		return errors.New("agent loader with a root agent is required")
	}
	rootAgent := config.AgentLoader.RootAgent()
	agentCard := &a2acore.AgentCard{
		Name:                              rootAgent.Name(),
		Description:                       rootAgent.Description(),
		DefaultInputModes:                 []string{"text/plain"},
		DefaultOutputModes:                []string{"text/plain"},
		URL:                               a.GetA2APublicURL(),
		PreferredTransport:                a2acore.TransportProtocolJSONRPC,
		Skills:                            adka2a.BuildAgentSkills(rootAgent),
		Capabilities:                      a2acore.AgentCapabilities{Streaming: true},
		SupportsAuthenticatedExtendedCard: false,
	}
	cardHandler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCard := *agentCard
		requestCard.URL = a.ResolveAgentCardURL(request)
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(&requestCard); err != nil {
			log.Errorf("failed to encode A2A Agent Card")
		}
	})
	router.Handle(a2asrv.WellKnownAgentCardPath, cardHandler).Methods(http.MethodGet)
	router.Handle(legacyAgentCardPath, cardHandler).Methods(http.MethodGet)

	agent := config.AgentLoader.RootAgent()
	executor := adka2a.NewExecutor(adka2a.ExecutorConfig{
		// Stream model output for A2A events; message/send aggregates the result.
		RunConfig: adkagent.RunConfig{
			StreamingMode: adkagent.StreamingModeSSE,
		},
		RunnerConfig: runner.Config{
			AppName:         agent.Name(),
			Agent:           agent,
			SessionService:  config.SessionService,
			ArtifactService: config.ArtifactService,
			MemoryService:   config.MemoryService,
			PluginConfig:    config.PluginConfig,
		},
	})
	reqHandler := a2asrv.NewHandler(executor, config.A2AOptions...)
	router.Handle(a.GetA2APath(), a2asrv.NewJSONRPCHandler(reqHandler)).Methods(http.MethodPost)

	a2aLauncher := a2a.NewLauncher()
	a2aLauncher.UserMessage(a.GetA2APublicURL(), log.Println)

	return nil
}

func (a *agentkitA2AServerApp) GetApiConfig() *apps.ApiConfig {
	return a.ApiConfig
}

func (a *agentkitA2AServerApp) GetServerName() string {
	return serverName
}

func NewAgentkitA2AServerApp(config *apps.ApiConfig) apps.BasicApp {
	if config == nil {
		config = apps.DefaultApiConfig()
	}
	return &agentkitA2AServerApp{
		ApiConfig: config,
	}
}
