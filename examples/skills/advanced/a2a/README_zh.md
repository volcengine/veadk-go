<!-- markdownlint-disable required-headers -->

# AgentKit Skills Sandbox 示例

这个目录提供 veadk-go 版本的 advanced Skills Sandbox 示例，对齐
veadk-python 的主要 runtime Agent 链路。

## 支持矩阵

| 场景 | 链路 | runtime Agent 工具 | 示例 |
| --- | --- | --- | --- |
| Skills Sandbox 同步委托 | `runtime Agent -> execute_skills -> Skills Sandbox` | `execute_skills` | `runtime_to_skills_sandbox_execute` |
| Skills Sandbox 非阻塞任务 | `runtime Agent -> invoke_skill/poll_skill -> Skills Sandbox` | `SkillToolset`、`invoke_skill`、`poll_skill` | `runtime_to_skills_sandbox_invoke_poll` |
| AIO Sandbox 代码执行 | `runtime Agent -> run_code -> AIO/script Sandbox` | `SkillToolset`、`run_code` | `runtime_to_aio_sandbox_run_code` |

Python 示例中的直接客户端链路 `client script -> Sandbox /a2a` 暂未包含。那条链路需要
AgentKit Sandbox Session 创建/复用客户端能力，不属于本次 VeADK Agent 工具对齐的核心范围。

## 通用配置

模型配置沿用 veadk-go 的 `MODEL_AGENT_*`：

```bash
export MODEL_AGENT_API_KEY="{{your_model_api_key}}"
export MODEL_AGENT_PROVIDER=openai
export MODEL_AGENT_NAME="{{your_model_name}}"
export MODEL_AGENT_API_BASE="{{your_model_base_url}}"
```

AgentKit 凭证解析顺序和其他内置 AgentKit 工具一致：

1. ADK tool context state
2. `VOLCENGINE_ACCESS_KEY` / `VOLCENGINE_SECRET_KEY`
3. 全局配置中的 Volcengine 凭证
4. 挂载的 VeFaaS IAM credential

Skills Sandbox 工具优先读取 `AGENTKIT_TOOL_ID_SKILLS`，未配置时 fallback 到
`AGENTKIT_TOOL_ID`。

## Skills Sandbox 同步委托

```bash
go run ./examples/skills/advanced/a2a/runtime_to_skills_sandbox_execute \
  --tool-id "{{your_skills_sandbox_tool_id}}" \
  --skill-space-id "{{your_skill_space_id}}" \
  --session-id remote-skills-demo-1 \
  --prompt "你有哪些技能"
```

runtime Agent 只挂载 `execute_skills`，不会暴露本地 bash/code 执行工具。工具会把请求委托给
Skills Sandbox，并等待 A2A task 到达终态。

## Skills Sandbox 非阻塞任务

```bash
go run ./examples/skills/advanced/a2a/runtime_to_skills_sandbox_invoke_poll \
  --tool-id "{{your_skills_sandbox_tool_id}}" \
  --skill-space-id "{{your_skill_space_id}}" \
  --session-id remote-skills-poll-demo-1 \
  --prompt "你有哪些技能"
```

这个示例会把远程 Skill metadata 加载到 `SkillToolset`，并给 Agent 显式挂载
`invoke_skill` 和 `poll_skill`。模型可以感知 A2A task 的生命周期，而不是在一个同步工具调用里等待最终结果。

## AIO Sandbox 代码执行

```bash
go run ./examples/skills/advanced/a2a/runtime_to_aio_sandbox_run_code \
  --tool-id "{{your_aio_or_script_sandbox_tool_id}}" \
  --skill-space-id "{{your_skill_space_id}}" \
  --session-id aio-skills-demo-1 \
  --prompt "请用 Python 打印 Hello World 并执行"
```

这个示例在 runtime Agent 中加载远程 Skill metadata，并暴露 `run_code`，让代码执行发生在
AIO/script Sandbox 中。它不使用 `execute_skills`、`invoke_skill` 或 `poll_skill`。

当前 Go 示例里 `SkillToolset` 保持只读。如果技能流程中需要执行脚本，应让 Agent 显式调用
`run_code`。
