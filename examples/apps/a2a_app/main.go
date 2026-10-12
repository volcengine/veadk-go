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

package main

import (
	"context"
	"flag"

	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"github.com/volcengine/veadk-go/v2/apps"
	"github.com/volcengine/veadk-go/v2/apps/a2a_app"
	"github.com/volcengine/veadk-go/v2/log"
	"google.golang.org/adk/v2/agent"
)

func main() {
	host := flag.String("host", "127.0.0.1", "HTTP listen host")
	port := flag.Int("port", 8000, "HTTP listen port")
	a2aPath := flag.String("a2a-path", "/", "internal A2A JSON-RPC route")
	a2aPublicPath := flag.String("a2a-public-path", "", "A2A route advertised by Agent Cards (defaults to --a2a-path)")
	publicURL := flag.String("public-url", "", "external base URL advertised by Agent Cards")
	trustProxyHeaders := flag.Bool("trust-proxy-headers", false, "trust Forwarded and X-Forwarded-* headers from a reverse proxy")
	flag.Parse()

	ctx := context.Background()

	a, err := veagent.New(&veagent.Config{})
	if err != nil {
		log.Errorf("NewLLMAgent failed: %v", err)
		return
	}

	apiConfig := apps.DefaultApiConfig().
		SetHost(*host).
		SetPort(*port).
		SetA2APath(*a2aPath).
		SetA2APublicPath(*a2aPublicPath).
		SetPublicURL(*publicURL)
	if *trustProxyHeaders {
		apiConfig.SetAgentCardURLResolver(apps.ForwardedAgentCardURL)
	}

	a2aApp := a2a_app.NewAgentkitA2AServerApp(apiConfig)

	err = a2aApp.Run(ctx, &apps.RunConfig{
		AgentLoader: agent.NewSingleLoader(a),
	})
	if err != nil {
		log.Errorf("Run failed: %v", err)
	}
}
