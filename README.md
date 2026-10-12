<p align="center">
    <img src="assets/images/logo.png" alt="Volcengine Agent Development Kit Logo" width="50%">
</p>

# Volcengine Agent Development Kit

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

An open-source kit for agent development, integrated the powerful capabilities of Volcengine.

中文版本: [README_zh.md](README_zh.md)

For more details, see our [documents](https://agentkit.gitbook.io/docs/veadk-go).

## Version support

VeADK Go maintains two major versions after the ADK v2 upgrade:

| Version | Module path | ADK dependency | Branch | Status |
| --- | --- | --- | --- | --- |
| V2 | `github.com/volcengine/veadk-go/v2` | `google.golang.org/adk/v2` | `main` | Active development. Use this version for new projects. |
| V1 | `github.com/volcengine/veadk-go` | `google.golang.org/adk` | `release/v1` | Maintenance. Use this version for existing V1 projects that cannot migrate yet. |

V2 contains breaking changes required by ADK v2 and A2A v2. V1 receives only compatibility, security, and critical bug fixes.

## Installation

Before you start, make sure you have the following installed:
- Go 1.26.5 or later

### V2

```bash
go get github.com/volcengine/veadk-go/v2
```

Use V2 imports in your code:

```go
import (
	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"google.golang.org/adk/v2/agent"
)
```

### V1

For existing V1 projects, install a V1 release:

```bash
go get github.com/volcengine/veadk-go@v1.0.1
```

Use V1 imports in your code:

```go
import (
	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"google.golang.org/adk/agent"
)
```

The V1 maintenance branch is `release/v1`. Check that branch for V1 examples and documentation:

```bash
git checkout release/v1
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

Enjoy a minimal V2 agent from VeADK:

```go
package main

import (
	"context"
	"fmt"
	"os"
	
	_ "github.com/volcengine/veadk-go/v2/agent"
	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"github.com/volcengine/veadk-go/v2/log"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/full"
	"google.golang.org/adk/v2/session"
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

For V1, use the same structure with V1 import paths:

```go
import (
	_ "github.com/volcengine/veadk-go/agent"
	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/log"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/session"
)
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

## Contributing

We welcome code, documentation, examples, and tests. Before sending a pull request, read [CONTRIBUTING.md](CONTRIBUTING.md) to choose the correct V1 or V2 branch and follow the compatibility rules.

## Security and privacy
This project takes security seriously.
For vulnerability reporting and supported versions, see [SECURITY.md](SECURITY.md)


## License

This project is licensed under the Apache 2.0 License.
