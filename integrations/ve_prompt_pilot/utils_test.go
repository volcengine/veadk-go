package ve_prompt_pilot

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseEventStreamLine_MockData(t *testing.T) {
	// Mock data provided by user
	mockLines := []string{
		"event: message",
		`data: "# Role  \nYou are an MBTI personality analysis master,"`,
		"",
		"event: message",
		`data: " specializing in analyzing users' MBTI personality types based on the personal"`,
		"",
		"event: message",
		`data: " information they provide."`,
		"",
		"event: message",
		`data: "  \n\n# Task Requirements  \n## Core Workflow  \n1."`,
		"",
		"event: message",
		`data: " **Collect Information**: First,"`,
		"",
		"event: message",
		`data: " listen carefully to the personal information users share (such as daily"`,
		"",
		"event: message",
		`data: " behavior,"`,
		"",
		"event: message",
		`data: " decision-making habits,"`,
		"",
		"event: message",
		`data: " social preferences,"`,
		"",
		"event: message",
		`data: " work/study styles,"`,
		"",
		"event: message",
		`data: " emotional responses,"`,
		"",
		"event: message",
		`data: " etc.). If the information provided is insufficient to make an accurate"`,
		"",
		"event: message",
		`data: " judgment,"`,
		"",
		"event: message",
		`data: " proactively ask targeted follow-up questions (e.g., \"Can you tell me how"`,
		"",
		"event: message",
		`data: " you usually spend your weekends?\" or \"When making an important decision"`,
		"",
		"event: message",
		`data: ","`,
		"",
		"event: message",
		`data: " do you prefer to rely on logical analysis or intuitive feelings?"`,
		"",
		"event: message",
		`data: "\").  \n2."`,
		"",
		"event: message",
		`data: " **Analyze Personality**: Based on the collected information,"`,
		"",
		"event: message",
		`data: " combine the core dimensions of MBTI (Extraversion/Introversion,"`,
		"",
		"event: message",
		`data: " Sensing/Intuition,"`,
		"",
		"event: message",
		`data: " Thinking/Feeling,"`,
		"",
		"event: message",
		`data: " Judging/Perceiving) to conduct a professional and rational analysis."`,
		"",
		"event: message",
		`data: "  \n3."`,
		"",
		"event: message",
		`data: " **Present Results**: Clearly state the inferred MBTI type and explain"`,
		"",
		"event: message",
		`data: " the reasoning in simple,"`,
		"",
		"event: message",
		`data: " easy-to-understand language—linking the analysis directly to the specific"`,
		"",
		"event: message",
		`data: " details the user provided."`,
		"",
		"event: message",
		`data: "  \n\n## Communication Style  \n- Maintain a professional yet approachable"`,
		"",
		"event: message",
		`data: " tone;"`,
		"",
		"event: message",
		`data: " avoid using overly obscure psychological jargon."`,
		"",
		"event: message",
		`data: "  \n- Ensure the analysis is objective and evidence-based,"`,
		"",
		"event: message",
		`data: " not relying on subjective assumptions beyond the user’s stated information"`,
		"",
		"event: message",
		`data: "."`,
		"",
		"event: message",
		`data: "  \n\n# Output Guidelines  \nEach analysis should include:  \n1."`,
		"",
		"event: message",
		`data: " The inferred MBTI type."`,
		"",
		"event: message",
		`data: "  \n2."`,
		"",
		"event: message",
		`data: " A breakdown of how the user’s behaviors align with each dimension of the"`,
		"",
		"event: message",
		`data: " type."`,
		"",
		"event: message",
		`data: "  \n3."`,
		"",
		"event: message",
		`data: " A brief,"`,
		"",
		"event: message",
		`data: " relatable summary of the type’s typical traits to help the user understand"`,
		"",
		"event: message",
		`data: " better."`,
		"",
		"event: message",
		`data: "  \n\nExample: If a user says,"`,
		"",
		"event: message",
		`data: " \"I love planning every detail of my trips in advance,"`,
		"",
		"event: message",
		`data: " prefer working alone on projects,"`,
		"",
		"event: message",
		`data: " and often make decisions based on whether they feel fair to others,\" your"`,
		"",
		"event: message",
		`data: " analysis might include:  \n- Inferred type: ISFJ  \n- Reasoning: \"Prefer"`,
		"",
		"event: message",
		`data: " planning details\" aligns with Judging;"`,
		"",
		"event: message",
		`data: " \"work alone\" suggests Introversion;"`,
		"",
		"event: message",
		`data: " \"focus on fairness to others\" reflects Feeling;"`,
		"",
		"event: message",
		`data: " \"attention to specific trip details\" indicates Sensing."`,
		"",
		"event: message",
		`data: "  \n- Summary: ISFJs are often caring,"`,
		"",
		"event: message",
		`data: " detail-oriented,"`,
		"",
		"event: message",
		`data: " and value stability in their lives."`,
		"",
		"event: usage",
		`data: {"total_tokens": 3807}`,
		"",
		"event: usage",
		`data: {"total_tokens": 3807}`,
		"",
	}

	var currentChunk *GeneratePromptStreamResponseChunk
	var fullContent strings.Builder
	var lastUsage *Usage

	for _, line := range mockLines {
		result := parseEventStreamLine(line, currentChunk)
		if result != nil {
			currentChunk = result

			// If we have content, append it
			if currentChunk.Event == "message" && currentChunk.Data != nil && currentChunk.Data.Content != "" {
				fullContent.WriteString(currentChunk.Data.Content)
				// Reset content to avoid double counting if we process the same chunk object again (though parseEventStreamLine creates new chunks for events)
				// Actually, parseEventStreamLine updates the *same* chunk object when parsing data.
				// However, since we're iterating line by line, and the mock data has event -> data -> empty -> event pattern.
				// Each "event: message" creates a NEW chunk.
				// Then "data: ..." fills it.
				// So we should capture the content when it's filled.
			}

			if currentChunk.Event == "usage" && currentChunk.Data != nil && currentChunk.Data.Usage != nil {
				lastUsage = currentChunk.Data.Usage
			}
		}
	}

	// Verify the assembled content contains expected parts
	expectedParts := []string{
		"# Role",
		"MBTI personality analysis master",
		"Task Requirements",
		"Collect Information",
		"Analyze Personality",
		"Present Results",
		"Communication Style",
		"Output Guidelines",
		"Example: If a user says",
		"ISFJ",
	}

	gotContent := fullContent.String()
	for _, part := range expectedParts {
		assert.Contains(t, gotContent, part, "Content should contain: "+part)
	}

	// Verify usage
	assert.NotNil(t, lastUsage)
	if lastUsage != nil {
		assert.Equal(t, 3807, lastUsage.TotalTokens)
	}
}

func TestParseEventStreamLine_Error(t *testing.T) {
	mockLines := []string{
		"event: error",
		"data: Something went wrong",
		"",
	}

	var currentChunk *GeneratePromptStreamResponseChunk
	var errorMsg string

	for _, line := range mockLines {
		result := parseEventStreamLine(line, currentChunk)
		if result != nil {
			currentChunk = result
			if currentChunk.Event == "error" && currentChunk.Data != nil && currentChunk.Data.Error != "" {
				errorMsg = currentChunk.Data.Error
			}
		}
	}

	assert.Equal(t, "Something went wrong", errorMsg)
}
