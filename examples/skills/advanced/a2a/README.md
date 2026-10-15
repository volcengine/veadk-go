<!-- markdownlint-disable required-headers -->

# AgentKit Skills Sandbox Examples

This directory mirrors the advanced Skills Sandbox flows from veadk-python for
veadk-go.

## Support Matrix

| Scenario | Flow | Runtime Agent tools | Sample |
| --- | --- | --- | --- |
| Skills Sandbox synchronous delegation | `runtime Agent -> execute_skills -> Skills Sandbox` | `execute_skills` | `runtime_to_skills_sandbox_execute` |
| Skills Sandbox non-blocking tasks | `runtime Agent -> invoke_skill/poll_skill -> Skills Sandbox` | `SkillToolset`, `invoke_skill`, `poll_skill` | `runtime_to_skills_sandbox_invoke_poll` |
| AIO Sandbox code execution | `runtime Agent -> run_code -> AIO/script Sandbox` | `SkillToolset`, `run_code` | `runtime_to_aio_sandbox_run_code` |

The direct client flow from the Python sample,
`client script -> Sandbox /a2a`, is intentionally not included here yet. That
flow needs AgentKit Sandbox session create/reuse client helpers rather than
VeADK Agent tools.

## Common Environment

Model settings follow the normal veadk-go `MODEL_AGENT_*` configuration:

```bash
export MODEL_AGENT_API_KEY="{{your_model_api_key}}"
export MODEL_AGENT_PROVIDER=openai
export MODEL_AGENT_NAME="{{your_model_name}}"
export MODEL_AGENT_API_BASE="{{your_model_base_url}}"
```

AgentKit credentials use the same order as other built-in AgentKit tools:

1. ADK tool context state
2. `VOLCENGINE_ACCESS_KEY` / `VOLCENGINE_SECRET_KEY`
3. configured global credentials
4. mounted VeFaaS IAM credential

`AGENTKIT_TOOL_ID_SKILLS` is used for Skills Sandbox tools and falls back to
`AGENTKIT_TOOL_ID` when unset.

## Skills Sandbox Synchronous Delegation

```bash
go run ./examples/skills/advanced/a2a/runtime_to_skills_sandbox_execute \
  --tool-id "{{your_skills_sandbox_tool_id}}" \
  --skill-space-id "{{your_skill_space_id}}" \
  --session-id remote-skills-demo-1 \
  --prompt "What skills do you have?"
```

The runtime Agent only exposes `execute_skills`. It does not mount local
bash/code execution tools. The request is delegated to the Skills Sandbox and
waits until the A2A task reaches a terminal state.

## Skills Sandbox Non-blocking Tasks

```bash
go run ./examples/skills/advanced/a2a/runtime_to_skills_sandbox_invoke_poll \
  --tool-id "{{your_skills_sandbox_tool_id}}" \
  --skill-space-id "{{your_skill_space_id}}" \
  --session-id remote-skills-poll-demo-1 \
  --prompt "What skills do you have?"
```

This sample loads remote Skill metadata into `SkillToolset`, then gives the
Agent explicit `invoke_skill` and `poll_skill` tools. The model can observe the
A2A task lifecycle instead of waiting inside one synchronous tool call.

## AIO Sandbox Code Execution

```bash
go run ./examples/skills/advanced/a2a/runtime_to_aio_sandbox_run_code \
  --tool-id "{{your_aio_or_script_sandbox_tool_id}}" \
  --skill-space-id "{{your_skill_space_id}}" \
  --session-id aio-skills-demo-1 \
  --prompt "Use Python to print Hello World and run it."
```

This sample loads remote Skill metadata in the runtime Agent and exposes
`run_code` for code execution in an AIO/script sandbox. It does not use
`execute_skills`, `invoke_skill`, or `poll_skill`.

`SkillToolset` Python script execution is backed by AgentKit `run_code`, so
`run_skill_script` also runs in the AIO/script sandbox for `.py` scripts. The
Agent may also call `run_code` directly for ad-hoc Python execution.
