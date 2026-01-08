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
	"google.golang.org/adk/session"
)

type RunConfig struct {
	SessionService  session.Service
	ArtifactService artifact.Service
	MemoryService   memory.Service
	AgentLoader     agent.Loader
	A2AOptions      []a2asrv.RequestHandlerOption
}

type ApiConfig struct {
	Port            int
	WriteTimeout    time.Duration
	ReadTimeout     time.Duration
	IdleTimeout     time.Duration
	SEEWriteTimeout time.Duration
}

type BasicApp interface {
	Run(ctx context.Context, config *RunConfig) error
	SetupRouters(router *mux.Router, config *RunConfig) error
}

func DefaultApiConfig() ApiConfig {
	return ApiConfig{
		Port:            8000,
		WriteTimeout:    time.Second * 15,
		ReadTimeout:     time.Second * 15,
		IdleTimeout:     time.Second * 60,
		SEEWriteTimeout: time.Second * 300,
	}
}

func (a *ApiConfig) SetPort(port int) {
	a.Port = port
}

func (a *ApiConfig) SetWriteTimeout(t int64) {
	a.WriteTimeout = time.Second * time.Duration(t)
}

func (a *ApiConfig) SetReadTimeout(t int64) {
	a.ReadTimeout = time.Second * time.Duration(t)
}

func (a *ApiConfig) SetIdleTimeout(t int64) {
	a.IdleTimeout = time.Second * time.Duration(t)
}

func (a *ApiConfig) SetSEEWriteTimeout(t int64) {
	a.SEEWriteTimeout = time.Second * time.Duration(t)
}

func (a *ApiConfig) GetWebUrl() string {
	return fmt.Sprintf("http://localhost:%d", a.Port)
}
