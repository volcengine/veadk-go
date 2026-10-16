# MCP Router Toolset

This example mounts a Volcengine MCP Router as an ADK toolset.

## Use an explicit MCP Router endpoint

```bash
export TOOL_MCP_ROUTER_URL="https://your-mcp-router-endpoint/mcp"
export TOOL_MCP_ROUTER_API_KEY="your-api-key"

go run ./examples/tool/mcp_set
```

`TOOL_MCP_ROUTER_API_KEY` is optional. When set, VeADK sends it as a bearer token.

## Use an AgentKit MCP Toolset ID

When only the AgentKit MCP Toolset ID is available, VeADK can resolve the runtime endpoint and API key from AgentKit:

```bash
export MCP_ROUTER_ID="mt-xxxx"
export VOLCENGINE_ACCESS_KEY="your-ak"
export VOLCENGINE_SECRET_KEY="your-sk"

go run ./examples/tool/mcp_set
```

`MCP_TOOLSET_ID` is accepted as an alias for `MCP_ROUTER_ID`.

You can also configure the same values in `config.yaml`:

```yaml
tools:
  mcp_router:
    id: mt-xxxx
    url: https://your-mcp-router-endpoint/mcp
    api_key: your-api-key
```

If `url` or `api_key` are set explicitly, they override the values resolved from the MCP Toolset ID.
