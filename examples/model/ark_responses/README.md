# Ark Responses

`model.NewArkResponsesModel` is an opt-in implementation of ADK's `model.LLM`.
Pass it through the Agent's existing `Model` field. Existing `NewArkModel`,
`NewOpenAIModel`, provider values, and default behavior remain unchanged.

```go
llm, err := model.NewArkResponsesModel(ctx, modelName, &model.ArkClientConfig{
    APIKey:  apiKey,
    BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
})
if err != nil {
    return err
}
root, err := veagent.New(&veagent.Config{
    Config: llmagent.Config{Name: "vision", Model: llm},
})
```

Authentication precedence matches `NewArkModel`: API key, AK/SK, then a lazy
`APIKeyProvider`. Custom HTTP clients and headers are supported. Invalidatable
key providers refresh once on HTTP 401/403, before any streaming output.

## Multimodal inputs

| ADK input | Ark Responses representation |
| --- | --- |
| `InlineData` with image MIME | `input_image` with a base64 data URI |
| `InlineData` with video MIME | `input_video` with a base64 data URI |
| `InlineData` with document MIME | `input_file.file_data` |
| `FileData` with HTTP(S) URL | `image_url`, `video_url`, or `file_url`, according to MIME |
| `FileData` with data URI | Image/video URL field, or document `file_data` with `filename` |
| `FileData.FileURI = "file_id://file-xxx"` | Corresponding input type with `file_id = "file-xxx"` |

Inline documents carry a `filename` from `DisplayName` (default `attachment.pdf`
for PDF), as required by Ark. `VideoMetadata.FPS` is forwarded for video inputs. Actual MIME support, file
limits, and vision capability depend on the selected Ark model. A Files API ID
must be accessible to the credentials used for the model request.

This model does **not** open local paths or download sandbox files. The caller
reads local files or uses `/v1/file/download`, then supplies `InlineData`.
Alternatively, the caller uploads via Ark Files API and supplies `file_id://`.
Unlike Python's automatic local-file upload path, `file://` is not accepted by
this adapter. It never interprets user-supplied paths as files on the runtime.

## Tool images

Images in `genai.FunctionResponse.Parts` support the same inline, URL and file ID
forms. For Ark compatibility, `function_call_output.output` stays a JSON string;
attachments follow in an adjacent user-role message that identifies the tool and
call ID. Image bytes are never serialized into the JSON tool result as text.
Parallel tool results retain their ordering and image association.

ADK Go's ordinary `functiontool` returns a JSON map, without setting native
`FunctionResponse.Parts`. This example's `image_tool.go` uses an existing
`OnEventCallback` to attach media before the event is persisted. It scopes pending
attachments by invocation and call ID and clears them on completion. The loader
owns file I/O; the model adapter has no knowledge of local/AIO/Skills modes.
This is an application example, not an automatically installed built-in tool.

```sh
export MODEL_AGENT_NAME='your-vision-model'
export MODEL_AGENT_API_KEY='...'
export MODEL_AGENT_API_BASE='https://ark.cn-beijing.volces.com/api/v3'

# User message: the example reads the local image into InlineData.
go run ./examples/model/ark_responses -image ./picture.png

# Real function call -> FunctionResponse image -> model continuation.
go run ./examples/model/ark_responses -image ./picture.png -tool

# Files API image, returned by a tool.
go run ./examples/model/ark_responses -uri file_id://file-xxx -tool
```

## History and streaming

ADK owns conversation history. Requests replay it with `store: false`; this
implementation does not enable Python's provider-side response caching or
`previous_response_id` chains. `ExtraBody["extra_body"]` may supply optional Ark
parameters but cannot override model, input, stream mode, or history ownership.
The adapter does not mutate the ADK request or append synthetic user messages.

Streaming emits partial text/reasoning followed by one authoritative completed
response, including function calls and usage. Failed/incomplete/truncated streams
return an error. New, unused provider response fields are tolerated without
changing SDK-wide decoder settings or upgrading dependencies.

## Verification

Normal tests use HTTP fixtures and a real ADK Runner to verify conversion,
authentication refresh, stream failure handling, and tool-image persistence and
replay. Live tests are disabled by default:

```sh
VEADK_ARK_RESPONSES_LIVE=1 \
VEADK_LIVE_ENV_FILE=/absolute/path/to/private.env \
go test -v -count=1 -timeout=18m ./examples/model/ark_responses \
  -run '^TestArkResponsesLive$'
```

Live tests need the model environment variables above, `AGENTKIT_TOOL_ID`, and
AgentKit credentials. They upload a generated image to Ark Files API, create an
AIO sandbox, and verify 24 combinations: no Skills/local Skills/AIO × user/tool
image × inline/file ID × ordinary/streaming calls. They check model recognition,
attachment persistence, and sandbox download bytes, and delete their remote test
resources afterward. URL/video/document conversion is covered by unit tests;
this live matrix specifically exercises image understanding.
