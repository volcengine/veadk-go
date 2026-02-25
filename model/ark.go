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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"strings"

	"github.com/google/uuid"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// ArkClientConfig holds configuration for the ARK SDK-based model.
type ArkClientConfig struct {
	APIKey    string
	AK        string // Volcengine Access Key (alternative to APIKey)
	SK        string // Volcengine Secret Key (alternative to APIKey)
	BaseURL   string
	Region    string
	ExtraBody map[string]any
}

type arkModel struct {
	name   string
	config *ArkClientConfig
	client *arkruntime.Client
}

// NewArkModel creates an LLM backed by the Volcengine ARK SDK.
// Auth is resolved as: APIKey > AK/SK. At least one must be provided.
func NewArkModel(ctx context.Context, modelName string, config *ArkClientConfig) (model.LLM, error) {
	_ = ctx

	if config == nil {
		config = &ArkClientConfig{}
	}

	var opts []arkruntime.ConfigOption
	if config.BaseURL != "" {
		opts = append(opts, arkruntime.WithBaseUrl(config.BaseURL))
	}
	if config.Region != "" {
		opts = append(opts, arkruntime.WithRegion(config.Region))
	}

	var client *arkruntime.Client
	switch {
	case config.APIKey != "":
		client = arkruntime.NewClientWithApiKey(config.APIKey, opts...)
	case config.AK != "" && config.SK != "":
		client = arkruntime.NewClientWithAkSk(config.AK, config.SK, opts...)
	default:
		return nil, fmt.Errorf("ark: API key or AK/SK pair is required")
	}

	return &arkModel{
		name:   modelName,
		config: config,
		client: client,
	}, nil
}

func (m *arkModel) Name() string {
	return m.name
}

func (m *arkModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	maybeAppendUserContent(req)

	arkReq, err := m.convertArkRequest(req)
	if err != nil {
		return func(yield func(*model.LLMResponse, error) bool) {
			yield(nil, fmt.Errorf("ark: failed to convert request: %w", err))
		}
	}

	if stream {
		return m.generateStream(ctx, arkReq)
	}
	return m.generate(ctx, arkReq)
}

// convertArkRequest converts a genai LLMRequest to an ARK SDK CreateChatCompletionRequest.
func (m *arkModel) convertArkRequest(req *model.LLMRequest) (*arkmodel.CreateChatCompletionRequest, error) {
	arkReq := &arkmodel.CreateChatCompletionRequest{
		Model:    m.name,
		Messages: make([]*arkmodel.ChatCompletionMessage, 0),
		StreamOptions: &arkmodel.StreamOptions{
			IncludeUsage: true,
		},
	}

	// System instruction
	if req.Config != nil && req.Config.SystemInstruction != nil {
		sysContent := extractTextFromContent(req.Config.SystemInstruction)
		if sysContent != "" {
			arkReq.Messages = append(arkReq.Messages, &arkmodel.ChatCompletionMessage{
				Role:    arkmodel.ChatMessageRoleSystem,
				Content: newArkStringContent(sysContent),
			})
		}
	}

	// Contents
	for _, content := range req.Contents {
		msgs, err := m.convertGenAIContentToArk(content)
		if err != nil {
			return nil, fmt.Errorf("failed to convert content: %w", err)
		}
		arkReq.Messages = append(arkReq.Messages, msgs...)
	}

	// Tools
	if req.Config != nil && len(req.Config.Tools) > 0 {
		for _, t := range req.Config.Tools {
			if t.FunctionDeclarations != nil {
				for _, fn := range t.FunctionDeclarations {
					arkReq.Tools = append(arkReq.Tools, convertFunctionDeclarationToArk(fn))
				}
			}
		}
	}

	// Generation config
	if req.Config != nil {
		if req.Config.Temperature != nil {
			temp := float32(*req.Config.Temperature)
			arkReq.Temperature = &temp
		}
		if req.Config.MaxOutputTokens > 0 {
			maxTokens := int(req.Config.MaxOutputTokens)
			arkReq.MaxTokens = &maxTokens
		}
		if req.Config.TopP != nil {
			topP := float32(*req.Config.TopP)
			arkReq.TopP = &topP
		}
		if len(req.Config.StopSequences) > 0 {
			arkReq.Stop = req.Config.StopSequences
		}
		if req.Config.ResponseMIMEType == "application/json" {
			arkReq.ResponseFormat = &arkmodel.ResponseFormat{
				Type: arkmodel.ResponseFormatJsonObject,
			}
		}
	}

	// Extra body: Thinking config
	if m.config.ExtraBody != nil {
		if extraBody, ok := m.config.ExtraBody["extra_body"]; ok {
			if eb, ok := extraBody.(map[string]any); ok {
				if thinking, ok := eb["thinking"]; ok {
					if tc, ok := thinking.(map[string]any); ok {
						if t, ok := tc["type"].(string); ok {
							arkReq.Thinking = &arkmodel.Thinking{
								Type: arkmodel.ThinkingType(t),
							}
						}
					} else if tc, ok := thinking.(map[string]string); ok {
						if t, ok := tc["type"]; ok {
							arkReq.Thinking = &arkmodel.Thinking{
								Type: arkmodel.ThinkingType(t),
							}
						}
					}
				}
				if effort, ok := eb["reasoning_effort"].(string); ok {
					re := arkmodel.ReasoningEffort(effort)
					arkReq.ReasoningEffort = &re
				}
			}
		}
	}

	return arkReq, nil
}

// convertGenAIContentToArk converts a genai.Content to ARK SDK messages.
func (m *arkModel) convertGenAIContentToArk(content *genai.Content) ([]*arkmodel.ChatCompletionMessage, error) {
	if content == nil || len(content.Parts) == 0 {
		return nil, nil
	}

	role := content.Role
	if role == "model" {
		role = arkmodel.ChatMessageRoleAssistant
	}

	// Handle function responses → tool messages
	var toolMessages []*arkmodel.ChatCompletionMessage
	for _, part := range content.Parts {
		if part.FunctionResponse != nil {
			responseJSON, err := json.Marshal(part.FunctionResponse.Response)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal function response: %w", err)
			}
			toolCallID := part.FunctionResponse.ID
			if toolCallID == "" {
				toolCallID = "call_" + uuid.New().String()[:8]
			}
			toolMessages = append(toolMessages, &arkmodel.ChatCompletionMessage{
				Role:       arkmodel.ChatMessageRoleTool,
				Content:    newArkStringContent(string(responseJSON)),
				ToolCallID: toolCallID,
			})
		}
	}
	if len(toolMessages) > 0 {
		return toolMessages, nil
	}

	// Handle text, inline data, function calls
	var textParts []string
	var contentParts []*arkmodel.ChatCompletionMessageContentPart
	var toolCalls []*arkmodel.ToolCall

	for _, part := range content.Parts {
		if part.Text != "" {
			textParts = append(textParts, part.Text)
		} else if part.InlineData != nil && len(part.InlineData.Data) > 0 {
			mimeType := part.InlineData.MIMEType
			base64Data := base64.StdEncoding.EncodeToString(part.InlineData.Data)
			dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)

			if strings.HasPrefix(mimeType, "image/") {
				contentParts = append(contentParts, &arkmodel.ChatCompletionMessageContentPart{
					Type:     arkmodel.ChatCompletionMessageContentPartTypeImageURL,
					ImageURL: &arkmodel.ChatMessageImageURL{URL: dataURI},
				})
			} else if strings.HasPrefix(mimeType, "video/") {
				contentParts = append(contentParts, &arkmodel.ChatCompletionMessageContentPart{
					Type:     arkmodel.ChatCompletionMessageContentPartTypeVideoURL,
					VideoURL: &arkmodel.ChatMessageVideoURL{URL: dataURI},
				})
			} else if strings.HasPrefix(mimeType, "text/") {
				textParts = append(textParts, string(part.InlineData.Data))
			}
		} else if part.FunctionCall != nil {
			argsJSON, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal function args: %w", err)
			}
			callID := part.FunctionCall.ID
			if callID == "" {
				callID = "call_" + uuid.New().String()[:8]
			}
			toolCalls = append(toolCalls, &arkmodel.ToolCall{
				ID:   callID,
				Type: arkmodel.ToolTypeFunction,
				Function: arkmodel.FunctionCall{
					Name:      part.FunctionCall.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}

	msg := &arkmodel.ChatCompletionMessage{Role: role}

	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
		if len(textParts) > 0 {
			msg.Content = newArkStringContent(strings.Join(textParts, "\n"))
		}
	} else if len(contentParts) > 0 {
		// Multi-modal: use list content
		for _, text := range textParts {
			contentParts = append([]*arkmodel.ChatCompletionMessageContentPart{{
				Type: arkmodel.ChatCompletionMessageContentPartTypeText,
				Text: text,
			}}, contentParts...)
		}
		msg.Content = &arkmodel.ChatCompletionMessageContent{ListValue: contentParts}
	} else if len(textParts) > 0 {
		msg.Content = newArkStringContent(strings.Join(textParts, "\n"))
	}

	return []*arkmodel.ChatCompletionMessage{msg}, nil
}

func convertFunctionDeclarationToArk(fn *genai.FunctionDeclaration) *arkmodel.Tool {
	params := convertFunctionParameters(fn)
	return &arkmodel.Tool{
		Type: arkmodel.ToolTypeFunction,
		Function: &arkmodel.FunctionDefinition{
			Name:        fn.Name,
			Description: fn.Description,
			Parameters:  params,
		},
	}
}

func newArkStringContent(s string) *arkmodel.ChatCompletionMessageContent {
	return &arkmodel.ChatCompletionMessageContent{StringValue: volcengine.String(s)}
}

// generate handles non-streaming chat completion.
func (m *arkModel) generate(ctx context.Context, arkReq *arkmodel.CreateChatCompletionRequest) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		resp, err := m.client.CreateChatCompletion(ctx, *arkReq)
		if err != nil {
			yield(nil, fmt.Errorf("ark: chat completion failed: %w", err))
			return
		}

		llmResp, err := m.convertArkResponse(&resp)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(llmResp, nil)
	}
}

// generateStream handles streaming chat completion.
func (m *arkModel) generateStream(ctx context.Context, arkReq *arkmodel.CreateChatCompletionRequest) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		stream, err := m.client.CreateChatCompletionStream(ctx, *arkReq)
		if err != nil {
			yield(nil, fmt.Errorf("ark: stream creation failed: %w", err))
			return
		}
		defer stream.Close()

		var textBuffer strings.Builder
		var reasoningBuffer strings.Builder
		var accToolCalls []*arkmodel.ToolCall
		var finalUsage *arkmodel.Usage
		var finishReason arkmodel.FinishReason

		for {
			chunk, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				yield(nil, fmt.Errorf("ark: stream recv failed: %w", err))
				return
			}

			if chunk.Usage != nil {
				finalUsage = chunk.Usage
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			choice := chunk.Choices[0]
			if choice.FinishReason != "" && choice.FinishReason != arkmodel.FinishReasonNull {
				finishReason = choice.FinishReason
			}
			delta := choice.Delta

			// Reasoning content
			if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
				text := *delta.ReasoningContent
				reasoningBuffer.WriteString(text)
				llmResp := &model.LLMResponse{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: text, Thought: true}},
					},
					Partial: true,
				}
				if !yield(llmResp, nil) {
					return
				}
			}

			// Text content
			if delta.Content != "" {
				textBuffer.WriteString(delta.Content)
				llmResp := &model.LLMResponse{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: delta.Content}},
					},
					Partial: true,
				}
				if !yield(llmResp, nil) {
					return
				}
			}

			// Tool calls (accumulate across chunks)
			if len(delta.ToolCalls) > 0 {
				for _, tc := range delta.ToolCalls {
					targetIdx := 0
					if tc.Index != nil {
						targetIdx = *tc.Index
					}
					for len(accToolCalls) <= targetIdx {
						accToolCalls = append(accToolCalls, &arkmodel.ToolCall{})
					}
					if tc.ID != "" {
						accToolCalls[targetIdx].ID = tc.ID
					}
					if tc.Type != "" {
						accToolCalls[targetIdx].Type = tc.Type
					}
					if tc.Function.Name != "" {
						accToolCalls[targetIdx].Function.Name += tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						accToolCalls[targetIdx].Function.Arguments += tc.Function.Arguments
					}
				}
			}
		}

		// Emit final response
		if textBuffer.Len() > 0 || len(accToolCalls) > 0 || finishReason != "" || finalUsage != nil {
			if finishReason == "" {
				finishReason = arkmodel.FinishReasonStop
			}
			finalResp := m.buildArkFinalResponse(textBuffer.String(), reasoningBuffer.String(), accToolCalls, finalUsage, finishReason)
			yield(finalResp, nil)
		}
	}
}

// convertArkResponse converts a non-streaming ARK response to an LLMResponse.
func (m *arkModel) convertArkResponse(resp *arkmodel.ChatCompletionResponse) (*model.LLMResponse, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("ark: no choices in response")
	}

	choice := resp.Choices[0]
	msg := &choice.Message

	var parts []*genai.Part

	// Reasoning content
	if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
		parts = append(parts, &genai.Part{Text: *msg.ReasoningContent, Thought: true})
	}

	// Text content
	textContent := ""
	if msg.Content != nil && msg.Content.StringValue != nil {
		textContent = *msg.Content.StringValue
	}
	if textContent != "" {
		parts = append(parts, genai.NewPartFromText(textContent))
	}

	// Tool calls
	for _, tc := range msg.ToolCalls {
		if tc.ID == "" && tc.Function.Name == "" && tc.Function.Arguments == "" {
			continue
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return nil, fmt.Errorf("ark: failed to unmarshal tool arguments: %w", err)
		}
		part := genai.NewPartFromFunctionCall(tc.Function.Name, args)
		part.FunctionCall.ID = tc.ID
		parts = append(parts, part)
	}

	llmResp := &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: parts,
		},
		FinishReason:  mapFinishReason(string(choice.FinishReason)),
		UsageMetadata: buildArkUsageMetadata(&resp.Usage),
		CustomMetadata: map[string]any{
			"response_model": resp.Model,
		},
	}

	return llmResp, nil
}

// buildArkFinalResponse builds the final LLMResponse at end of stream.
func (m *arkModel) buildArkFinalResponse(text, reasoningText string, toolCalls []*arkmodel.ToolCall, usage *arkmodel.Usage, finishReason arkmodel.FinishReason) *model.LLMResponse {
	var parts []*genai.Part

	if reasoningText != "" {
		parts = append(parts, &genai.Part{Text: reasoningText, Thought: true})
	}

	if text != "" {
		parts = append(parts, genai.NewPartFromText(text))
	}

	for _, tc := range toolCalls {
		if tc.ID == "" && tc.Function.Name == "" && tc.Function.Arguments == "" {
			continue
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			continue
		}
		part := genai.NewPartFromFunctionCall(tc.Function.Name, args)
		part.FunctionCall.ID = tc.ID
		parts = append(parts, part)
	}

	return &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: parts,
		},
		FinishReason:  mapFinishReason(string(finishReason)),
		UsageMetadata: buildArkUsageMetadata(usage),
		CustomMetadata: map[string]any{
			"response_model": m.name,
		},
	}
}

// buildArkUsageMetadata converts ARK SDK Usage to genai usage metadata.
func buildArkUsageMetadata(usage *arkmodel.Usage) *genai.GenerateContentResponseUsageMetadata {
	if usage == nil {
		return nil
	}

	promptTokens := usage.PromptTokens
	completionTokens := usage.CompletionTokens
	totalTokens := usage.TotalTokens
	if totalTokens == 0 && (promptTokens > 0 || completionTokens > 0) {
		totalTokens = promptTokens + completionTokens
	}

	metadata := &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:     int32(promptTokens),
		CandidatesTokenCount: int32(completionTokens),
		TotalTokenCount:      int32(totalTokens),
	}
	if usage.PromptTokensDetails.CachedTokens > 0 {
		metadata.CachedContentTokenCount = int32(usage.PromptTokensDetails.CachedTokens)
	}
	return metadata
}
