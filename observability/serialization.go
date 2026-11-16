package observability

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func serializeContentForTelemetry(content *genai.Content) string {
	if content == nil {
		return ""
	}

	payload := normalizeContentForTelemetry(content)
	if payload == nil {
		return ""
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(b)
}

func serializeLLMRequestForTelemetry(req *model.LLMRequest) string {
	if req == nil {
		return ""
	}

	contents := make([]map[string]any, 0, len(req.Contents))
	for _, content := range req.Contents {
		if normalized := normalizeContentForTelemetry(content); normalized != nil {
			contents = append(contents, normalized)
		}
	}

	payload := map[string]any{
		"model":    req.Model,
		"contents": contents,
	}

	if req.Config != nil && req.Config.SystemInstruction != nil {
		if normalized := normalizeContentForTelemetry(req.Config.SystemInstruction); normalized != nil {
			payload["system_instruction"] = normalized
		}
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(b)
}

func normalizeContentForTelemetry(content *genai.Content) map[string]any {
	if content == nil {
		return nil
	}

	parts := make([]map[string]any, 0, len(content.Parts))
	for _, part := range content.Parts {
		if part == nil {
			continue
		}
		normalized := normalizePartForTelemetry(part)
		if normalized != nil {
			parts = append(parts, normalized)
		}
	}

	payload := map[string]any{
		"role":  content.Role,
		"parts": parts,
	}

	return payload
}

func normalizePartForTelemetry(part *genai.Part) map[string]any {
	if part.Text != "" {
		return map[string]any{
			"type": "text",
			"text": part.Text,
		}
	}

	if part.FunctionCall != nil {
		return map[string]any{
			"type": "function_call",
			"id":   part.FunctionCall.ID,
			"name": part.FunctionCall.Name,
			"args": part.FunctionCall.Args,
		}
	}

	if part.FunctionResponse != nil {
		return map[string]any{
			"type":     "function_response",
			"id":       part.FunctionResponse.ID,
			"name":     part.FunctionResponse.Name,
			"response": part.FunctionResponse.Response,
		}
	}

	if part.FileData != nil {
		return normalizeFileDataForTelemetry(part.FileData)
	}

	if part.InlineData != nil {
		return normalizeInlineDataForTelemetry(part.InlineData)
	}

	return nil
}

func normalizeFileDataForTelemetry(file *genai.FileData) map[string]any {
	if file == nil {
		return nil
	}

	mimeType := file.MIMEType
	name := file.DisplayName
	url := file.FileURI

	if strings.HasPrefix(mimeType, "image/") {
		return map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"name": name,
				"url":  url,
			},
		}
	}

	if strings.HasPrefix(mimeType, "video/") {
		return map[string]any{
			"type": "video_url",
			"video_url": map[string]any{
				"name": name,
				"url":  url,
			},
		}
	}

	if strings.HasPrefix(mimeType, "audio/") {
		return map[string]any{
			"type": "audio_url",
			"audio_url": map[string]any{
				"name": name,
				"url":  url,
			},
		}
	}

	return map[string]any{
		"type": "file",
		"file": map[string]any{
			"name":      name,
			"url":       url,
			"mime_type": mimeType,
		},
	}
}

func normalizeInlineDataForTelemetry(blob *genai.Blob) map[string]any {
	if blob == nil {
		return nil
	}

	mimeType := blob.MIMEType
	name := blob.DisplayName

	if strings.HasPrefix(mimeType, "text/") {
		return map[string]any{
			"type": "text",
			"text": string(blob.Data),
		}
	}

	url := ""
	if len(blob.Data) > 0 && mimeType != "" {
		url = fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(blob.Data))
	}

	if strings.HasPrefix(mimeType, "image/") {
		return map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"name": name,
				"url":  url,
			},
		}
	}

	if strings.HasPrefix(mimeType, "video/") {
		return map[string]any{
			"type": "video_url",
			"video_url": map[string]any{
				"name": name,
				"url":  url,
			},
		}
	}

	if strings.HasPrefix(mimeType, "audio/") {
		return map[string]any{
			"type": "audio_url",
			"audio_url": map[string]any{
				"name": name,
				"url":  url,
			},
		}
	}

	return map[string]any{
		"type": "file",
		"file": map[string]any{
			"name":        name,
			"mime_type":   mimeType,
			"data_base64": url,
		},
	}
}
