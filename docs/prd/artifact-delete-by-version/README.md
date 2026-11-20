# PRD：VeADK Go 支持按版本删除制品

## Review Summary

- **需求来源：开发者功能特性**；Meego story `7397937083`，优先级 P1。
- **结论：能力缺口已在最新 main 通过真实 HTTP 复现。复用 ADK Artifact Service，仅补齐 VeADK REST 入口。**
- 变更面：AgentKitServerApp、Artifact REST route/handler、接口文档和测试；依据为服务装配代码及 ADK v2.1.0 官方实现。
- 差异判断：本次适用于 Go V2/main；V1/release/v1 不自动回移。本次不是 GitHub MR 同步需求，不要求 Python、Java 跟进。
- Review 重点：`Version=0` 是 ADK 的全量删除哨兵；新接口必须拒绝 0，避免扩大删除范围。缺失路径段与 handler 参数缺失需分层验收。

## Scope Note

展开用户问题、REST 契约、服务复用、兼容性、验收及验证附录。不涉及 UI、模型推理、数据库迁移、新存储适配或新观测指标。

## 1. 需求来源与目标

| 项目 | 内容 |
| --- | --- |
| 来源 | 开发者功能特性；需求负责人：shanchunhua |
| 目标仓库 | volcengine/veadk-go；模块 apps/agentkit_server_app |
| 交付分支 | feat/c777f456-qgzv；基线 main |
| 调研基线 | 8034d8e9680e6ce87500bc1370aadf80deb3645f；2026-11-20 已 fetch，HEAD 与 origin/main 一致 |
| 共享上下文 | repositories status 返回 context_version=5，resolved，base_branch=main；采用用户更新后的仓库集合 |
| 运行依赖 | Go 1.26.5；google.golang.org/adk/v2 v2.1.0；genai v1.63.0；gorilla/mux v1.8.1 |
| 影响能力 | Artifact、Server；属于 VeADK 封装原生 ADK 的核心能力 |

开发者需要清理会话制品的一次历史修订，同时保留其他修订继续使用。现有 REST 入口只允许全量删除，客户端无法安全完成这一任务。

**目标：调用新接口后仅指定版本消失，其余版本内容与作用域保持正确；现有 list/load/delete 契约保持兼容。**

## 2. 调研现状与验证结论

| 调研对象 | 当前行为与证据 | 对需求的影响 |
| --- | --- | --- |
| VeADK AgentKitServerApp | app.go 的 SetupRouters 创建官方 adkrest.NewServer，并以 StripPrefix 挂载 REST fallback；没有自有 Artifact handler | 新增精确路由应在 fallback 前注册，使用 RunConfig.ArtifactService |
| ADK REST router/controller | 只提供 GET 单版本和 DELETE 全量；DeleteArtifactHandler 不设置 Version | 新 URL 当前返回 404，不能只修改现有全量删除 handler |
| 官方 Artifact Service | DeleteRequest.Version 为 int64；InMemory/GCS 的 0 表示全量，非 0 表示指定版本；InMemory 自动版本从 1 开始 | 复用 Service.Delete；新接口只接受正整数，不能把 0 透传 |
| 现有错误响应 | 官方全量删除成功为 200、空 body；服务错误为 500、text/plain；缺失版本 load 返回 500 | 新删除接口沿用该错误模型，不另造全局错误映射 |

**验证结论：确认是新功能缺口，不作为已有“按版本删除”接口的 Bug 修复。** 已通过未修改的最新主分支启动 AgentKitServerApp，分别测试默认路径与 `/api` 前缀。

- 底层 Delete(Version=2) 后，direct.txt 的 Versions 返回 `[1]`。
- REST DELETE report.txt/versions/2 返回 404；随后版本 1、2 均返回 200 且内容不变。
- 非法版本和缺失路径段在当前基线返回 404，原因是删除路由不存在，不代表 handler 已实现校验。
- 原全量 DELETE 返回 200、空 body；记录的 DeleteRequest.Version=0；随后两个版本的 GET 均返回 500。
- 删除 u1 的 report.txt 后，u2 的同名制品仍可读取；list 中保留独立的 direct.txt。

代码与执行过程见附录。当前结论只证明基线缺口及官方底层能力，**不代表新接口已开发或验收通过**。

## 3. 需求范围与行为

### 3.1 REQ-001：仅删除指定版本

User Story：作为调用 VeADK Go REST API 的开发者，我希望删除某个历史版本，以保留该制品其他可用修订。Priority：P1。

EARS：

- 当客户端提交完整作用域和合法版本时，VeADK shall 调用配置中的 Artifact Service.Delete，正确传入 AppName、UserID、SessionID、FileName、Version 和请求 context。
- 删除成功时，VeADK shall 返回 200、空 body；仅该版本被删除，其他版本内容不变。
- 新接口 shall 默认可用，无需使用 WithRouteSetup 手动注册；支持空前缀及 SetApiPathPrefix 配置。

```gherkin
Scenario: 删除历史版本并保留最新版本
  Given 同一制品含版本 1 和 2
  When DELETE 该制品 /versions/1
  Then 响应为 200 且 body 为空
  And 版本 1 无法再读取
  And 版本 2 仍可读取且内容不变
```

### 3.2 REQ-002：参数校验与异常

- app_name、user_id、session_id、artifact_name、version 均为必填。handler 接收到缺失或空参数时 shall 返回 400，且不调用 Delete。
- version 使用十进制 int64 整数解析规则；需 `1 <= version <= 9223372036854775807`。可采用 `strconv.ParseInt(raw, 10, 64)`，再校验大于 0；允许该解析器接受的前导 `+` 与前导零。客户端应优先使用 Save 返回的版本号。
- 字母、小数、空白、溢出、0、负数 shall 返回 400，不能回落为全量删除。`version parameter must be a positive integer` 可作为统一错误文本。
- 路由不得用整数正则过滤 `{version}`，否则非法输入会得到路由 404，无法满足 400 契约。
- 对 URL 缺少必需路径段而无法匹配 route 的请求，允许标准 404（或路由规范化重定向）；不得调用 Delete。handler 的空参数单测独立验证 400。不存在 version 的 `/versions/` 不等同于现有全量删除路径。
- 服务返回 nil 时 shall 返回 200；服务返回错误时 shall 返回 500、text/plain 错误响应，遵循现有 Artifact 删除错误模型，不返回成功。
- 已不存在的正整数版本：不预加载或自行判定不存在；按配置服务的 Delete 结果响应。InMemory 返回 nil 时为 200，真实后端返回错误时按 500 响应。
- ArtifactService 未配置时，新入口 shall 返回 500 且不发生 panic；保留现有非 Artifact 入口可启动的行为。

```gherkin
Scenario: 拒绝全量删除哨兵
  Given 同一制品含多个版本
  When DELETE 该制品 /versions/0
  Then 响应为 400
  And Artifact Service.Delete 未被调用
  And 所有版本仍可读取
```

### 3.3 REQ-003：兼容性与范围控制

- 现有全量 DELETE 路径及行为不变，继续使用 Version=0；不支持通过该路径的 query version 改变删除语义。
- GET list、GET load（含 query version）、GET 单版本保持官方 ADK 当前实现和响应格式；A2A、WebUI、自定义路由及中间件保持现有装配行为。
- 新接口遵循现有服务入口和中间件约定，作用域参数不等同于鉴权凭据，不新增独立身份或权限体系。
- 不新增制品格式、迁移或后台清理任务；不降级 ADK/其他依赖、不修改 module cache，不复制官方 Service 存储逻辑。

### 3.4 REST 契约与实现边界

```text
DELETE {ApiPathPrefix}/apps/{app_name}/users/{user_id}/sessions/{session_id}/artifacts/{artifact_name}/versions/{version}
```

请求无 body；完整键由五个路径参数构成。ApiPathPrefix 为空时与用户建议 URL 一致。

| 场景 | 新接口响应 | 是否调用底层 Delete |
| --- | --- | --- |
| 合法正整数，服务成功 | 200，空 body | 是，Version 精确等于请求版本 |
| version 解析失败、溢出、0 或负数 | 400，text/plain | 否 |
| handler 参数缺失 | 400，text/plain | 否 |
| 路由无法匹配 | 标准路由错误/规范化行为 | 否 |
| 服务失败或未配置 | 500，text/plain | 失败时已调用；未配置时未调用 |

建议变更位置：apps/agentkit_server_app/app.go 负责内置精确路由装配，同目录独立 Artifact handler 文件承载校验与服务调用。注册时保持正确 API 前缀，位于自定义路由及 ADK REST fallback 前；新 handler 仅负责本次新增 DELETE，其他动词及现有接口继续由官方 handler 处理。无需复制官方内部 router/controller 或 fork ADK。

## 4. 验收标准与验证要求

| 编号 | 必验场景 | 通过标准 |
| --- | --- | --- |
| AC-01 | 删除中间/历史版本 | 先保存 3 个版本，删 2；版本集合仅移除 2，1/3 的内容保持一致 |
| AC-02 | 删除当前最新版本 | 删 3；latest load 返回剩余最新版本 2，1/2 未被删除 |
| AC-03 | 删除最后一个版本 | 指定版本消失，list 不再含该制品；其他制品仍存在 |
| AC-04 | 参数传递 | 捕获请求五个字段与 context；Version 非 0 且值准确，只调用一次 Delete |
| AC-05 | 非法版本 | abc、1.5、空白、溢出、0、-1 为 400；Delete 调用数为 0；版本均未改变 |
| AC-06 | int64 边界与格式 | 正最大值成功传入（是否存在由服务决定）；+1、01 与整数解析一致；最大值加 1 为 400 |
| AC-07 | 缺失参数 | 每个必填参数缺失/空值的 handler 单测为 400；缺失 URL 段不触发任何删除 |
| AC-08 | 失败及重复删除 | 可控失败服务为 500；重复删除和不存在版本按服务结果响应；失败不被吞掉 |
| AC-09 | 隔离与兼容 | 不同 app/user/session 的同名制品不受影响；全量 delete、list、query/path load 回归通过 |
| AC-10 | 服务装配 | 默认开启；空前缀和 /api 均可访问；nil ArtifactService 返回 500；中间件照常生效 |

**验证层次必须分开：**

- handler 单测：使用可捕获请求的 Service 测试替身验证参数、调用次数、context、缺失字段、服务失败；测试替身不能替代数据语义验证。
- 路由集成：通过 AgentKitServerApp 实际装配测试合法/非法请求，防止 fallback 抢占；关注 GET 同路径不被新 DELETE 路由截获。
- **用户侧 E2E：** 独立启动 AgentKitServerApp，配置真实官方 InMemoryService，用户应用保存制品并将版本集合、删除请求写入文件；另一个客户端测试 HTTP 及输出文件。保存与断言代码分开。E2E 须证明“删一个、保留其他”，不能只断言 200。
- 回归命令：`gofmt -w <changed-go-files>`、`go test ./...`；按仓库 CI 执行 `go mod tidy -diff`、`go build -mod=readonly -v ./...`、`go test -mod=readonly -race -count=1 -shuffle=on ./...`。记录失败日志；不得通过降级依赖修复验证环境。
- 本节点仅交付 PRD，已执行未修改 main 的用户应用构建和 HTTP 基线验证；尚未执行新 handler 单测/新增功能 E2E，完整仓库测试与 lint 留给开发节点，文档变更以 diff 检查及文档回读验证。

**真实服务范围：** 本次执行了真实本地 HTTP 和真实 InMemoryService，未 mock 数据删除。未调用模型、GCS/TOS 或 AgentKit 控制台：该需求只补 REST 参数透传，模型推理与控制台部署不参与路径；本地验证无需凭据，云后端差异须由对应存储适配测试覆盖。未部署控制台 Agent，因此没有控制台 agentid。不得将本地结果描述成云后端验收通过。

## 5. 官方 SDK 与同类框架调研

| 对象 | 调研结论 | 采用方式 |
| --- | --- | --- |
| ADK Go v2.1.0 artifact/service.go、inmemory.go | DeleteRequest.Version int64；Versions 可核验版本集合；InMemory 已支持指定版本删除 | 直接调用配置 Service.Delete，用 Versions 验证数据结果 |
| ADK Go v2.1.0 gcsartifact/service.go | 非 0 时删除单个版本对象，0 时列举并删除全部；删除对象失败会返回 error | 不重写存储；云调用本次未执行，不宣称其 E2E 通过 |
| ADK Go 官方 adkrest | 已有同形态 GET /versions/{version}，缺少 DELETE 单版本；成功删除 200 空 body，服务错误 500 | 沿用路径结构和响应惯例，在 VeADK 装配层补入口 |
| Google genai v1.63.0 | 制品数据由 genai.Part 承载；删除版本由 ADK Service 负责 | 本次不新增 GenAI SDK API，不涉及模型调用 |
| 官方 ADK Python | 固定 commit c81876a49911745248f2b774c02770ed211ff460 的 BaseArtifactService.delete_artifact 不含 version 参数，只接受 app/user/filename/session | Python 抽象与 Go 不同，不直接复制 Python 删除实现、不推导 Python 必须同步 |

**调研决策：复用 Go 官方 ADK 已有 Service 能力，并补充产品入口及契约测试。** 需求明确指定 Go，最相关对照是 ADK Python；不为本次单接口改动展开 LangChain/LangGraph、Java 的无关能力调研。

证据来源：

- [VeADK 服务装配](https://github.com/volcengine/veadk-go/blob/8034d8e9680e6ce87500bc1370aadf80deb3645f/apps/agentkit_server_app/app.go)
- [VeADK 版本线约定](https://github.com/volcengine/veadk-go/blob/8034d8e9680e6ce87500bc1370aadf80deb3645f/CONTRIBUTING.md)
- 官方 Go 源码来自 `go mod download google.golang.org/adk/v2` 获取的 v2.1.0 校验模块；本地路径 `$GOMODCACHE/google.golang.org/adk/v2@v2.1.0/{artifact,server/adkrest}`。
- [官方 Python 删除服务抽象](https://github.com/google/adk-python/blob/c81876a49911745248f2b774c02770ed211ff460/src/google/adk/artifacts/base_artifact_service.py)
- [官方 Artifact 概念文档](https://google.github.io/adk-docs/artifacts/)
- [PRD 标准](https://bytedance.larkoffice.com/wiki/IzN5wcZxMi1yskkhQutchFhWnlh)

## 6. 待确认问题与交付状态

- **无阻塞性的产品待确认问题。** 正整数规则依据 ADK v2 的 0 哨兵及“只删一个版本”要求确定；Review 应检查该规则。
- 当前状态：需求规格已形成，基线缺口验证完成；新功能待开发与验收。本次交付不包含产品运行代码。
- 限制：自定义 ArtifactService 必须正确实现非 0 版本删除语义；不能仅凭接口类型保证第三方实现行为。若支持某真实存储后端，开发/测试节点需记录后端类型及验证结果。
- 交付：本需求目录下的 PRD 及验证附录、飞书需求目录中的 PRD、AgentNative 登记产物。文档与本需求内容集中存放，不将临时验证源码提交分支。

## 7. 附录：调研与可执行验证

### 7.1 环境与调研命令

验证日期：2026-11-20。Linux amd64，Go 1.26.5；main commit 与“需求来源与目标”一致。用户应用使用官方通用 Agent 作为 AgentKitServerApp 的 root，避免模型调用；直接注入真实 InMemoryService。应用标识 `artifact_probe`，user 为 `u1`/`u2`，session 为 `s1`；这些是本地测试标识，不是控制台 agentid。

在仓库根目录执行：

```bash
git fetch origin main
git rev-parse HEAD origin/main
go version
go mod download google.golang.org/adk/v2
rg -n 'DeleteRequest|Version|DeleteArtifact' "$(go env GOMODCACHE)/google.golang.org/adk/v2@v2.1.0/artifact" "$(go env GOMODCACHE)/google.golang.org/adk/v2@v2.1.0/server/adkrest"
git ls-remote https://github.com/google/adk-python.git refs/heads/main
curl -fsSL https://raw.githubusercontent.com/google/adk-python/c81876a49911745248f2b774c02770ed211ff460/src/google/adk/artifacts/base_artifact_service.py -o /tmp/python-artifact-service.py
rg -n -A 17 'async def delete_artifact' /tmp/python-artifact-service.py
```

实际结果：两个 Git SHA 均为 `8034d8e9680e6ce87500bc1370aadf80deb3645f`；`go version go1.26.5 linux/amd64`；官方 REST 只有 DeleteArtifactHandler，全量请求未赋 Version；Python 固定提交的 delete_artifact 不含 version。GitHub REST API 查询 commit 曾返回 403，改用只读 git ls-remote 获取 SHA 后下载固定提交源码完成调研。

### 7.2 用户应用代码（server.go）

临时文件放到 `/tmp/veadk-artifact-version-validation`，不加入分支。以下应用真实启动 AgentKitServerApp、保存两个版本、对独立制品执行底层单版本删除，并记录 REST Delete 请求；不在用户应用中做 HTTP 测试断言。

```go
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"sync"

	"github.com/volcengine/veadk-go/v2/apps"
	server "github.com/volcengine/veadk-go/v2/apps/agentkit_server_app"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type recordedService struct {
	artifact.Service
	mu sync.Mutex
}

func (s *recordedService) Delete(ctx context.Context, req *artifact.DeleteRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile("/tmp/veadk-artifact-version-validation/delete.jsonl", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(req); err != nil {
		return err
	}
	return s.Service.Delete(ctx, req)
}
func main() {
	ctx := context.Background()
	store := artifact.InMemoryService()
	seed := func(name, user string) {
		for _, text := range []string{"first", "second"} {
			_, err := store.Save(ctx, &artifact.SaveRequest{AppName: "artifact_probe", UserID: user, SessionID: "s1", FileName: name, Part: &genai.Part{Text: text}})
			if err != nil {
				log.Fatal(err)
			}
		}
	}
	seed("report.txt", "u1")
	seed("report.txt", "u2")
	seed("direct.txt", "u1")
	err := store.Delete(ctx, &artifact.DeleteRequest{AppName: "artifact_probe", UserID: "u1", SessionID: "s1", FileName: "direct.txt", Version: 2})
	versions, verErr := store.Versions(ctx, &artifact.VersionsRequest{AppName: "artifact_probe", UserID: "u1", SessionID: "s1", FileName: "direct.txt"})
	f, e := os.Create("/tmp/veadk-artifact-version-validation/baseline.json")
	if e != nil {
		log.Fatal(e)
	}
	json.NewEncoder(f).Encode(map[string]any{"delete_ok": err == nil, "versions_ok": verErr == nil, "remaining": versions.Versions})
	f.Close()
	root, err := agent.New(agent.Config{Name: "artifact_probe"})
	if err != nil {
		log.Fatal(err)
	}
	prefix := ""
	if len(os.Args) > 1 {
		prefix = os.Args[1]
	}
	port := 18083
	if len(os.Args) > 2 {
		port, err = strconv.Atoi(os.Args[2])
		if err != nil {
			log.Fatal(err)
		}
	}
	api := apps.DefaultApiConfig().SetHost("127.0.0.1").SetPort(port).SetApiPathPrefix(prefix).SetSimpleAPIEnabled(false).SetWebUIEnabled(false)
	app := server.NewAgentkitServerApp(api)
	err = app.Run(ctx, &apps.RunConfig{AgentLoader: agent.NewSingleLoader(root), SessionService: session.InMemoryService(), ArtifactService: &recordedService{Service: store}, DisableObservability: true})
	if err != nil {
		log.Fatal(err)
	}
}
```

### 7.3 独立客户端验证代码（client.py）

这是**基线复现脚本**：404 是当前基线的预期证据；实现新接口后应另写验收脚本，按 AC-01～AC-10 断言 200/400 及版本集合变化，不能继续把 404 当作功能验收通过。

```python
import json, sys, urllib.request, urllib.error
base = sys.argv[1]
p = '/apps/artifact_probe/users/u1/sessions/s1/artifacts/report.txt'
records=[]
def call(method,path):
    try:
        with urllib.request.urlopen(urllib.request.Request(base+path,method=method),timeout=5) as r:
            code,body=r.status,r.read().decode()
    except urllib.error.HTTPError as e:
        code,body=e.code,e.read().decode()
    records.append({'method':method,'path':path,'status':code,'body':body})
    return code,body
assert json.load(open('/tmp/veadk-artifact-version-validation/baseline.json')) == {'delete_ok':True,'versions_ok':True,'remaining':[1]}
assert call('GET',p+'/versions/1') == (200,'{"text":"first"}\n')
assert call('GET',p+'/versions/2') == (200,'{"text":"second"}\n')
assert call('DELETE',p+'/versions/2')[0] == 404
assert call('GET',p+'/versions/1')[0] == 200
assert call('GET',p+'/versions/2')[0] == 200
for v in ['abc','1.5','9223372036854775808','0','-1']:
    call('DELETE',p+'/versions/'+v)
call('DELETE',p+'/versions/')
call('DELETE','/apps/artifact_probe/users/u1/sessions/s1/artifacts/')
assert call('GET',p.rsplit('/',1)[0])[0] == 200
assert call('DELETE',p)[0] == 200
assert call('GET',p.rsplit('/',1)[0]) == (200,'["direct.txt"]\n')
assert call('GET',p+'/versions/1')[0] == 500
assert call('GET',p+'/versions/2')[0] == 500
assert call('GET',p.replace('/u1/','/u2/')+'/versions/1')[0] == 200
with open('/tmp/veadk-artifact-version-validation/http-'+(sys.argv[2])+'.json','w') as f: json.dump(records,f,ensure_ascii=False,indent=2)
print(json.dumps(records,ensure_ascii=False,indent=2))
print('BASELINE PASS: direct version delete preserves 1; REST version DELETE missing; legacy delete and user isolation checked')
```

### 7.4 执行方式与实际结果

把上面两个代码块分别存为 server.go、client.py 后，在 VeADK 仓库根目录构建。终端 A 启动服务，终端 B 测试；每次测试必须重启用户服务重新播种，避免前次全量删除改变数据。

```bash
mkdir -p /tmp/veadk-artifact-version-validation
gofmt -w /tmp/veadk-artifact-version-validation/server.go
go build -mod=readonly -o /tmp/veadk-artifact-version-validation/server /tmp/veadk-artifact-version-validation/server.go
# 终端 A：带前缀验证
/tmp/veadk-artifact-version-validation/server /api 18083
# 终端 B
python /tmp/veadk-artifact-version-validation/client.py http://127.0.0.1:18083/api api
# 停止终端 A 服务；另一次独立验证使用以下命令
/tmp/veadk-artifact-version-validation/server '' 18085
# 终端 B：无前缀验证
python /tmp/veadk-artifact-version-validation/client.py http://127.0.0.1:18085 root
```

实际使用的最终端口为 18083、18085；临时 18084 曾被旧验证进程占用，最终无前缀验证改在独立端口完成。应用启动时 sonic 提示 Go 版本未匹配其加速解码范围，回退 encoding/json；未降级依赖，HTTP 验证通过。

用户应用输出文件：baseline.json（底层验证）、delete.jsonl（Delete 请求）。客户端输出文件：http-api.json、http-root.json（完整 HTTP 记录）。两种前缀各执行 18 次 HTTP 请求，最终脚本退出码均为 0。

```json
{"delete_ok": true, "versions_ok": true, "remaining": [1]}
```

全量 REST 删除对应请求（从 delete.jsonl 取单条）：

```json
{"AppName":"artifact_probe","UserID":"u1","SessionID":"s1","FileName":"report.txt","Version":0}
```

| 观察步骤 | HTTP 状态 | 实际内容或状态 |
| --- | --- | --- |
| GET report.txt/versions/1、2 | 200、200 | first、second |
| DELETE report.txt/versions/2 | 404 | 404 page not found |
| 再 GET 版本 1、2 | 200、200 | 两个版本内容不变 |
| DELETE 版本 abc/1.5/溢出/0/-1 | 全为 404 | 目前不存在对应删除 route |
| DELETE 缺少版本段/文件名段 | 404 | 不触发 Delete |
| GET artifacts | 200 | ["direct.txt","report.txt"] |
| 旧 DELETE report.txt | 200 | 空 body；底层 Version=0 |
| 再 GET artifacts | 200 | ["direct.txt"] |
| 再 GET report.txt/versions/1、2 | 500、500 | artifact not found: file does not exist |
| GET u2 的 report.txt/versions/1 | 200 | first |

```text
BASELINE PASS: direct version delete preserves 1; REST version DELETE missing; legacy delete and user isolation checked
```

### 7.5 开发后 E2E 扩展步骤

1. 用户应用增加第三个版本，将每次 Save 返回的版本和内容记录到文件；分别为 app/user/session 的不同作用域播种同名制品。
2. 客户端读播种结果，用真实返回版本构造新 DELETE；核验 200 空 body、应用记录的非 0 Version、被删版本无法读取及其他版本内容不变。
3. 用户应用可在 Delete 后调用真实 Service.Versions 并记录剩余集合；客户端从 HTTP 与输出文件分别验结果，删除全部后记录“无版本”的服务结果。
4. 每个破坏数据的用例独立播种：历史/中间/最新/最后版本、重复删除、全量删除；非法参数组执行后读取原全部版本，保证无误删。
5. handler 单测单独构造空 mux 参数及可控错误 Service，覆盖无法由完整路由构造的缺失参数和失败分支；不得将这些替身结果冒充真实后端 E2E。

**完成标准：** 新 route/handler 已默认装配；AC-01～AC-10 全部有可复核结果，既有 Artifact API 回归通过，临时验证源码不提交到产品分支。
