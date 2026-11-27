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

[Ark Responses 与工具返回图片](examples/model/ark_responses/README_zh.md)

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

## 通过 VeADK Server 删除制品的指定版本

使用 `apps/agentkit_server_app.NewAgentkitServerApp` 并配置
`apps.RunConfig.ArtifactService` 后，可调用以下接口删除一个已保存的版本：

```http
DELETE /apps/{app_name}/users/{user_id}/sessions/{session_id}/artifacts/{artifact_name}/versions/{version}
```

路径前需添加配置的 `ApiPathPrefix`，例如 `/api`。`version` 必须为大于 0
的 int64 整数；非法值、0 和负数返回 HTTP 400。成功返回 HTTP 200 和空响应体，
底层失败返回 HTTP 500 及对应错误文本，未配置 ArtifactService 时返回受控的
HTTP 500。其他版本仍可使用，原有 `DELETE .../artifacts/{artifact_name}`
继续删除该制品的全部版本。

该接口由 VeADK `AgentkitServerApp` 提供。上文原生 ADK 的 `full.NewLauncher()`
使用上游 REST 路由，不包含此 VeADK 扩展接口。

## 直接使用 Runner

如果你希望在代码中直接调用 Agent，并且只关心最终文本响应，可以使用 `runner.RunText`：

```go
r, err := runner.New(runner.Config{
	AppName:           "demo",
	Agent:             veAgent,
	AutoCreateSession: true,
})
if err != nil {
	return err
}

answer, err := r.RunText(ctx, "user", "session", "hello", agent.RunConfig{})
if err != nil {
	return err
}
fmt.Println(answer)
```

如果需要多模态或多 part 输入，可以继续使用 ADK Go 的 `genai.Part` 类型，并从事件流中收集最终文本：

```go
msg := runner.UserParts(
	genai.NewPartFromText("describe this image"),
	genai.NewPartFromBytes(imageBytes, "image/png"),
)

answer, err := runner.CollectFinalText(
	r.Run(ctx, "user", "session", msg, agent.RunConfig{}),
)
```

如果需要流式 UI、工具确认、Tracing 或其他高级流程，仍然可以直接遍历 `Run` 返回的原始事件流。多轮对话时复用相同的 `userID` 和 `sessionID` 即可延续会话上下文。

可运行示例和 runner 用法说明见 [examples/runner](examples/runner)。

## 贡献

欢迎贡献代码、文档、示例和测试。提交 Pull Request 前，请阅读[贡献指南](CONTRIBUTING_zh.md)，选择正确的 V1 或 V2 分支，并遵守兼容性规则。

## 安全和隐私

本项目重视安全问题。漏洞报告和受支持版本请参考 [SECURITY.md](SECURITY.md)。

## License

本项目使用 Apache 2.0 License。
