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
	"encoding/json"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// mapFinishReason converts an OpenAI/ARK finish reason string to a genai.FinishReason.
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

// maybeAppendUserContent ensures the request ends with a user message,
// which is required by some models.
func maybeAppendUserContent(req *model.LLMRequest) {
	if len(req.Contents) == 0 {
		req.Contents = append(req.Contents, genai.NewContentFromText("Handle the requests as specified in the System Instruction.", "user"))
		return
	}

	if last := req.Contents[len(req.Contents)-1]; last != nil && last.Role != "user" {
		req.Contents = append(req.Contents, genai.NewContentFromText("Continue processing previous requests as instructed. Exit or provide a summary if no more outputs are needed.", "user"))
	}
}

// extractTextFromContent extracts all text parts from a genai.Content and joins them.
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

// convertFunctionParameters converts a genai.FunctionDeclaration's parameters to a map.
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
