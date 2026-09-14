# LLM Shield

The example installs LLM Shield only when `ENABLE_LLM_SHIELD` is `1`, `true`,
`yes`, or `on` (case insensitive, surrounding whitespace ignored). Set
`TOOL_LLM_SHIELD_APP_ID` when enabled. Disabled construction performs no Shield
configuration validation, credential lookup, or network access.

```go
shieldPlugins, err := builtin_tools.LLMShieldPluginsFromEnv()
if err != nil {
    return err
}
runConfig.PluginConfig.Plugins = append(runConfig.PluginConfig.Plugins, shieldPlugins...)
```

This helper follows the extracted veadk-python Sandbox assembly: only
**before-model** is installed, and service failures **fail open**. Like Python,
this callback examines the first text part of the last content only when its
role is `user`. It does not scan the full transcript, later parts, attachments,
tool results, or model output. A block replaces the model invocation with a
final Go ADK response so HTTP/A2A consumers receive the blocking message.

The example also uses web search and a model; configure those dependencies
separately. The SDK does not enable Shield globally merely because the
variable is present: the application must call the helper and install its result.

## Configuration and authentication

| Variable | Meaning |
| --- | --- |
| `ENABLE_LLM_SHIELD` | Opt-in switch used by the assembly helper |
| `TOOL_LLM_SHIELD_APP_ID` | Required environment value when the helper is enabled |
| `TOOL_LLM_SHIELD_API_KEY` | Optional API key; takes priority over Role credentials |
| `TOOL_LLM_SHIELD_REGION` | Region; falls back to `REGION`, existing SDK configuration, then `cn-beijing` |
| `TOOL_LLM_SHIELD_URL` | Optional HTTP(S) origin; defaults to `https://<region>.sdk.access.llm-shield.omini-shield.com` |

The legacy client constructor continues to support the existing `tool.llm_shield`
configuration. These helpers do not load dotenv files themselves.

Without an API key, the client uses the shared lazy `veauth.CredentialSource`.
The default source reads Volcengine AK/SK/SessionToken environment values or the
mounted VeFaaS IAM credential at request time. It resolves credentials on every
moderation request so rotation is visible without restarting. API key requests
perform no Role lookup. Signed requests use service `llmshield`,
`X-Top-Service`, `X-Top-Region`, and Python's `X-Security-Token` header.

Both auth paths send `POST /v2/moderate?Action=Moderate&Version=2025-08-31`, with
`Scene` and `Message` (`Role`, `Content`, `ContentType: 1`). The default timeout
is Python's 50 seconds; the caller's context can cancel sooner. HTTP redirects
are refused to avoid forwarding credentials and content to another endpoint.
Responses, including error bodies, are bounded to 4 MiB and closed. Client errors
and callback logs never include prompts, tool values, response bodies, URLs,
credential-source errors, or credentials.

As in Python, a block requires `DecisionType == 2` **and nonempty `RiskInfo.Risks`**.
A block without risk entries permits execution. Numeric/string categories are
supported; reported reasons contain category names or numeric IDs only. An
otherwise nonblocking degraded or malformed response is a service failure and
therefore permits execution under the default policy. An actionable block still
blocks when a response also reports degradation.

## Explicit policy and callback scope

```go
shield, err := builtin_tools.NewLLMShieldPlugin(builtin_tools.LLMShieldConfig{
    Enabled:       true,
    AppID:         appID,
    APIKey:        apiKey,
    HTTPClient:    httpClient,
    Timeout:       5 * time.Second,
    FailurePolicy: builtin_tools.LLMShieldFailClose,
    Callbacks: []builtin_tools.LLMShieldCallback{
        builtin_tools.LLMShieldBeforeModel,
        builtin_tools.LLMShieldBeforeTool,
    },
})
if err != nil {
    return err
}
if shield != nil {
    runConfig.PluginConfig.Plugins = append(runConfig.PluginConfig.Plugins, shield)
}
```

Explicit configuration reads no environment or global config. A nil `Callbacks`
slice defaults to before-model; an explicitly empty slice installs no callbacks.
`Enabled: false` returns nil immediately. Injected HTTP clients are copied before
redirect policy is applied; the caller-owned client is not modified. Injected
transports and credential sources must support concurrent use. Do not mutate
client configuration or `CategoryMap` while requests are active.

`LLMShieldFailClose` is an explicit Go policy extension: service failures return a
redacted error and stop execution. Cancellation from the caller propagates under
both policies. No automatic retries are performed, matching Python.

Additional scopes correspond to Python's callbacks:

- `LLMShieldAfterModel`: first text part of a model response, checked as `assistant`.
- `LLMShieldBeforeTool`: argument names and values, checked as `user`.
- `LLMShieldAfterTool`: result values, checked as `assistant`.

Go map keys are sorted for deterministic tool checks; structured values use JSON.
These output callbacks are not a buffered streaming-output safety guarantee:
previously emitted chunks cannot be retracted. Enable them only with the desired
application-level delivery policy.

`NewLLMShieldPlugins()` remains available and preserves its historical four
callbacks. Use the new singular constructor/helper for the Python Sandbox default.
`NewLLMShieldClientWithConfig` and `Moderate(ctx, message, role)` also allow direct
moderation without an Agent. The direct method returns redacted service errors;
only plugin callbacks apply fail-open/fail-close.

## Verification

```sh
go test -gcflags='all=-N -l' ./tool/builtin_tools -run TestLLMShield -count=1
go test -race -gcflags='all=-N -l' ./tool/builtin_tools -run TestLLMShield -count=3
go vet ./tool/builtin_tools ./examples/plugins
```

The tests use fake Shield/OpenAI services, independently verify Role signatures,
and launch a real Go server subprocess with synthetic environment values for
HTTP/A2A permit/block/failure, concurrency, sensitive-log and shutdown/restart
contracts. They do not load real dotenv files or call live Shield. Live service
acceptance still requires a dedicated test App ID and injected credentials.
