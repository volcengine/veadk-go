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
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/gorilla/mux"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/artifact"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
)

type RunConfig struct {
	SessionService  session.Service
	ArtifactService artifact.Service
	MemoryService   memory.Service
	AgentLoader     agent.Loader
	A2AOptions      []a2asrv.RequestHandlerOption
	PluginConfig    runner.PluginConfig
}

type ApiConfig struct {
	Port            int
	WriteTimeout    time.Duration
	ReadTimeout     time.Duration
	IdleTimeout     time.Duration
	SEEWriteTimeout time.Duration
	ApiPathPrefix   string
}

type BasicApp interface {
	Run(ctx context.Context, config *RunConfig) error
	SetupRouters(router *mux.Router, config *RunConfig) error
}

func DefaultApiConfig() *ApiConfig {
	return &ApiConfig{
		Port:            8000,
		WriteTimeout:    time.Second * 60,
		ReadTimeout:     time.Second * 60,
		IdleTimeout:     time.Second * 120,
		SEEWriteTimeout: time.Second * 300,
		ApiPathPrefix:   "", // set /api same as ADK-Go
	}
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

func (a *ApiConfig) SetSEEWriteTimeout(t int64) *ApiConfig {
	a.SEEWriteTimeout = time.Second * time.Duration(t)
	return a
}

func (a *ApiConfig) SetApiPathPrefix(p string) *ApiConfig {
	a.ApiPathPrefix = p
	return a
}

func (a *ApiConfig) GetWebUrl() string {
	return fmt.Sprintf("http://localhost:%d", a.Port)
}

func (a *ApiConfig) GetAPIPath() string {
	return fmt.Sprintf("http://localhost:%d%s", a.Port, a.ApiPathPrefix)
}
