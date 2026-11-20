# Review Summary

- **本次需求：**为 VeADK Go 新增按版本删除 Artifact 的 REST API，保留删除全部版本的旧接口。
- **需求来源：**开发者功能特性；关联 Meego 需求 `7397937083`，优先级 P1。
- 类型与变更面：Open API、Server 路由、参数校验与错误处理、兼容性；依据为需求文本、`apps/agentkit_server_app/app.go` 和官方 ADK Artifact 实现。
- 差异判断：仅面向 Go V2/main。依据 `CONTRIBUTING.md`，新特性不回移 Go V1；本次不是 MR 同步，不要求 Python/Java 跟进。
- **本次变更关键信息：**用户已确认仅允许正整数 int64；0 和负数返回 400，底层 Delete 零次调用。
- **Review 重点：**`Version=0` 的全量删除语义、API 前缀、HTTP 方法匹配、缺失路径与 handler 缺参的差别、底层失败映射。

## Scope Note

展开 API、后端行为、兼容性、验收及验证附录；不涉及前端 UX、新数据模型、存储实现、埋点和 Dashboard。范围较小，不单独维护追溯矩阵。版本取值边界已由用户确认，决策记录见第 6 节。

# PRD：VeADK Go Artifact 按版本删除 API

## 1. 需求来源与目标

| 项目 | 内容 |
|---|---|
| 来源 | 开发者功能特性；需求原文和 Meego 最新详情一致 |
| 目标仓库 | `volcengine/veadk-go`，`apps/agentkit_server_app` |
| 交付分支 | `feat/9e0bff2c-q1yf`；基线 `main` |
| 调研基线 | 2026-11-20 fetch 后 `origin/main`：`8034d8e9680e6ce87500bc1370aadf80deb3645f` |
| 共享上下文 | CLI 返回 Context 4，`resolved`；唯一目标仓库为上述 Go 仓库 |
| 影响能力 | Artifact / Server，属于封装原生 ADK 能力 |
| 依赖 | Go 1.26.5、ADK Go V2 v2.1.0、gorilla/mux v1.8.1、genai v1.63.0 |
| 格式参考 | [prd文档标准](https://bytedance.larkoffice.com/wiki/IzN5wcZxMi1yskkhQutchFhWnlh)，读取 revision 264 |

用户是通过 REST 管理 Agent 会话制品的开发者。目标是删除一个历史版本，同时保留同名制品其他版本，避免调用全量删除接口导致数据丢失。

## 2. 调研现状与 Bug 验证

- 调研路径：读取最新 main、依赖锁定版本的官方路由/controller/service，启动实际 VeADK AgentkitServerApp，由独立 HTTP 客户端验证；应用和验证脚本分开。
- 当前 REST 并非 VeADK 自己实现：`AgentkitServerApp.SetupRouters` 创建 `adkrest.NewServer`，在 API 前缀下用 fallback handler 托管官方 API。
- 官方锁定版本已有按版本 GET 和全量 DELETE；没有按版本 DELETE。官方 `ArtifactsAPIController` 导出，但路由及部分参数辅助类型处于 `internal` 包。
- 底层 `artifact.InMemoryService().Delete`：非零版本只删一个版本，零值删除全部；自动保存版本从 1 开始。不存在条目删除成功，符合 Service 的幂等契约。
- **问题定性：**属于 REST 能力缺口。本轮完成主分支端到端验证；结果见附录。版本 DELETE 应用层尚未新增，不能将基线验证写成新功能通过。
- `ArtifactService` 为调用方配置的服务，新接口需使用同一实例。没有配置该服务时，应返回明确失败，不能 panic 或返回删除成功。

## 3. 需求范围与行为

### REQ-001：指定版本删除（P1）

User Story：作为 REST 客户端开发者，我希望仅删除指定 Artifact 版本，以便清理历史内容并保留其他版本。

- When 客户端请求完整合法的版本删除路径，VeADK shall 用五个路径参数构造 `artifact.DeleteRequest`，将解析得到的 `int64` 写入 `Version`，调用配置的 `ArtifactService.Delete(req.Context(), request)`。
- When 底层删除成功，VeADK shall 返回 HTTP 200，响应沿用旧删除接口的空响应表示。
- VeADK shall 仅删除目标版本，保留其他版本的编号和内容；保留不同 app/user/session/name 的制品，按 ADK 自带 `user:` 命名空间规则处理。
- When 指定版本不存在且底层返回 nil，VeADK shall 返回 200；不额外进行 Load 存在性检查，不引入新的 404 删除语义。

```gherkin
Scenario: 清理中间历史版本
  Given report.txt 有版本 1、2、3，内容分别为 content-1、content-2、content-3
  When 客户端 DELETE report.txt/versions/2
  Then 响应为 200
  And 版本 2 无法再加载
  And 版本 1 和 3 的内容保持原值
  And 再次删除版本 2 仍成功
```

### REQ-002：输入与失败处理（P1）

- If handler 接收缺失或为空的 app_name、user_id、session_id、artifact_name 或 version，VeADK shall 返回 400，指出缺失字段，且不调用 Delete。
- If version 为非整数或超出 int64 范围，VeADK shall 返回 400，且不调用 Delete；路由不能用仅匹配数字的正则把非法版本变成 404/405。
- **已确认契约：**version 必须为正整数 int64，范围 `1..9223372036854775807`。If version 为 `0` 或负数，VeADK shall 返回 400 且不调用 Delete，防止零值触发全量删除。使用十进制 int64 解析，不 trim 空白，不将空值默认成 0；正号和前导零按标准十进制解析接受（如 `+1`、`01` 均为版本 1），不新增不同版本含义。
- If ArtifactService.Delete 返回错误，VeADK shall 按锁定 ADK 的旧删除行为返回 500 及现有 HTTP 错误表示，不能吞错或返回 200；不增加业务错误码体系。
- If 未配置 ArtifactService，VeADK shall 返回 500 的明确服务未配置错误；这只约束新增 handler，不扩展修复旧 API。
- 缺失 URL 段导致路由不匹配时，沿用现有路由的 404/重定向行为，不补宽泛删除路由；必须证明缺段不会落到全量删除接口。handler 缺参 400 由直接 handler 测试覆盖。

### REQ-003：兼容与集成（P1）

- VeADK shall 保持旧 DELETE 路径及请求语义不变：删除同名制品全部版本；旧路径上的查询参数 `version` 不改变旧语义。
- VeADK shall 保持 list/load 和按版本 GET 的路由、响应、错误行为不变；新 handler 仅处理版本 DELETE，不吞掉同路径 GET。
- VeADK shall 支持默认空 API 前缀及配置前缀（如 `/api`），并沿用应用已有 middleware、CORS 与适用的 HTTP observability 包装。
- 新入口应内置于 AgentkitServerApp，用户无需通过 WithRouteSetup 自行实现删除功能；既有自定义路由回调能力保留。

### API Design

```http
DELETE {ApiPathPrefix}/apps/{app_name}/users/{user_id}/sessions/{session_id}/artifacts/{artifact_name}/versions/{version}
```

| 参数 | 位置 / 类型 | 必填 | 规则 |
|---|---|---|---|
| app_name | Path / string | 是 | 非空，写入 AppName |
| user_id | Path / string | 是 | 非空，写入 UserID |
| session_id | Path / string | 是 | 非空，写入 SessionID |
| artifact_name | Path / string | 是 | 非空，写入 FileName；沿用 ADK 的文件名/命名空间约束 |
| version | Path / int64 | 是 | 十进制正整数，取值 1 到 9223372036854775807；0 和负数返回 400 |

- Request body：无。成功 HTTP 200；采用旧 controller 的 `EncodeJSONResponse(nil, 200, writer)` 等价行为，以实测空 body 为准，不改为 204。
- 400：参数缺失、非法整数/溢出、零与负数。500：底层失败或未配置服务。错误格式沿用现有 text/plain HTTP 错误响应。
- 认证与权限：沿用宿主应用已有 middleware；不新增独立鉴权体系或控制台 API。OPTIONS 使用现有 CORS 链，不能触发 Delete。

```bash
curl -i -X DELETE 'http://127.0.0.1:18080/api/apps/artifact_probe/users/u1/sessions/s1/artifacts/report.txt/versions/2'
```

### Backend / NFR 与实现边界

- 推荐在 VeADK 内新增一个精确 route 和 handler，在官方 ADK fallback 之前挂载；只使用官方公开类型/方法，不访问 ADK internal，不复制整个 Artifact controller/router。
- 继续复用现有 ArtifactService，不新增存储服务、迁移、版本重排或请求字段。
- 不降级 ADK 或其他依赖，不通过修改 module cache、vendor 或 replace fork 交付实现。若未来正式 ADK 已补齐能力，评估复用并移除重复路径，但本次不依赖未发布能力。
- 本次不对 Python/Java 做同步，也不回移 release/v1。Go V1 的支持需独立需求和版本调研。

## 4. 验收标准与验证要求

| 用例 | 验收证据 / 通过标准 |
|---|---|
| 参数映射 | spy 服务验证五字段和值完整准确、Version 为 2、只调用一次、使用请求 Context |
| 中间版本删除 | 实际服务保存 1/2/3；HTTP 删除 2；版本列表为 1/3；读取内容保持；删除 2 再次成功 |
| 边界版本 | 删除第一版和最新版本；剩余编号/内容保持；删最新后无 version 的 Load 返回剩余最新版本 |
| 最后一版/不存在 | 删除最后一版后 list 不含文件；重复删除、不存在文件或版本遵循 Service 幂等成功 |
| 非法版本 | abc、1.5、空白、int64 溢出、0、-1 均 400；spy 证明底层 Delete 零次调用；+1/01 映射版本 1 |
| 缺少参数 | handler 逐项缺少/为空五字段返回 400；实际 URL 缺段不误删，记录 router 原有状态/重定向 |
| 底层失败/未配置 | error 服务返回 500；nil 服务返回明确 500；不 panic、不返回成功 |
| 隔离 | 同名跨 app/user/session、不同文件保持；user: 命名空间遵循官方语义 |
| API 回归 | list、默认 Load、query version Load、版本 GET、全量 DELETE 均保持原行为；旧 DELETE 不变为按 query version 删除 |
| 路由集成 | 空前缀、/api 均通过；方法 GET/DELETE 不相互遮蔽；middleware 被调用；启用 CORS 的 OPTIONS 不删除制品 |

- **必须执行 E2E：**启动实际 `AgentkitServerApp.Run`，独立测试进程通过真实 localhost TCP/HTTP 请求接口，以官方 InMemoryService 为真实制品服务；通过服务输出和 API 结果交叉检查，不用 mock handler 替代端到端验证。
- 单测：handler 参数/失败映射采用 spy/fake 服务，执行 race；集成测试验证真实 router 及服务状态。验证应用代码与测试代码分开，临时复现程序放 `/tmp`，只在 PRD 附录保留，不提交 `.go`/`.py` 验证文件。
- 环境：main 的 Go/ADK 锁定版本；本轮不调用 Ark、TOS 或 AgentKit 控制台：REST 版本删除不依赖模型或云资源，仓库未提供云 Artifact backend，本地官方服务足以验证这条链路。无控制台 AgentID；本地 app=`artifact_probe`，user=`u1`，session=`s1`。
- 下游实现需执行仓库规范：`gofmt -w <changed-go-files>`、`go mod tidy -diff`、`go build -mod=readonly ./...`、`go test -mod=readonly -race -count=1 -shuffle=on ./...` 和 CI golangci-lint。本节点仅新增 Markdown，无运行时代码变化，不将全库测试宣称为已执行；附录验证应用编译和 HTTP 测试为本轮证据。
- 失败时记录 commit、依赖、请求、返回状态、剩余版本/内容和调用次数；不满足版本隔离、零值防误删或回归检查，不能验收通过。

## 5. SDK 与同类框架调研

| 来源 | 已有能力 / 限制 | 采用结论 |
|---|---|---|
| 官方 ADK Go v2.1.0 `artifact/service.go`、`artifact/inmemory.go` | DeleteRequest.Version 为 int64；0 全量删、非0单版；不存在删除成功 | 使用现成 Service，不再实现版本存储和删除算法 |
| 官方 ADK Go v2.1.0 `server/adkrest/controllers/artifacts.go`、`internal/routers/artifacts.go` | GET 指定版本；DELETE 仅全量；错误为 500，成功 200 空响应 | 增加最小 REST 扩展，沿用现有错误及成功格式 |
| 官方 ADK Go main controller（2026-11-20 读取） | 读到服务不可用/授权/错误处理变化，全量 Delete 仍未传版本 | 锁定依赖为实施基线；不把 upstream main 新语义混入本次 |
| 同类实现：ADK Python main InMemoryArtifactService（2026-11-20 读取） | delete_artifact 无 version 参数；list_versions 使用 0 起始编号 | 不能把 Python 的 0 版本惯例套用到 Go；仅参考生命周期，不做跨语言复制 |
| genai v1.63.0 | Part 为制品内容类型，删除无模型请求 | 无新增模型 SDK 调用需求 |

调研来源：

- [VeADK main app](https://github.com/volcengine/veadk-go/blob/8034d8e9680e6ce87500bc1370aadf80deb3645f/apps/agentkit_server_app/app.go)
- [官方 ADK Go Artifact 包](https://pkg.go.dev/google.golang.org/adk/v2@v2.1.0/artifact)
- [官方 ADK Go REST controller main](https://github.com/google/adk-go/blob/main/server/adkrest/controllers/artifacts.go)
- [官方 ADK Python 同类实现](https://github.com/google/adk-python/blob/main/src/google/adk/artifacts/in_memory_artifact_service.py)

## 6. 决策记录、待确认问题与交付状态

- **决策 D-001（已确认）：**用户选择“仅允许正整数 int64；0 和负数返回 400”。本次直接落实到 REQ-002、API 参数表及非法版本验收场景。
- 决策依据：官方 ADK 自动版本从 1 起始，`DeleteRequest.Version=0` 表示全量删除；拒绝零值可满足“仅删除指定版本”的隔离要求。
- **待确认问题：无。**无需改造底层服务、升级或降级依赖。
- **当前状态：**需求澄清与 Spec 设计完成，可以进入开发与自测。官方 SDK/同类实现调研、主分支端到端能力缺口验证及可执行附录已齐备。
- 本轮交付：PRD（包含临时应用、独立验证脚本、命令与实测结果）；没有实现新 REST route/handler，新接口功能测试和全库回归由下游开发完成。
- 风险与阻塞：无未解决需求决策或外部服务阻塞；必须按第 4 节证明 0/负数不会触发删除，不能将基线验证当作新功能验收或已发布证明。

## 7. 附录：调研 / 验证代码

### 7.1 环境与命令

```text
OS: linux/amd64
调研与验证日期: 2026-11-20 UTC
基线 commit: 8034d8e9680e6ce87500bc1370aadf80deb3645f
工作分支: feat/9e0bff2c-q1yf
Go: 1.26.5；ADK: google.golang.org/adk/v2 v2.1.0
服务: VeADK AgentkitServerApp + ADK InMemoryService
地址: 127.0.0.1:18080，API 前缀 /api
标识: artifact_probe / u1 / s1 / report.txt
```

从 workspace 根目录执行下列命令，临时 module 保留仓库依赖版本，用本地 replace 指向最新 main checkout；replace 仅用于验证应用，不更改产品 go.mod。

```bash
agentnative workflow repositories status --available --json
git -C repos/volcengine_veadk-go fetch origin main
git -C repos/volcengine_veadk-go log -1 --format='%H %cI %s' origin/main
mkdir -p /tmp/veadk-artifact-validation
cp repos/volcengine_veadk-go/go.mod /tmp/veadk-artifact-validation/go.mod
cp repos/volcengine_veadk-go/go.sum /tmp/veadk-artifact-validation/go.sum
export VEADK_VALIDATION_REPO="$(pwd)/repos/volcengine_veadk-go"
python3 - <<'PY'
import os
from pathlib import Path
p = Path('/tmp/veadk-artifact-validation/go.mod')
s = p.read_text().replace('module github.com/volcengine/veadk-go/v2', 'module artifact-validation')
s += '\nrequire github.com/volcengine/veadk-go/v2 v2.0.0\n'
s += 'replace github.com/volcengine/veadk-go/v2 => ' + os.environ['VEADK_VALIDATION_REPO'] + '\n'
p.write_text(s)
PY
cd /tmp/veadk-artifact-validation
# 将 7.2、7.3 代码分别保存为 main.go 和 probe.py
gofmt -w main.go
go build -mod=mod -o artifact-server main.go
# 终端 A 启动服务
./artifact-server
# 终端 B 切换到同一 /tmp 目录后执行
python3 probe.py
# 完成后终端 A Ctrl-C，SDK/HTTP 结果保留在 sdk-evidence.json、results.json
```

### 7.2 用户应用 main.go

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/volcengine/veadk-go/v2/apps"
	"github.com/volcengine/veadk-go/v2/apps/agentkit_server_app"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
	"io/fs"
	"os"
	"os/signal"
	"syscall"
)

func check(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	svc := artifact.InMemoryService()
	save := func(name string) {
		for i := 1; i <= 3; i++ {
			_, err := svc.Save(ctx, &artifact.SaveRequest{AppName: "artifact_probe", UserID: "u1", SessionID: "s1", FileName: name, Part: genai.NewPartFromText(fmt.Sprintf("content-%d", i))})
			check(err)
		}
	}
	versions := func(name string) []int64 {
		r, e := svc.Versions(ctx, &artifact.VersionsRequest{AppName: "artifact_probe", UserID: "u1", SessionID: "s1", FileName: name})
		if errors.Is(e, fs.ErrNotExist) {
			return []int64{}
		}
		check(e)
		return r.Versions
	}
	save("report.txt")
	save("sdk.txt")
	before := versions("sdk.txt")
	check(svc.Delete(ctx, &artifact.DeleteRequest{AppName: "artifact_probe", UserID: "u1", SessionID: "s1", FileName: "sdk.txt", Version: 2}))
	after := versions("sdk.txt")
	check(svc.Delete(ctx, &artifact.DeleteRequest{AppName: "artifact_probe", UserID: "u1", SessionID: "s1", FileName: "sdk.txt", Version: 0}))
	f, e := os.Create("sdk-evidence.json")
	check(e)
	check(json.NewEncoder(f).Encode(map[string]any{"before": before, "after_delete_2": after, "after_delete_0": versions("sdk.txt")}))
	check(f.Close())
	root, e := agent.New(agent.Config{Name: "artifact_probe"})
	check(e)
	cfg := apps.DefaultApiConfig().SetHost("127.0.0.1").SetPort(18080).SetApiPathPrefix("/api").SetWebUIEnabled(false)
	app := agentkit_server_app.NewAgentkitServerApp(cfg)
	check(app.Run(ctx, &apps.RunConfig{AgentLoader: agent.NewSingleLoader(root), SessionService: session.InMemoryService(), ArtifactService: svc, DisableObservability: true}))
}
```

### 7.3 独立验证脚本 probe.py

```python
import json
import time
import urllib.request
import urllib.error

base = 'http://127.0.0.1:18080/api/apps/artifact_probe/users/u1/sessions/s1/artifacts'

def call(method, path):
    try:
        with urllib.request.urlopen(urllib.request.Request(base + path, method=method), timeout=3) as r:
            return r.status, r.read().decode()
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode()

for _ in range(100):
    try:
        status, body = call('GET', '')
        break
    except urllib.error.URLError:
        time.sleep(0.1)
else:
    raise RuntimeError('server not ready')

results = []
def verify(name, method, path, expected, content=None):
    status, body = call(method, path)
    assert status == expected, (name, status, body)
    if content is not None:
        assert json.loads(body) == content, (name, body)
    results.append({'case': name, 'status': status, 'body': body.strip()})

verify('list-before', 'GET', '', 200, ['report.txt'])
verify('load-version-2', 'GET', '/report.txt/versions/2', 200, {'text': 'content-2'})
verify('delete-version-route-missing', 'DELETE', '/report.txt/versions/2', 404)
verify('version-1-retained', 'GET', '/report.txt/versions/1', 200, {'text': 'content-1'})
verify('version-2-still-present', 'GET', '/report.txt/versions/2', 200, {'text': 'content-2'})
verify('version-3-retained', 'GET', '/report.txt/versions/3', 200, {'text': 'content-3'})
verify('invalid-version-route-missing', 'DELETE', '/report.txt/versions/abc', 404)
verify('missing-version-route', 'DELETE', '/report.txt/versions/', 404)
verify('old-delete-all', 'DELETE', '/report.txt', 200)
verify('list-after-delete-all', 'GET', '', 200, [])
evidence = json.load(open('sdk-evidence.json'))
assert sorted(evidence['before']) == [1, 2, 3]
assert sorted(evidence['after_delete_2']) == [1, 3]
assert not evidence['after_delete_0']
json.dump({'sdk': evidence, 'http': results}, open('results.json', 'w'), ensure_ascii=False, indent=2)
print(json.dumps({'sdk': evidence, 'http': results}, ensure_ascii=False, indent=2))
```

### 7.4 实际结果与结论

```json
{
  "sdk": {
    "after_delete_0": [],
    "after_delete_2": [
      3,
      1
    ],
    "before": [
      3,
      2,
      1
    ]
  },
  "http": [
    {
      "case": "list-before",
      "status": 200,
      "body": "[\"report.txt\"]"
    },
    {
      "case": "load-version-2",
      "status": 200,
      "body": "{\"text\":\"content-2\"}"
    },
    {
      "case": "delete-version-route-missing",
      "status": 404,
      "body": "404 page not found"
    },
    {
      "case": "version-1-retained",
      "status": 200,
      "body": "{\"text\":\"content-1\"}"
    },
    {
      "case": "version-2-still-present",
      "status": 200,
      "body": "{\"text\":\"content-2\"}"
    },
    {
      "case": "version-3-retained",
      "status": 200,
      "body": "{\"text\":\"content-3\"}"
    },
    {
      "case": "invalid-version-route-missing",
      "status": 404,
      "body": "404 page not found"
    },
    {
      "case": "missing-version-route",
      "status": 404,
      "body": "404 page not found"
    },
    {
      "case": "old-delete-all",
      "status": 200,
      "body": ""
    },
    {
      "case": "list-after-delete-all",
      "status": 200,
      "body": "[]"
    }
  ]
}
```

- **验证应用编译成功，最终 probe.py 退出码 0，10 个 HTTP 检查及 SDK 状态断言通过。**这些是基线检查通过，确认需求缺口存在；新增接口的验收仍需开发后执行。
- 指定版本 DELETE 返回 404，后续 GET 证明版本 2 未删除，版本 1/3 也保留；非法版本 DELETE 也为 404，新功能应改为校验后的 400。
- SDK 枚举按当前服务返回 `[3,2,1]`，删 2 后为 `[3,1]`；断言比较排序后的版本集合，REST 本次不定义版本枚举顺序。删 0 后 `Versions` 原生返回 `fs.ErrNotExist`，临时应用将该“无剩余版本”状态归一化为空数组写入证据。
- 预期新行为：版本 DELETE 2 为 200，GET 2 因不存在沿用当前 Load 的错误响应，GET 1/3 保留原内容；旧全量 DELETE 继续 200 空 body。
- 初次临时程序把删光后的 Versions 当作成功列表，产生 `artifact not found`；已按官方服务处理 ErrNotExist。探测脚本初始预估缺 DELETE 方法为 405，实际宿主 fallback 为 404，已修正为实测状态；初始版本断言假设升序，已改为集合比较。重新启动全新服务后执行最终脚本，结果如上，未改动任何产品代码。
- 启动时 sonic 发出当前 Go 版本不支持加速解码的提示并回退 encoding/json，HTTP 验证正常完成；依赖保持原版本。

### 7.5 官方实现调研命令

```bash
cd repos/volcengine_veadk-go
go mod download google.golang.org/adk/v2
VEADK_MODCACHE="$(go env GOMODCACHE)"
rg -n 'Version|func .*Delete' "$VEADK_MODCACHE/google.golang.org/adk/v2@v2.1.0/artifact/service.go" "$VEADK_MODCACHE/google.golang.org/adk/v2@v2.1.0/artifact/inmemory.go"
cat "$VEADK_MODCACHE/google.golang.org/adk/v2@v2.1.0/server/adkrest/internal/routers/artifacts.go"
cat "$VEADK_MODCACHE/google.golang.org/adk/v2@v2.1.0/server/adkrest/controllers/artifacts.go"
curl -fsSL https://raw.githubusercontent.com/google/adk-go/main/server/adkrest/controllers/artifacts.go -o /tmp/veadk-go-artifact-reference.go
curl -fsSL https://raw.githubusercontent.com/google/adk-python/main/src/google/adk/artifacts/in_memory_artifact_service.py -o /tmp/veadk-python-artifact-reference.py
rg -n -A40 'async def delete|func .*Delete' /tmp/veadk-python-artifact-reference.py /tmp/veadk-go-artifact-reference.go
```

读取结果：锁定 ADK Go 的路由只有 ListArtifacts、LoadArtifact、LoadArtifactVersion、DeleteArtifact；DeleteArtifactHandler 构造请求时未写 Version；InMemory Delete 的 `version != 0` 分支删除单版，否则 DeleteRange 全量删。读取的 Python 同类实现 delete_artifact 不带版本参数，list_versions 为 `range(len(versions))`。远程 main 文件属于读取时快照，不视为产品已使用的发布依赖。
