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
