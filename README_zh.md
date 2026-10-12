<p align="center">
    <img src="assets/images/logo.png" alt="Volcengine Agent Development Kit Logo" width="50%">
</p>

# 火山引擎 Agent Development Kit

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

火山引擎 Agent Development Kit for Go 是一个面向 Agent 开发的开源工具包，集成了火山引擎的相关能力。

English version: [README.md](README.md)

更多信息请参考[文档](https://agentkit.gitbook.io/docs/veadk-go)。

## 版本支持

ADK v2 升级后，VeADK Go 同时维护两个大版本：

| 版本 | 模块路径 | ADK 依赖 | 分支 | 状态 |
| --- | --- | --- | --- | --- |
| V2 | `github.com/volcengine/veadk-go/v2` | `google.golang.org/adk/v2` | `main` | 主开发线。新项目推荐使用该版本。 |
| V1 | `github.com/volcengine/veadk-go` | `google.golang.org/adk` | `release/v1` | 维护线。适合暂时无法迁移的存量 V1 项目。 |

V2 包含 ADK v2 和 A2A v2 所需的破坏性更新。V1 只接收兼容性修复、安全修复和关键 bugfix。

## 安装

开始前请确认已安装：

- Go 1.26.5 或更高版本

### V2

```bash
go get github.com/volcengine/veadk-go/v2
```

在代码中使用 V2 import 路径：

```go
import (
	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"google.golang.org/adk/v2/agent"
)
```

### V1

存量 V1 项目可以安装 V1 版本：

```bash
go get github.com/volcengine/veadk-go@v1.0.1
```

在代码中使用 V1 import 路径：

```go
import (
	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"google.golang.org/adk/agent"
)
```

V1 维护分支是 `release/v1`。如果需要查看 V1 示例和文档，可以切换到该分支：

```bash
git checkout release/v1
```

## 配置

建议在你自己的项目根目录创建 `config.yaml`。VeADK 会自动读取它。运行一个最小 Agent 时，只需要在 `config.yaml` 中配置以下内容：

```yaml
model:
  agent:
    provider: openai
    name: doubao-seed-1-6-250615
    api_base: https://ark.cn-beijing.volces.com/api/v3/
    api_key: # <-- 在这里填写你的火山引擎 ARK API Key
```

## 快速体验

下面是一个最小 V2 Agent 示例：

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

V1 项目可以使用相同结构，但需要使用 V1 import 路径：

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

## 运行 Agent

1. 使用命令行运行

```shell
go run agent.go
```

2. 使用 Web 界面运行

```shell
go run agent.go web api webui
```

如果 Agent 运行时间较长，可以增加超时时间：

```shell
go run agent.go web -read-timeout 3m -write-timeout 3m api webui
```

## 贡献

欢迎贡献代码、文档、示例和测试。提交 Pull Request 前，请阅读[贡献指南](CONTRIBUTING_zh.md)，选择正确的 V1 或 V2 分支，并遵守兼容性规则。

## 安全和隐私

本项目重视安全问题。漏洞报告和受支持版本请参考 [SECURITY.md](SECURITY.md)。

## License

本项目使用 Apache 2.0 License。
