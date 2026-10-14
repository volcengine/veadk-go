# Runner examples

This directory shows how to call an agent through the VeADK runner directly.

## Basic

Run the basic example:

```shell
go run ./examples/runner/basic
```

It demonstrates three common patterns:

- `RunText` for plain text input and a final text response.
- `UserParts` with `CollectFinalText` for multi-part or multimodal input.
- `Run` for consuming the raw ADK event stream.

For multi-turn conversations, reuse the same `userID` and `sessionID` across calls.

## Runtime concepts

`runner.Config` configures the VeADK runner instance. It owns stable runtime dependencies such as the root agent, session service, artifact service, memory service, plugin config, and auto-save behavior. `ShortTermMemory` and `LongTermMemory` are VeADK aliases for the ADK Go `SessionService` and `MemoryService` fields.

`apps.RunConfig` configures VeADK app launchers such as simple app, A2A app, and server app. It uses the same memory/session normalization rules as the runner, but applies them to an app server lifecycle rather than a single runner instance.

`agent.RunConfig` is the ADK Go per-call run config passed to `Run` and `RunText`. Use it for one invocation's ADK behavior, such as streaming mode or saving input blobs as artifacts.

`context.Context` is the standard Go cancellation, deadline, and request-scoped value context. It is not the conversation context. Conversation history comes from the tuple of `userID`, `sessionID`, and the configured `SessionService`.

## Python runner mapping

VeADK Python accepts a broad `messages` value in `Runner.run`. VeADK Go keeps ADK Go's typed content model and uses focused helpers instead.

| Python input | VeADK Go equivalent |
| --- | --- |
| `runner.run(messages="hello")` | `r.RunText(ctx, userID, sessionID, "hello", agent.RunConfig{})` |
| `runner.run(messages=MediaMessage(...))` | Build `genai.Part` values and call `runner.UserParts(...)`, then `r.Run(...)` |
| `runner.run(messages=[...])` | Call `RunText` or `Run` multiple times with the same `userID` and `sessionID` |
| Return final text | `RunText(...)` or `CollectFinalText(r.Run(...))` |
| Consume event stream | Iterate over `r.Run(...)` |

This keeps Go call sites type-safe while still covering the same common workflows: plain text, multimodal input, multi-turn sessions, final text responses, and raw event streams.

## Input helpers

Use these helpers when constructing user messages:

```go
msg := runner.UserText("hello")
msg := runner.UserBytes(imageBytes, "image/png")
msg := runner.UserURI("gs://bucket/image.png", "image/png")
msg := runner.UserParts(
	genai.NewPartFromText("describe this image"),
	genai.NewPartFromBytes(imageBytes, "image/png"),
)
```

For model messages, tool calls, tool responses, or custom roles, use the underlying `google.golang.org/genai` constructors directly.
