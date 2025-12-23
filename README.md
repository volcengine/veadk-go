<p align="center">
    <img src="assets/images/logo.png" alt="Volcengine Agent Development Kit Logo" width="50%">
</p>

# Volcengine Agent Development Kit

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

An open-source kit for agent development, integrated the powerful capabilities of Volcengine.

For more details, see our [documents](https://volcengine.github.io/veadk-python/).

## Installation

Before you start, make sure you have the following installed:
- Go 1.24.4 or later


```bash
go get github.com/volcengine/veadk-go
```

## Configuration

We recommand you to create a `config.yaml` file in the root directory of your own project, `VeADK` is able to read it automatically. For running a minimal agent, you just need to set the following configs in your `config.yaml` file:

```yaml
model:
  agent:
    provider: openai
    name: doubao-seed-1-6-250615
    api_base: https://ark.cn-beijing.volces.com/api/v3/
    api_key: # <-- set your Volcengine ARK api key here
```

## Have a try

Enjoy a minimal agent from VeADK:

```golang
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/google/uuid"
	_ "github.com/volcengine/veadk-go/agent"
	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

func main() {
	ctx := context.Background()
	veAgent, err := veagent.New(&veagent.Config{
		ModelExtraConfig: map[string]any{
			"extra_body": map[string]any{
				"thinking": map[string]string{
					"type": "disabled",
				},
			},
		},
	})
	if err != nil {
		fmt.Printf("NewVeAgent failed: %v", err)
		return
	}

	appName := "veAgent_app"
	userID := "user-1234"
	sessionId := fmt.Sprintf("%s-%s", session.KeyPrefixTemp, uuid.NewString())
	sessionService := session.InMemoryService()
	agentRunner, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          veAgent,
		SessionService: sessionService,
	})
	if err != nil {
		log.Printf("New runner error:%v", err)
		return
	}

	_, err = sessionService.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionId,
	})
	if err != nil {
		log.Printf("Create session error:%v", err)
		return
	}

	for event, err := range agentRunner.Run(ctx, userID, sessionId, genai.NewContentFromText("你好", genai.RoleUser), agent.RunConfig{StreamingMode: agent.StreamingModeNone}) {
		if err != nil {
			log.Printf("got unexpected error: %v", err)
		}

		eventStr, _ := json.Marshal(event)
		log.Printf("got event: %s\n", string(eventStr))
	}
}

```

## Run your agent

1、Run with command-line interface

Run your agent using the following Go command:

```shell
go run agent.go
```

2、Run with web interface

Run your agent with the ADK web interface using the following Go command:

```shell
go run agent.go web api webui
```

If a large agent takes a long time to run, you can increase the timeout parameter.

```shell
go run agent.go web -read-timeout 3m -write-timeout 3m api 
```



## License

This project is licensed under the Apache 2.0 License.
