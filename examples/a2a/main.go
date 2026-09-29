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
	"os"

	"github.com/volcengine/veadk-go/v2/agent/remoteagent"
	"github.com/volcengine/veadk-go/v2/apps"
	"github.com/volcengine/veadk-go/v2/apps/agentkit_server_app"
	"github.com/volcengine/veadk-go/v2/log"
	"google.golang.org/adk/v2/agent"
)

func main() {
	remoteURL := flag.String("remote-url", "http://127.0.0.1:8000", "remote A2A server base URL")
	host := flag.String("host", "127.0.0.1", "HTTP listen host")
	port := flag.Int("port", 8001, "HTTP listen port")
	flag.Parse()

	ctx := context.Background()

	remoteAgent, err := remoteagent.NewVeRemoteAgent(
		remoteagent.NewDefaultConfig().
			SetName("remoteAgent").
			SetApiKey(os.Getenv("REMOTE_AGENT_API_KEY")).
			SetBaseUrl(*remoteURL),
	)
	if err != nil {
		log.Errorf("Failed to create a remote agent: %v", err)
		return
	}

	app := agentkit_server_app.NewAgentkitServerApp(
		apps.DefaultApiConfig().SetHost(*host).SetPort(*port),
	)

	err = app.Run(ctx, &apps.RunConfig{
		AgentLoader: agent.NewSingleLoader(remoteAgent),
	})
	if err != nil {
		log.Errorf("Run failed: %v", err)
	}
}
