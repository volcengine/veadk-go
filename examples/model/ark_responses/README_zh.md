# Ark Responses

通过 `model.NewArkResponsesModel(ctx, modelName, &model.ArkClientConfig{...})`
创建模型，再传给 Agent 已有的 `Model` 字段即可。不会新增 provider 值或 Agent
配置字段，不改变现有 `NewArkModel`、`NewOpenAIModel` 以及默认调用方式。

## 支持范围

- 用户消息中的图片、视频、文档：`InlineData`、HTTP(S) URL、Data URI。
- 方舟 Files API：`FileData.FileURI = "file_id://file-xxx"`，按 MIME 转换为
  `input_image`、`input_video`、`input_file` 的 `file_id` 字段。
- `FunctionResponse.Parts` 中的图片及其他附件，同样支持上述表示方式。
- 普通/流式输出、函数调用、系统指令、生成参数、结构化输出及用量信息。
- 复用 `ArkClientConfig`：API Key、AK/SK、动态 Token、HTTPClient 和额外请求头。

模型层与 AIO、local、Skills 无关。本地文件由调用方读取，沙箱文件由工具通过
`/v1/file/download` 下载，然后生成 `InlineData`；也可以先上传 Files API。
这个实现不会读取 `file://` 或裸文件路径，不会把用户输入当作 runtime 文件路径。
具体文件类型、大小限制和视觉能力取决于选用的方舟模型。内联文档会使用已有的
`DisplayName` 作为 filename；未提供时 PDF 使用 `attachment.pdf`，满足 Ark 所需元信息。

## 工具返回图片

为兼容 Ark，工具的 JSON 结果保持为 `function_call_output.output` 字符串；附件
紧邻该结果，以带工具名和 call_id 说明的图片/文件消息传给模型，不把图片编码成
JSON 文本。多个工具结果各自保持附件关联。

ADK Go 的普通 functiontool 只返回 JSON map。示例中的 `image_tool.go` 使用已有
`OnEventCallback`，在保存工具事件之前填充原生 `FunctionResponse.Parts`，让图片
同时进入模型输入和会话历史。待附加图片按 invocation/call ID 隔离，并在调用结束
后清理。这是应用自定义工具示例，不会自动安装新内置工具，也不修改 ADK。

```sh
# 设置 MODEL_AGENT_NAME、MODEL_AGENT_API_KEY，可选 MODEL_AGENT_API_BASE。
go run ./examples/model/ark_responses -image ./picture.png
# 由模型实际调用工具读取图片，然后继续回答。
go run ./examples/model/ark_responses -image ./picture.png -tool
# 使用真实 Files API 文件 ID。
go run ./examples/model/ark_responses -uri file_id://file-xxx -tool
```

历史由 ADK 管理，使用 `store=false` 和完整历史回放。本期不实现 Python 的
provider 端会话缓存/previous_response_id 链，也不增加每请求 modelName/thinkingLevel。
模型响应新增未知字段不会导致解析失败，不需要更改 SDK 全局设置或升级依赖。

## 验收

普通测试覆盖协议转换、Token 刷新、流式异常，以及真实 ADK Runner 的工具图片
保存和跨轮回放。真实验收需显式开启：

```sh
VEADK_ARK_RESPONSES_LIVE=1 \
VEADK_LIVE_ENV_FILE=/绝对路径/private.env \
go test -v -count=1 -timeout=18m ./examples/model/ark_responses \
  -run '^TestArkResponsesLive$'
```

需要模型配置、`AGENTKIT_TOOL_ID` 和 AgentKit 凭据。测试创建真实沙箱、上传图片到
Files API，验证 24 种组合：无 Skills/local Skills/AIO × 用户消息/工具返回 ×
InlineData/file_id × 普通/流式。检查模型识图结果、附件持久化和沙箱下载字节，
结束后删除远端测试资源。URL、视频和文档转换由单元测试覆盖；该真实矩阵专门验收图片。
