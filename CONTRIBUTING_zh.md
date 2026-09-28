# 贡献指南

感谢你帮助改进 Volcengine Agent Development Kit for Go。本文档说明如何选择正确的版本线、准备变更，以及如何在 V1 和 V2 之间共享修复。

English version: [CONTRIBUTING.md](CONTRIBUTING.md)

## 选择正确的版本

VeADK Go 同时维护两个大版本：

| 版本 | 分支 | 模块路径 | ADK 依赖 | 贡献策略 |
| --- | --- | --- | --- | --- |
| V2 | `main` | `github.com/volcengine/veadk-go/v2` | `google.golang.org/adk/v2` | 主开发线。新功能和破坏性更新进入这里。 |
| V1 | `release/v1` | `github.com/volcengine/veadk-go` | `google.golang.org/adk` | 维护线。只接收安全修复、关键 bugfix 和低风险兼容修复。 |

除非你正在修复影响存量 V1 用户的问题，否则新工作默认选择 V2。

## V1 和 V2 之间可以共享什么

有些变更应该同时应用到两个版本线，有些变更应该只保留在 V2。

| 变更类型 | V2 | V1 |
| --- | --- | --- |
| 安全修复 | 是 | 是 |
| 关键生产 bugfix | 是 | 通常是 |
| 低风险兼容修复 | 是 | 视情况而定 |
| 新功能 | 是 | 否 |
| 破坏性 API 变更 | 是 | 否 |
| ADK v2 或 A2A v2 迁移 | 是 | 否 |
| 没有用户可见修复的重构 | 有价值时可以 | 否 |
| 文档 | 是 | 适用于 V1 时可以 |

如果一个修复同时影响两个版本，请先在 V2 上完成，再把最小兼容补丁 backport 到 V1。不要把 V2 整体 merge 到 `release/v1`。

## 分支和 Pull Request 流程

V2 变更：

```bash
git checkout main
git pull
git checkout -b feat/your-change
```

V1 修复：

```bash
git checkout release/v1
git pull
git checkout -b fix/v1-your-change
```

同时影响 V1 和 V2 的修复：

1. 先向 `main` 提交主 Pull Request。
2. V2 修复被接受后，将最小补丁 cherry-pick 或手工 backport 到 `release/v1`。
3. 单独提交 V1 Pull Request，并在描述中关联 V2 Pull Request。

请保持 V1 和 V2 Pull Request 分离，方便 reviewer 检查各自版本线的 import 路径、依赖和兼容性规则。

## 兼容性规则

V2 代码必须使用 V2 模块路径：

```go
import (
	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"google.golang.org/adk/v2/agent"
)
```

V1 代码必须使用 V1 模块路径：

```go
import (
	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"google.golang.org/adk/agent"
)
```

不要在同一版本线中混用 V1 和 V2 import 路径。尤其注意：

- 不要向 `release/v1` 添加 `github.com/volcengine/veadk-go/v2` import。
- 不要向 `release/v1` 添加 `google.golang.org/adk/v2` import。
- 不要在新的 V2 代码中引入 V1 ADK 或 A2A API。
- 示例和 README 代码片段必须和所在分支保持一致。

## 开发检查

提交 Pull Request 前，请根据变更范围运行检查：

```bash
gofmt -w <changed-go-files>
go test ./...
```

如果修改了依赖或模块路径，也请检查：

```bash
go mod tidy
git diff --check
```

V2 变更需要确认相关 Go 文件使用必要的 `/v2` import 路径。V1 变更需要确认没有引入 VeADK 或 ADK 的 `/v2` import。

## Pull Request 检查清单

请求 review 前，请确认：

- Pull Request 指向正确分支。
- 变更只作用于一个版本线，除非这是明确的 backport。
- 如果变更影响运行时行为，已经补充测试或示例。
- 如果公共 API、配置或示例发生变化，已经同步更新文档。
- 安全问题请通过 `SECURITY.md` 中的流程报告，不要提交公开 issue。

## 发布

V2 发布使用 `v2.0.0` 及后续 tag。

V1 发布从 `release/v1` 分支打 `v1.0.1` 及后续 tag。

当 backport 对 V1 用户重要时，应发布对应的 V1 patch 版本。
