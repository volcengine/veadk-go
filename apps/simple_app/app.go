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

package simple_app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/apps"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher/web"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

type agentkitSimpleApp struct {
	*apps.ApiConfig
	appName string
	userID  string
	session session.Session
	runner  *runner.Runner
}

func NewAgentkitSimpleApp(config *apps.ApiConfig) apps.BasicApp {
	return &agentkitSimpleApp{
		ApiConfig: config,
		appName:   "agentkit_simple_app",
		userID:    "agentkit_user",
	}
}

func (app *agentkitSimpleApp) SetupRouters(router *mux.Router, config *apps.RunConfig) error {
	ctx := context.Background()
	sessionService := config.SessionService
	if sessionService == nil {
		sessionService = session.InMemoryService()
	}

	if app.appName == "" {
		app.appName = config.AgentLoader.RootAgent().Name()
	}

	if app.userID == "" {
		app.userID = "agentkit_user"
	}

	resp, err := sessionService.Create(ctx, &session.CreateRequest{
		AppName: app.appName,
		UserID:  app.userID,
	})
	if err != nil {
		return fmt.Errorf("failed to create the session service: %w", err)
	}
	app.session = resp.Session

	r, err := runner.New(runner.Config{
		AppName:         app.appName,
		Agent:           config.AgentLoader.RootAgent(),
		SessionService:  sessionService,
		ArtifactService: config.ArtifactService,
		MemoryService:   config.MemoryService,
	})
	if err != nil {
		return fmt.Errorf("new runner error: %w", err)
	}
	app.runner = r

	router.NewRoute().Path("/invoke").Methods(http.MethodPost).HandlerFunc(app.newInvokeHandler())
	router.NewRoute().Path("/health").Methods(http.MethodGet).HandlerFunc(app.newHealthHandler())

	log.Printf("       invoke:  you can invoke agent using %s/invoke", app.GetWebUrl())
	log.Printf("       health:  you can get health status using: %s/health", app.GetWebUrl())

	return nil
}

func (app *agentkitSimpleApp) Run(ctx context.Context, config *apps.RunConfig) error {
	router := web.BuildBaseRouter()

	log.Printf("Web servers starts on %s", app.GetWebUrl())
	err := app.SetupRouters(router, config)
	if err != nil {
		return fmt.Errorf("setup simple app router error: %w", err)
	}

	srv := http.Server{
		Addr:         fmt.Sprintf(":%s", fmt.Sprint(app.Port)),
		WriteTimeout: app.WriteTimeout,
		ReadTimeout:  app.ReadTimeout,
		IdleTimeout:  app.IdleTimeout,
		Handler:      router,
	}

	err = srv.ListenAndServe()
	if err != nil {
		return fmt.Errorf("server failed: %v", err)
	}

	return nil
}

type Request struct {
	Prompt string `json:"prompt"`
}

type Response struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	SessionId string `json:"session_id"`
	Data      string `json:"data"`
}

func (app *agentkitSimpleApp) newInvokeHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var req Request
		ctx := context.Background()

		body, err := io.ReadAll(r.Body)
		defer func() {
			_ = r.Body.Close()
		}()
		if err != nil {
			res := Response{Code: http.StatusBadRequest, Message: fmt.Sprintf("read request error: %s", err.Error()), Data: ""}
			_ = json.NewEncoder(w).Encode(res)
			return
		}

		err = json.Unmarshal(body, &req)
		if err != nil {
			res := Response{Code: 400, Message: fmt.Sprintf("json unmarshal %s error:%v", string(body), err), Data: ""}
			_ = json.NewEncoder(w).Encode(res)
			return
		}

		userInput := genai.NewContentFromText(req.Prompt, "user")

		var finalResponseText []string
		for event, err := range app.runner.Run(ctx, app.userID, app.session.ID(), userInput, agent.RunConfig{StreamingMode: agent.StreamingModeNone}) {
			if err != nil {
				log.Printf("Agent Run Error: %v", err)
				continue
			}
			if event.Content != nil && !event.Partial {
				for _, part := range event.Content.Parts {
					if !part.Thought {
						finalResponseText = append(finalResponseText, part.Text)
					}
				}
			}
		}

		res := Response{
			Code:      200,
			Message:   "success",
			SessionId: app.session.ID(),
			Data:      strings.Join(finalResponseText, ""),
		}
		_ = json.NewEncoder(w).Encode(res)
	}
}

func (app *agentkitSimpleApp) newHealthHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		res := Response{
			Code:    200,
			Message: "success",
			Data:    fmt.Sprintf("Service %s is running ...", app.appName),
		}
		_ = json.NewEncoder(w).Encode(res)
	}
}
