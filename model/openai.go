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
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/volcengine/veadk-go/common"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

type ClientConfig struct {
	APIKey     string
	BaseURL    string
	ExtraBody  map[string]any
	HTTPClient *http.Client
}

type openAIModel struct {
	name       string
	config     *ClientConfig
	httpClient *http.Client
}

func NewOpenAIModel(ctx context.Context, modelName string, config *ClientConfig) (model.LLM, error) {
	_ = ctx

	if config == nil {
		config = &ClientConfig{}
	}

	if config.APIKey == "" {
		config.APIKey = os.Getenv(common.MODEL_AGENT_API_KEY)
		if config.APIKey == "" {
			return nil, fmt.Errorf("openai: API key not found, set MODEL_AGENT_API_KEY environment variable or provide config.APIKey")
		}
	}

	if config.BaseURL == "" {
		config.BaseURL = os.Getenv(common.MODEL_AGENT_API_BASE)
		if config.BaseURL == "" {
			return nil, fmt.Errorf("openai: base URL not found, set MODEL_AGENT_API_BASE environment variable or provide config.BaseURL")
		}
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &openAIModel{
		name:       modelName,
		config:     config,
		httpClient: httpClient,
	}, nil
}

func (m *openAIModel) Name() string {
	return m.name
}

func (m *openAIModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	m.maybeAppendUserContent(req)

	openaiReq, err := m.convertOpenAIRequest(req)
	if err != nil {
		return func(yield func(*model.LLMResponse, error) bool) {
			yield(nil, fmt.Errorf("failed to convert request: %w", err))
		}
	}
	if extraBody, ok := m.config.ExtraBody["extra_body"]; ok {
		openaiReq.ExtraBody = extraBody.(map[string]any)
	}

	if stream {
		return m.generateStream(ctx, openaiReq)
	}

	return m.generate(ctx, openaiReq)
}

type openAIRequest struct {
	Model          string          `json:"model"`
	Messages       []message       `json:"messages"`
	Tools          []tool          `json:"tools,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	TopP           *float64        `json:"top_p,omitempty"`
	Stop           []string        `json:"stop,omitempty"`
	Stream         bool            `json:"stream,omitempty"`
	StreamOptions  *streamOptions  `json:"stream_options,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	ExtraBody      map[string]any
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

func (r openAIRequest) MarshalJSON() ([]byte, error) {
	topLevel := make(map[string]interface{})

	topLevel["model"] = r.Model
	topLevel["messages"] = r.Messages
	if len(r.Tools) > 0 {
		topLevel["tools"] = r.Tools
	}
	if r.Temperature != nil {
		topLevel["temperature"] = *r.Temperature
	}
	if r.MaxTokens != nil {
		topLevel["max_tokens"] = *r.MaxTokens
	}
	if r.TopP != nil {
		topLevel["top_p"] = *r.TopP
	}
	if len(r.Stop) > 0 {
		topLevel["stop"] = r.Stop
	}
	if r.Stream {
		topLevel["stream"] = r.Stream
	}
	if r.ResponseFormat != nil {
		topLevel["response_format"] = r.ResponseFormat
	}
	if r.StreamOptions != nil {
		topLevel["stream_options"] = r.StreamOptions
	}

	if r.ExtraBody != nil {
		for k, v := range r.ExtraBody {
			topLevel[k] = v
		}
	}

	return json.MarshalIndent(topLevel, "", "  ")
}

type responseFormat struct {
	Type string `json:"type"`
}

type message struct {
	Role             string     `json:"role"`
	Content          any        `json:"content,omitempty"`
	ToolCalls        []toolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ReasoningContent any        `json:"reasoning_content,omitempty"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Index    *int         `json:"index,omitempty"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type tool struct {
	Type     string   `json:"type"`
	Function function `json:"function"`
}

type function struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type response struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
	Usage   *usage   `json:"usage,omitempty"`
}

type choice struct {
	Index        int      `json:"index"`
	Message      *message `json:"message,omitempty"`
	Delta        *message `json:"delta,omitempty"`
	FinishReason string   `json:"finish_reason,omitempty"`
}

type usage struct {
	PromptTokens        int                  `json:"prompt_tokens"`
	InputTokens         int                  `json:"input_tokens"` // Ark-compatible field
	CompletionTokens    int                  `json:"completion_tokens"`
	OutputTokens        int                  `json:"output_tokens"` // Ark-compatible field
	TotalTokens         int                  `json:"total_tokens"`
	PromptTokensDetails *promptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

type promptTokensDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
}

func (m *openAIModel) convertOpenAIRequest(req *model.LLMRequest) (*openAIRequest, error) {
	openaiReq := &openAIRequest{
		Model:    m.name,
		Messages: make([]message, 0),
	}

	if req.Config != nil && req.Config.SystemInstruction != nil {
		sysContent := extractTextFromContent(req.Config.SystemInstruction)
		if sysContent != "" {
			openaiReq.Messages = append(openaiReq.Messages, message{
				Role:    "system",
				Content: sysContent,
			})
		}
	}

	for _, content := range req.Contents {
		msgs, err := m.convertGenAIContent(content)
		if err != nil {
			return nil, fmt.Errorf("failed to convert content: %w", err)
		}
		openaiReq.Messages = append(openaiReq.Messages, msgs...)
	}

	if req.Config != nil && len(req.Config.Tools) > 0 {
		for _, tool := range req.Config.Tools {
			if tool.FunctionDeclarations != nil {
				for _, fn := range tool.FunctionDeclarations {
					openaiReq.Tools = append(openaiReq.Tools, convertFunctionDeclaration(fn))
				}
			}
		}
	}

	if req.Config != nil {
		if req.Config.Temperature != nil {
			temp := float64(*req.Config.Temperature)
			openaiReq.Temperature = &temp
		}
		if req.Config.MaxOutputTokens > 0 {
			maxTokens := int(req.Config.MaxOutputTokens)
			openaiReq.MaxTokens = &maxTokens
		}
		if req.Config.TopP != nil {
			topP := float64(*req.Config.TopP)
			openaiReq.TopP = &topP
		}
		if len(req.Config.StopSequences) > 0 {
			openaiReq.Stop = req.Config.StopSequences
		}
		if req.Config.ResponseMIMEType == "application/json" {
			openaiReq.ResponseFormat = &responseFormat{Type: "json_object"}
		}
	}

	openaiReq.StreamOptions = &streamOptions{IncludeUsage: true}

	return openaiReq, nil
}

func (m *openAIModel) convertGenAIContent(content *genai.Content) ([]message, error) {
	if content == nil || len(content.Parts) == 0 {
		return nil, nil
	}

	role := content.Role
	if role == "model" {
		role = "assistant"
	}

	var toolMessages []message
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
			toolMessages = append(toolMessages, message{
				Role:       "tool",
				Content:    string(responseJSON),
				ToolCallID: toolCallID,
			})
		}
	}
	if len(toolMessages) > 0 {
		return toolMessages, nil
	}

	var textParts []string
	var contentArray []map[string]any
	var toolCalls []toolCall

	for _, part := range content.Parts {
		if part.Text != "" {
			textParts = append(textParts, part.Text)
		} else if part.InlineData != nil && len(part.InlineData.Data) > 0 {
			mimeType := part.InlineData.MIMEType
			base64Data := base64.StdEncoding.EncodeToString(part.InlineData.Data)
			dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)

			if strings.HasPrefix(mimeType, "image/") {
				contentArray = append(contentArray, map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url": dataURI,
					},
				})
			} else if strings.HasPrefix(mimeType, "video/") {
				contentArray = append(contentArray, map[string]any{
					"type": "video_url",
					"video_url": map[string]any{
						"url": dataURI,
					},
				})
			} else if strings.HasPrefix(mimeType, "audio/") {
				contentArray = append(contentArray, map[string]any{
					"type": "audio_url",
					"audio_url": map[string]any{
						"url": dataURI,
					},
				})
			} else if mimeType == "application/pdf" || mimeType == "application/json" {
				contentArray = append(contentArray, map[string]any{
					"type": "file",
					"file": map[string]any{
						"file_data": dataURI,
					},
				})
			} else if strings.HasPrefix(mimeType, "text/") {
				textParts = append(textParts, string(part.InlineData.Data))
			}
		} else if part.FileData != nil && part.FileData.FileURI != "" {
			contentArray = append(contentArray, map[string]any{
				"type": "file",
				"file": map[string]any{
					"file_id": part.FileData.FileURI,
				},
			})
		} else if part.FunctionCall != nil {
			argsJSON, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal function args: %w", err)
			}
			callID := part.FunctionCall.ID
			if callID == "" {
				callID = "call_" + uuid.New().String()[:8]
			}
			toolCalls = append(toolCalls, toolCall{
				ID:   callID,
				Type: "function",
				Function: functionCall{
					Name:      part.FunctionCall.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}

	msg := message{Role: role}

	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
		if len(textParts) > 0 {
			msg.Content = strings.Join(textParts, "\n")
		}
	} else if len(contentArray) > 0 {
		textMaps := make([]map[string]any, len(textParts))
		for i, text := range textParts {
			textMaps[i] = map[string]any{
				"type": "text",
				"text": text,
			}
		}
		msg.Content = append(textMaps, contentArray...)
	} else if len(textParts) > 0 {
		msg.Content = strings.Join(textParts, "\n")
	}

	return []message{msg}, nil
}

func extractTextFromContent(content *genai.Content) string {
	if content == nil {
		return ""
	}
	var texts []string
	for _, part := range content.Parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func convertFunctionDeclaration(fn *genai.FunctionDeclaration) tool {
	params := convertFunctionParameters(fn)

	return tool{
		Type: "function",
		Function: function{
			Name:        fn.Name,
			Description: fn.Description,
			Parameters:  params,
		},
	}
}

func convertFunctionParameters(fn *genai.FunctionDeclaration) map[string]any {
	if fn.ParametersJsonSchema != nil {
		if params := tryConvertJsonSchema(fn.ParametersJsonSchema); params != nil {
			return params
		}
	}

	if fn.Parameters != nil {
		return convertLegacyParameters(fn.Parameters)
	}

	return make(map[string]any)
}

func tryConvertJsonSchema(schema any) map[string]any {
	if params, ok := schema.(map[string]any); ok {
		return params
	}

	jsonBytes, err := json.Marshal(schema)
	if err != nil {
		return nil
	}

	var params map[string]any
	if err := json.Unmarshal(jsonBytes, &params); err != nil {
		return nil
	}

	return params
}

func convertLegacyParameters(schema *genai.Schema) map[string]any {
	params := map[string]any{
		"type": "object",
	}

	if schema.Properties != nil {
		props := make(map[string]any)
		for k, v := range schema.Properties {
			props[k] = schemaToMap(v)
		}
		params["properties"] = props
	}

	if len(schema.Required) > 0 {
		params["required"] = schema.Required
	}

	return params
}

func schemaToMap(schema *genai.Schema) map[string]any {
	result := make(map[string]any)
	if schema.Type != genai.TypeUnspecified {
		result["type"] = strings.ToLower(string(schema.Type))
	}
	if schema.Description != "" {
		result["description"] = schema.Description
	}
	if schema.Items != nil {
		result["items"] = schemaToMap(schema.Items)
	}
	if schema.Properties != nil {
		props := make(map[string]any)
		for k, v := range schema.Properties {
			props[k] = schemaToMap(v)
		}
		result["properties"] = props
	}
	if len(schema.Enum) > 0 {
		result["enum"] = schema.Enum
	}
	return result
}

func (m *openAIModel) generate(ctx context.Context, openaiReq *openAIRequest) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		resp, err := m.doRequest(ctx, openaiReq)
		if err != nil {
			yield(nil, err)
			return
		}

		llmResp, err := m.convertResponse(resp)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(llmResp, nil)
	}
}

func (m *openAIModel) generateStream(ctx context.Context, openaiReq *openAIRequest) iter.Seq2[*model.LLMResponse, error] {
	openaiReq.Stream = true

	return func(yield func(*model.LLMResponse, error) bool) {
		httpResp, err := m.sendRequest(ctx, openaiReq)
		if err != nil {
			yield(nil, err)
			return
		}
		defer func() {
			_ = httpResp.Body.Close()
		}()

		scanner := bufio.NewScanner(httpResp.Body)
		// Set a larger buffer for the scanner to handle long SSE lines
		const maxScannerBuffer = 1 * 1024 * 1024 // 1MB
		scanner.Buffer(make([]byte, 64*1024), maxScannerBuffer)

		var textBuffer strings.Builder
		var reasoningBuffer strings.Builder
		var toolCalls []toolCall
		var finalUsage usage
		var usageFound bool
		var finishedReason string

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var chunk response
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			if chunk.Usage != nil {
				finalUsage = *chunk.Usage
				usageFound = true
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			choice := chunk.Choices[0]
			if choice.FinishReason != "" {
				finishedReason = choice.FinishReason
			}
			delta := choice.Delta
			if delta == nil {
				continue
			}

			if delta.ReasoningContent != nil {
				if text, ok := delta.ReasoningContent.(string); ok && text != "" {
					reasoningBuffer.WriteString(text)
					llmResp := &model.LLMResponse{
						Content: &genai.Content{
							Role: "model",
							Parts: []*genai.Part{
								{Text: text, Thought: true},
							},
						},
						Partial: true,
					}
					if !yield(llmResp, nil) {
						return
					}
				}
			}

			if delta.Content != nil {
				if text, ok := delta.Content.(string); ok && text != "" {
					textBuffer.WriteString(text)
					llmResp := &model.LLMResponse{
						Content: &genai.Content{
							Role: "model",
							Parts: []*genai.Part{
								{Text: text},
							},
						},
						Partial: true,
					}
					if !yield(llmResp, nil) {
						return
					}
				}
			}

			if len(delta.ToolCalls) > 0 {
				for _, tc := range delta.ToolCalls {
					targetIdx := 0
					if tc.Index != nil {
						targetIdx = *tc.Index
					}
					for len(toolCalls) <= targetIdx {
						toolCalls = append(toolCalls, toolCall{})
					}
					if tc.ID != "" {
						toolCalls[targetIdx].ID = tc.ID
					}
					if tc.Type != "" {
						toolCalls[targetIdx].Type = tc.Type
					}
					if tc.Function.Name != "" {
						toolCalls[targetIdx].Function.Name += tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						toolCalls[targetIdx].Function.Arguments += tc.Function.Arguments
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			yield(nil, fmt.Errorf("stream error: %w", err))
			return
		}

		if textBuffer.Len() > 0 || len(toolCalls) > 0 || finishedReason != "" || usageFound {
			var u *usage
			if usageFound {
				u = &finalUsage
			}
			if finishedReason == "" {
				finishedReason = "stop"
			}
			finalResp := m.buildFinalResponse(textBuffer.String(), reasoningBuffer.String(), toolCalls, u, finishedReason)
			yield(finalResp, nil)
		}
	}
}

func (m *openAIModel) sendRequest(ctx context.Context, openaiReq *openAIRequest) (*http.Response, error) {
	reqBody, err := openaiReq.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	baseURL := strings.TrimSuffix(m.config.BaseURL, "/")
	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.config.APIKey)
	httpResp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		if err = httpResp.Body.Close(); err != nil {
			return nil, fmt.Errorf("API failed to close response body: %w", err)
		}
		return nil, fmt.Errorf("API error (status %d): %s", httpResp.StatusCode, string(body))
	}

	return httpResp, nil
}

func (m *openAIModel) doRequest(ctx context.Context, openaiReq *openAIRequest) (*response, error) {
	httpResp, err := m.sendRequest(ctx, openaiReq)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()

	var resp response
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &resp, nil
}

func (m *openAIModel) convertResponse(resp *response) (*model.LLMResponse, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := resp.Choices[0]
	msg := choice.Message
	if msg == nil {
		return nil, fmt.Errorf("no message in choice")
	}

	var parts []*genai.Part

	if reasoningParts := extractReasoningParts(msg.ReasoningContent); len(reasoningParts) > 0 {
		parts = append(parts, reasoningParts...)
	}

	toolCalls := msg.ToolCalls
	textContent := ""
	if msg.Content != nil {
		if text, ok := msg.Content.(string); ok {
			textContent = text
		}
	}

	if len(toolCalls) == 0 && textContent != "" {
		parsedCalls, remainder := parseToolCallsFromText(textContent)
		if len(parsedCalls) > 0 {
			toolCalls = parsedCalls
			textContent = remainder
		}
	}

	if textContent != "" {
		parts = append(parts, genai.NewPartFromText(textContent))
	}

	for _, tc := range toolCalls {
		if tc.ID == "" && tc.Function.Name == "" && tc.Function.Arguments == "" {
			continue
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tools arguments: %w", err)
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
		CustomMetadata: map[string]any{
			"response_model": resp.Model,
		},
	}

	llmResp.UsageMetadata = buildUsageMetadata(resp.Usage)
	llmResp.FinishReason = mapFinishReason(choice.FinishReason)

	return llmResp, nil
}

func (m *openAIModel) buildFinalResponse(text string, reasoningText string, toolCalls []toolCall, usage *usage, finishReason string) *model.LLMResponse {
	var parts []*genai.Part

	if reasoningText != "" {
		parts = append(parts, &genai.Part{
			Text:    reasoningText,
			Thought: true,
		})
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

	llmResp := &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: parts,
		},
		FinishReason:  mapFinishReason(finishReason),
		UsageMetadata: buildUsageMetadata(usage),
		CustomMetadata: map[string]any{
			"response_model": m.name,
		},
	}

	return llmResp
}

func buildUsageMetadata(usage *usage) *genai.GenerateContentResponseUsageMetadata {
	if usage == nil {
		return nil
	}

	promptTokens := usage.PromptTokens
	if promptTokens == 0 {
		promptTokens = usage.InputTokens
	}
	completionTokens := usage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = usage.OutputTokens
	}
	totalTokens := usage.TotalTokens
	if totalTokens == 0 && (promptTokens > 0 || completionTokens > 0) {
		totalTokens = promptTokens + completionTokens
	}

	metadata := &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:     int32(promptTokens),
		CandidatesTokenCount: int32(completionTokens),
		TotalTokenCount:      int32(totalTokens),
	}
	if usage.PromptTokensDetails != nil {
		metadata.CachedContentTokenCount = int32(usage.PromptTokensDetails.CachedTokens)
	}
	return metadata
}

func extractReasoningParts(reasoningContent any) []*genai.Part {
	if reasoningContent == nil {
		return nil
	}

	var parts []*genai.Part
	extractTexts(reasoningContent, &parts)
	return parts
}

func extractTexts(value any, parts *[]*genai.Part) {
	if value == nil {
		return
	}

	switch v := value.(type) {
	case string:
		if v != "" {
			*parts = append(*parts, &genai.Part{Text: v, Thought: true})
		}
	case []any:
		for _, item := range v {
			extractTexts(item, parts)
		}
	case map[string]any:
		for _, key := range []string{"text", "content", "reasoning", "reasoning_content"} {
			if text, ok := v[key].(string); ok && text != "" {
				*parts = append(*parts, &genai.Part{Text: text, Thought: true})
			}
		}
	}
}

func parseToolCallsFromText(text string) ([]toolCall, string) {
	if text == "" {
		return nil, ""
	}

	var toolCalls []toolCall
	var remainder strings.Builder
	cursor := 0

	for cursor < len(text) {
		braceIndex := strings.Index(text[cursor:], "{")
		if braceIndex == -1 {
			remainder.WriteString(text[cursor:])
			break
		}
		braceIndex += cursor

		remainder.WriteString(text[cursor:braceIndex])

		var candidate map[string]any
		decoder := json.NewDecoder(strings.NewReader(text[braceIndex:]))
		if err := decoder.Decode(&candidate); err != nil {
			remainder.WriteString(text[braceIndex : braceIndex+1])
			cursor = braceIndex + 1
			continue
		}

		endPos := braceIndex + int(decoder.InputOffset())

		name, hasName := candidate["name"].(string)
		args, hasArgs := candidate["arguments"]
		if hasName && hasArgs {
			argsStr := ""
			switch a := args.(type) {
			case string:
				argsStr = a
			default:
				if jsonBytes, err := json.Marshal(args); err == nil {
					argsStr = string(jsonBytes)
				}
			}

			callID := "call_" + uuid.New().String()[:8]
			if id, ok := candidate["id"].(string); ok && id != "" {
				callID = id
			}

			toolCalls = append(toolCalls, toolCall{
				ID:   callID,
				Type: "function",
				Function: functionCall{
					Name:      name,
					Arguments: argsStr,
				},
			})
		} else {
			remainder.WriteString(text[braceIndex:endPos])
		}
		cursor = endPos
	}

	return toolCalls, strings.TrimSpace(remainder.String())
}

func mapFinishReason(reason string) genai.FinishReason {
	switch reason {
	case "stop":
		return genai.FinishReasonStop
	case "length":
		return genai.FinishReasonMaxTokens
	case "tool_calls", "function_call":
		return genai.FinishReasonStop
	case "content_filter":
		return genai.FinishReasonSafety
	default:
		return genai.FinishReasonOther
	}
}

func (m *openAIModel) maybeAppendUserContent(req *model.LLMRequest) {
	if len(req.Contents) == 0 {
		req.Contents = append(req.Contents, genai.NewContentFromText("Handle the requests as specified in the System Instruction.", "user"))
		return
	}

	if last := req.Contents[len(req.Contents)-1]; last != nil && last.Role != "user" {
		req.Contents = append(req.Contents, genai.NewContentFromText("Continue processing previous requests as instructed. Exit or provide a summary if no more outputs are needed.", "user"))
	}
}
