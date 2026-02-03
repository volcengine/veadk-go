<p align="center">
    <img src="assets/images/logo.png" alt="Volcengine Agent Development Kit Logo" width="50%">
</p>

# Volcengine Agent Development Kit

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

An open-source kit for agent development, integrated the powerful capabilities of Volcengine.

For more details, see our [documents](https://agentkit.gitbook.io/docs/veadk-go).

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

```go
package main

import (
	"context"
	"fmt"
	"os"
	
	_ "github.com/volcengine/veadk-go/agent"
	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/log"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/session"
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
		log.Errorf("NewVeAgent failed: %v", err)
		return
	}

	config := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(veAgent),
		SessionService: session.InMemoryService(),
	}

	l := full.NewLauncher()
	if err = l.Execute(ctx, config, os.Args[1:]); err != nil {
		log.Fatalf("Run failed: %v\n\n%s", err, l.CommandLineSyntax())
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
go run agent.go web -read-timeout 3m -write-timeout 3m api webui
```



## License

This project is licensed under the Apache 2.0 License.
