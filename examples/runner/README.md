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

## Update session state

Update an existing session before the next agent run with `r.UpdateSession`:

```go
// r is an initialized VeADK runner. Create the session once, or use a session
// already created by a previous Run/RunText call.
_, err := r.SessionService().Create(ctx, &session.CreateRequest{
    AppName: r.AppName(), UserID: "user", SessionID: "session",
    State: map[string]any{"phase": "created", "keep": true},
})
if err != nil {
    return err
}
if err := r.UpdateSession(ctx, runner.SessionUpdateRequest{
    UserID: "user", SessionID: "session",
    StateDelta: map[string]any{"phase": "approved", "approval": true},
}); err != nil {
    return err
}
// Read the committed state and event history through the same service.
updated, err := r.SessionService().Get(ctx, &session.GetRequest{
    AppName: r.AppName(), UserID: "user", SessionID: "session",
})
if err != nil {
    return err
}
phase, err := updated.Session.State().Get("phase") // "approved"
```

The snippets use `google.golang.org/adk/v2/session`. The SDK accepts one request shape for state, content, and artifact-version updates:

```go
type SessionUpdateRequest struct {
    AppName string // package helper only; Runner uses r.AppName()
    UserID string
    SessionID string
    StateDelta map[string]any
    Content *genai.Content
    ArtifactDelta map[string]int64
    Author string
    InvocationID string
}

func (r *Runner) UpdateSession(ctx context.Context, req runner.SessionUpdateRequest) error
func UpdateSession(ctx context.Context, sessionService session.Service, req runner.SessionUpdateRequest) error
```

For a custom runtime or app integration, pass its final session service to `runner.UpdateSession(ctx, svc, runner.SessionUpdateRequest{AppName: appName, ...})`. The runner method uses the normalized app name and service: `SessionService` takes precedence over `ShortTermMemory`, and the default is an in-memory service. Updating does not execute the agent or save long-term memory. A missing session returns an error even when `AutoCreateSession` is enabled.

The simple HTTP API exposes the same request shape:

```http
POST /sessions/{session_id}:update
Content-Type: application/json

{
  "user_id": "user",
  "state_delta": {
    "phase": "approved",
    "approval": true
  },
  "author": "external_tool",
  "content": {
    "role": "user",
    "parts": [{"text": "approval granted"}]
  },
  "artifact_delta": {
    "approval.pdf": 3
  }
}
```

`session_id` comes from the path. `user_id` is optional for the simple app and defaults to its configured user; custom callers should pass the intended user explicitly. The endpoint returns `{ "code": 200, "message": "success", "session_id": "..." }` on success and uses `400` for malformed request payloads or obvious validation errors.

Choose a short-term backend once when constructing the runtime, using the existing memory factory:

```go
svc, err := memory.NewShortTermMemoryService(memory.BackendShortTermSQLite,
    &short_term_memory_backends.SqliteBackendConfig{
        CommonDatabaseConfig: &configs.CommonDatabaseConfig{DBUrl: "session.db"},
    })
if err != nil {
    return err
}
r, err := runner.New(runner.Config{AppName: "app", Agent: rootAgent, ShortTermMemory: svc})
```

These packages are `github.com/volcengine/veadk-go/v2/{memory,configs,runner}` and `github.com/volcengine/veadk-go/v2/memory/short_term_memory_backends`.

| Backend | Factory type | Configuration |
| --- | --- | --- |
| Local memory | `memory.BackendShortTermLocal` | `nil`; valid only for that service instance and process |
| SQLite | `memory.BackendShortTermSQLite` | `*short_term_memory_backends.SqliteBackendConfig` |
| MySQL | `memory.BackendShortTermMySQL` | `*short_term_memory_backends.MysqlBackendConfig` |
| PostgreSQL | `memory.BackendShortTermPostgreSQL` | `*short_term_memory_backends.PostgresqlBackendConfig` |

Database configurations embed `*configs.CommonDatabaseConfig`; provide `DBUrl` or the existing database settings. Keep credentials out of source and logs. Updates use the supplied service's normal persistence behavior; do not create another service to perform the update. Database services can read persisted state after reopening the same database, while a new in-memory service starts empty.

State deltas merge at the top level, preserving unspecified keys. A supplied object replaces the whole value at its key; there is no deep merge. `nil` stores JSON null rather than deleting the key. Persistent values must be JSON serializable, including finite numbers; invalid values fail before appending. The caller's map is not modified and must remain stable during the call.

Ordinary keys belong to the target session. ADK `user:` keys are shared across sessions of the same app/user, and `app:` keys across the same app. `temp:` keys are filtered before validation and never enter the new API's stored state or events. A nil, empty, or temp-only delta verifies that the session exists and succeeds without adding an event or changing the update time.

Each nonempty persistent update appends one standard non-partial ADK event. State/artifact-only updates default to author `system`; content updates must provide `Author` because they enter session history and can affect later prompts. `ArtifactDelta` records filename/version references only; save artifact bytes through the same runtime's `ArtifactService` before adding those references. Get, serialization, cancellation, and append errors preserve their underlying causes through Go error wrapping. MySQL strict mode has a known upstream ADK v2.1.0 limitation when creating shared app/user state: the PRD survey reproduced a mixed `app:`/`user:` update failing on a zero update-time value. Basic session keys are the compatibility target; shared-state errors are returned unchanged in the error chain. The standalone `user:` MySQL case still requires e2e verification.

Serialize updates and runs on the same session. Get plus AppendEvent provides neither CAS nor cross-request deduplication, and database stale/lock errors are returned without retrying. If a response is lost, read back before deciding whether to retry. The caller is responsible for app/user authorization.

The existing lower-level path remains available: `Get` the latest session, construct `session.NewEvent` with `Actions.StateDelta`, then `AppendEvent` on the same service. Calling `State().Set` on a retrieved snapshot alone does not persist it. For updates accompanying agent execution, continue using `adkrunner.WithStateDelta` with `Run`/`RunText`; arbitrary complete events still go through `SessionService.AppendEvent`.

## Runtime concepts

`runner.Config` configures the VeADK runner instance. It owns stable runtime dependencies such as the root agent, session service, artifact service, memory service, plugin config, and auto-save behavior. `ShortTermMemory` and `LongTermMemory` are VeADK aliases for the ADK Go `SessionService` and `MemoryService` fields.

`apps.RunConfig` configures VeADK app launchers such as simple app, A2A app, and server app. It uses the same memory/session normalization rules as the runner, but applies them to an app server lifecycle rather than a single runner instance.

`agent.RunConfig` is the ADK Go per-call run config passed to `Run` and `RunText`. Use it for one invocation's ADK behavior, such as streaming mode or saving input blobs as artifacts.

`context.Context` is the standard Go cancellation, deadline, and request-scoped value context. It is not the conversation context. Conversation history comes from the tuple of `userID`, `sessionID`, and the configured `SessionService`.

## App integration

The app package uses the same session, memory, plugin, and auto-save concepts as the direct runner path:

- `apps.RunConfig.ShortTermMemory` is an alias for `SessionService`.
- `apps.RunConfig.LongTermMemory` is an alias for `MemoryService`.
- When `AutoSaveSession` is enabled, apps install the same VeADK auto-save memory plugin used by the runner.
- The simple app `/invoke` route returns a final text response through the VeADK runner helper path.
- A2A and ADK REST routes keep the raw ADK event stream because those protocols need streaming events, artifacts, tool calls, and metadata.

Use direct `runner.New` when embedding an agent in your own Go flow. Use `apps.RunConfig` when launching one of VeADK's HTTP app surfaces.

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
