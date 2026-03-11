// Copyright (c) 2025 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package model

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/bytedance/mockey"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/utils"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func newTestArkModel(t *testing.T) (*arkModel, *arkruntime.Client) {
	t.Helper()
	client := arkruntime.NewClientWithApiKey("test-api-key")
	return &arkModel{
		name:   "test-model",
		config: &ArkClientConfig{APIKey: "test-api-key"},
		client: client,
	}, client
}

func TestNewArkModel(t *testing.T) {
	t.Run("with_api_key", func(t *testing.T) {
		llm, err := NewArkModel(context.Background(), "test-model", &ArkClientConfig{
			APIKey:  "test-key",
			BaseURL: "https://ark.example.com/api/v3/",
		})
		assert.NoError(t, err)
		assert.NotNil(t, llm)
		assert.Equal(t, "test-model", llm.Name())
	})

	t.Run("with_ak_sk", func(t *testing.T) {
		llm, err := NewArkModel(context.Background(), "test-model", &ArkClientConfig{
			AK: "test-ak",
			SK: "test-sk",
		})
		assert.NoError(t, err)
		assert.NotNil(t, llm)
	})

	t.Run("no_auth", func(t *testing.T) {
		_, err := NewArkModel(context.Background(), "test-model", &ArkClientConfig{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API key or AK/SK pair is required")
	})

	t.Run("nil_config", func(t *testing.T) {
		_, err := NewArkModel(context.Background(), "test-model", nil)
		assert.Error(t, err)
	})
}

func TestArkModel_Generate(t *testing.T) {
	mockey.PatchConvey("simple text generation", t, func() {
		am, _ := newTestArkModel(t)

		mockey.Mock((*arkruntime.Client).CreateChatCompletion).Return(
			arkmodel.ChatCompletionResponse{
				ID:    "chatcmpl-test",
				Model: "test-model",
				Choices: []*arkmodel.ChatCompletionChoice{
					{
						Index: 0,
						Message: arkmodel.ChatCompletionMessage{
							Role:    arkmodel.ChatMessageRoleAssistant,
							Content: &arkmodel.ChatCompletionMessageContent{StringValue: volcengine.String("Hello!")},
						},
						FinishReason: arkmodel.FinishReasonStop,
					},
				},
				Usage: arkmodel.Usage{
					PromptTokens:     10,
					CompletionTokens: 5,
					TotalTokens:      15,
				},
			}, nil,
		).Build()

		req := &model.LLMRequest{
			Contents: genai.Text("Hi"),
			Config: &genai.GenerateContentConfig{
				Temperature: float32Ptr(0),
			},
		}

		want := &model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{{Text: "Hello!"}},
			},
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     10,
				CandidatesTokenCount: 5,
				TotalTokenCount:      15,
			},
			CustomMetadata: map[string]any{
				"response_model": "test-model",
			},
			FinishReason: genai.FinishReasonStop,
		}

		for got, err := range am.GenerateContent(context.Background(), req, false) {
			assert.NoError(t, err)
			if diff := cmp.Diff(want, got, cmpopts.IgnoreUnexported(genai.Content{}, genai.Part{})); diff != "" {
				t.Errorf("GenerateContent() mismatch (-want +got):\n%s", diff)
			}
		}
	})
}

func TestArkModel_GenerateWithToolCalls(t *testing.T) {
	mockey.PatchConvey("tool call generation", t, func() {
		am, _ := newTestArkModel(t)

		mockey.Mock((*arkruntime.Client).CreateChatCompletion).Return(
			arkmodel.ChatCompletionResponse{
				ID:    "chatcmpl-test",
				Model: "test-model",
				Choices: []*arkmodel.ChatCompletionChoice{
					{
						Index: 0,
						Message: arkmodel.ChatCompletionMessage{
							Role: arkmodel.ChatMessageRoleAssistant,
							ToolCalls: []*arkmodel.ToolCall{
								{
									ID:   "call_abc123",
									Type: arkmodel.ToolTypeFunction,
									Function: arkmodel.FunctionCall{
										Name:      "get_weather",
										Arguments: `{"city":"Beijing"}`,
									},
								},
							},
						},
						FinishReason: arkmodel.FinishReasonToolCalls,
					},
				},
				Usage: arkmodel.Usage{
					PromptTokens:     10,
					CompletionTokens: 5,
					TotalTokens:      15,
				},
			}, nil,
		).Build()

		req := &model.LLMRequest{
			Contents: genai.Text("What's the weather in Beijing?"),
			Config: &genai.GenerateContentConfig{
				Tools: []*genai.Tool{
					{
						FunctionDeclarations: []*genai.FunctionDeclaration{
							{
								Name:        "get_weather",
								Description: "Get weather info",
								Parameters: &genai.Schema{
									Type: genai.TypeObject,
									Properties: map[string]*genai.Schema{
										"city": {Type: genai.TypeString, Description: "City name"},
									},
									Required: []string{"city"},
								},
							},
						},
					},
				},
			},
		}

		for got, err := range am.GenerateContent(context.Background(), req, false) {
			assert.NoError(t, err)
			assert.NotNil(t, got)
			assert.Equal(t, "model", got.Content.Role)

			// Should have a function call part
			var foundFC bool
			for _, part := range got.Content.Parts {
				if part.FunctionCall != nil {
					foundFC = true
					assert.Equal(t, "get_weather", part.FunctionCall.Name)
					assert.Equal(t, "call_abc123", part.FunctionCall.ID)
					assert.Equal(t, "Beijing", part.FunctionCall.Args["city"])
				}
			}
			assert.True(t, foundFC, "expected function call part")
		}
	})
}

func TestArkModel_GenerateWithReasoning(t *testing.T) {
	mockey.PatchConvey("reasoning content generation", t, func() {
		am, _ := newTestArkModel(t)

		reasoning := "Let me think about this..."
		mockey.Mock((*arkruntime.Client).CreateChatCompletion).Return(
			arkmodel.ChatCompletionResponse{
				ID:    "chatcmpl-test",
				Model: "test-model",
				Choices: []*arkmodel.ChatCompletionChoice{
					{
						Index: 0,
						Message: arkmodel.ChatCompletionMessage{
							Role:             arkmodel.ChatMessageRoleAssistant,
							Content:          &arkmodel.ChatCompletionMessageContent{StringValue: volcengine.String("The answer is 4.")},
							ReasoningContent: &reasoning,
						},
						FinishReason: arkmodel.FinishReasonStop,
					},
				},
				Usage: arkmodel.Usage{
					PromptTokens:     10,
					CompletionTokens: 20,
					TotalTokens:      30,
				},
			}, nil,
		).Build()

		req := &model.LLMRequest{
			Contents: genai.Text("What is 2+2?"),
		}

		for got, err := range am.GenerateContent(context.Background(), req, false) {
			assert.NoError(t, err)
			assert.NotNil(t, got)

			// Should have reasoning part (Thought=true) then text part
			assert.GreaterOrEqual(t, len(got.Content.Parts), 2)
			assert.True(t, got.Content.Parts[0].Thought)
			assert.Equal(t, "Let me think about this...", got.Content.Parts[0].Text)
			assert.Equal(t, "The answer is 4.", got.Content.Parts[1].Text)
		}
	})
}

func TestArkModel_GenerateStream(t *testing.T) {
	mockey.PatchConvey("streaming text generation", t, func() {
		am, _ := newTestArkModel(t)

		chunks := []arkmodel.ChatCompletionStreamResponse{
			{
				ID:    "chatcmpl-test",
				Model: "test-model",
				Choices: []*arkmodel.ChatCompletionStreamChoice{
					{Index: 0, Delta: arkmodel.ChatCompletionStreamChoiceDelta{Content: "Hello"}},
				},
			},
			{
				ID:    "chatcmpl-test",
				Model: "test-model",
				Choices: []*arkmodel.ChatCompletionStreamChoice{
					{Index: 0, Delta: arkmodel.ChatCompletionStreamChoiceDelta{Content: " World"}},
				},
			},
			{
				ID:    "chatcmpl-test",
				Model: "test-model",
				Choices: []*arkmodel.ChatCompletionStreamChoice{
					{Index: 0, Delta: arkmodel.ChatCompletionStreamChoiceDelta{}, FinishReason: arkmodel.FinishReasonStop},
				},
				Usage: &arkmodel.Usage{
					PromptTokens:     10,
					CompletionTokens: 5,
					TotalTokens:      15,
				},
			},
		}

		chunkIdx := 0
		mockStreamReader := &utils.ChatCompletionStreamReader{}

		mockey.Mock((*arkruntime.Client).CreateChatCompletionStream).Return(
			mockStreamReader, nil,
		).Build()

		mockey.Mock((*utils.ChatCompletionStreamReader).Recv).To(func(_ *utils.ChatCompletionStreamReader) (arkmodel.ChatCompletionStreamResponse, error) {
			if chunkIdx >= len(chunks) {
				return arkmodel.ChatCompletionStreamResponse{}, io.EOF
			}
			chunk := chunks[chunkIdx]
			chunkIdx++
			return chunk, nil
		}).Build()

		mockey.Mock((*utils.ChatCompletionStreamReader).Close).Return(nil).Build()

		req := &model.LLMRequest{
			Contents: genai.Text("Say hello"),
		}

		var partialText strings.Builder
		var finalResp *model.LLMResponse
		for resp, err := range am.GenerateContent(context.Background(), req, true) {
			assert.NoError(t, err)
			if resp.Partial && len(resp.Content.Parts) > 0 {
				partialText.WriteString(resp.Content.Parts[0].Text)
			} else {
				finalResp = resp
			}
		}

		assert.Equal(t, "Hello World", partialText.String())
		assert.NotNil(t, finalResp)
		assert.Equal(t, genai.FinishReasonStop, finalResp.FinishReason)
		assert.NotNil(t, finalResp.UsageMetadata)
		assert.Equal(t, int32(10), finalResp.UsageMetadata.PromptTokenCount)
	})
}

func TestArkModel_ConvertRequest(t *testing.T) {
	t.Run("system_instruction", func(t *testing.T) {
		am := &arkModel{name: "test-model", config: &ArkClientConfig{}}
		req := &model.LLMRequest{
			Contents: genai.Text("Hello"),
			Config: &genai.GenerateContentConfig{
				SystemInstruction: genai.NewContentFromText("You are helpful.", "system"),
			},
		}

		arkReq, err := am.convertArkRequest(req)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(arkReq.Messages), 2)
		assert.Equal(t, arkmodel.ChatMessageRoleSystem, arkReq.Messages[0].Role)
		assert.Equal(t, "You are helpful.", *arkReq.Messages[0].Content.StringValue)
	})

	t.Run("generation_config", func(t *testing.T) {
		am := &arkModel{name: "test-model", config: &ArkClientConfig{}}
		req := &model.LLMRequest{
			Contents: genai.Text("Hello"),
			Config: &genai.GenerateContentConfig{
				Temperature:    float32Ptr(0.5),
				MaxOutputTokens: 100,
				TopP:           float32Ptr(0.9),
				StopSequences:  []string{"END"},
			},
		}

		arkReq, err := am.convertArkRequest(req)
		assert.NoError(t, err)
		assert.InDelta(t, float32(0.5), *arkReq.Temperature, 0.001)
		assert.Equal(t, 100, *arkReq.MaxTokens)
		assert.InDelta(t, float32(0.9), *arkReq.TopP, 0.001)
		assert.Equal(t, []string{"END"}, arkReq.Stop)
	})

	t.Run("thinking_config", func(t *testing.T) {
		am := &arkModel{
			name: "test-model",
			config: &ArkClientConfig{
				ExtraBody: map[string]any{
					"extra_body": map[string]any{
						"thinking": map[string]any{
							"type": "disabled",
						},
					},
				},
			},
		}
		req := &model.LLMRequest{
			Contents: genai.Text("Hello"),
		}

		arkReq, err := am.convertArkRequest(req)
		assert.NoError(t, err)
		assert.NotNil(t, arkReq.Thinking)
		assert.Equal(t, arkmodel.ThinkingTypeDisabled, arkReq.Thinking.Type)
	})

	t.Run("tools", func(t *testing.T) {
		am := &arkModel{name: "test-model", config: &ArkClientConfig{}}
		req := &model.LLMRequest{
			Contents: genai.Text("Hello"),
			Config: &genai.GenerateContentConfig{
				Tools: []*genai.Tool{
					{
						FunctionDeclarations: []*genai.FunctionDeclaration{
							{
								Name:        "search",
								Description: "Search the web",
								Parameters: &genai.Schema{
									Type: genai.TypeObject,
									Properties: map[string]*genai.Schema{
										"query": {Type: genai.TypeString},
									},
								},
							},
						},
					},
				},
			},
		}

		arkReq, err := am.convertArkRequest(req)
		assert.NoError(t, err)
		assert.Len(t, arkReq.Tools, 1)
		assert.Equal(t, arkmodel.ToolTypeFunction, arkReq.Tools[0].Type)
		assert.Equal(t, "search", arkReq.Tools[0].Function.Name)
	})
}

func TestArkModel_ConvertContentWithFunctionResponse(t *testing.T) {
	am := &arkModel{name: "test-model", config: &ArkClientConfig{}}

	content := &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			genai.NewPartFromFunctionResponse("get_weather", map[string]any{
				"temp": 20,
			}),
		},
	}
	content.Parts[0].FunctionResponse.ID = "call_abc123"

	msgs, err := am.convertGenAIContentToArk(content)
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, arkmodel.ChatMessageRoleTool, msgs[0].Role)
	assert.Equal(t, "call_abc123", msgs[0].ToolCallID)
}

func TestArkModel_BuildUsageMetadata(t *testing.T) {
	t.Run("normal_usage", func(t *testing.T) {
		usage := &arkmodel.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		}
		meta := buildArkUsageMetadata(usage)
		assert.Equal(t, int32(10), meta.PromptTokenCount)
		assert.Equal(t, int32(5), meta.CandidatesTokenCount)
		assert.Equal(t, int32(15), meta.TotalTokenCount)
	})

	t.Run("nil_usage", func(t *testing.T) {
		meta := buildArkUsageMetadata(nil)
		assert.Nil(t, meta)
	})

	t.Run("computed_total", func(t *testing.T) {
		usage := &arkmodel.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
		}
		meta := buildArkUsageMetadata(usage)
		assert.Equal(t, int32(15), meta.TotalTokenCount)
	})
}
