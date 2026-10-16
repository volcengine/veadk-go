# MCP Router Toolset

这个示例把火山引擎 MCP Router 挂载为 ADK Toolset。

## 使用显式 MCP Router 地址

```bash
export TOOL_MCP_ROUTER_URL="https://your-mcp-router-endpoint/mcp"
export TOOL_MCP_ROUTER_API_KEY="your-api-key"

go run ./examples/tool/mcp_set
```

`TOOL_MCP_ROUTER_API_KEY` 是可选的。配置后，VeADK 会把它作为 bearer token 发送。

## 使用 AgentKit MCP Toolset ID

如果使用方只有 AgentKit MCP Toolset ID，VeADK 可以通过 AgentKit 解析出运行时 endpoint 和 API key：

```bash
export MCP_ROUTER_ID="mt-xxxx"
export VOLCENGINE_ACCESS_KEY="your-ak"
export VOLCENGINE_SECRET_KEY="your-sk"

go run ./examples/tool/mcp_set
```

`MCP_TOOLSET_ID` 也可以作为 `MCP_ROUTER_ID` 的别名使用。

也可以在 `config.yaml` 中配置：

```yaml
tools:
  mcp_router:
    id: mt-xxxx
    url: https://your-mcp-router-endpoint/mcp
    api_key: your-api-key
```

如果显式配置了 `url` 或 `api_key`，它们会覆盖通过 MCP Toolset ID 解析出的值。
