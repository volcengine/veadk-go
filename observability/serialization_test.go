package observability

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/genai"
)

func TestSerializeContentForTelemetry_Multimodal(t *testing.T) {
	content := &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			{Text: "hello"},
			{FileData: &genai.FileData{FileURI: "https://example.com/cat.jpg", MIMEType: "image/jpeg", DisplayName: "cat.jpg"}},
			{InlineData: &genai.Blob{MIMEType: "image/png", DisplayName: "chart.png", Data: []byte("png-bytes")}},
		},
	}

	serialized := serializeContentForTelemetry(content)
	assert.NotEmpty(t, serialized)

	var decoded map[string]any
	err := json.Unmarshal([]byte(serialized), &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "user", decoded["role"])

	parts, ok := decoded["parts"].([]any)
	assert.True(t, ok)
	assert.Len(t, parts, 3)

	imagePart, ok := parts[1].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "image_url", imagePart["type"])

	inlineImagePart, ok := parts[2].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "image_url", inlineImagePart["type"])
	imageURL, ok := inlineImagePart["image_url"].(map[string]any)
	assert.True(t, ok)
	assert.True(t, strings.HasPrefix(imageURL["url"].(string), "data:image/png;base64,"))
}

func TestSerializeContentForTelemetry_TextInlineData(t *testing.T) {
	content := &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			{InlineData: &genai.Blob{MIMEType: "text/plain", Data: []byte("inline text")}},
		},
	}

	serialized := serializeContentForTelemetry(content)
	assert.NotEmpty(t, serialized)
	assert.Contains(t, serialized, "inline text")
}

func TestSerializeContentForTelemetry_Nil(t *testing.T) {
	assert.Equal(t, "", serializeContentForTelemetry(nil))
}
