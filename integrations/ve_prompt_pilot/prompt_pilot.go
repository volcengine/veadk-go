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

package ve_prompt_pilot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/prompts"
	"github.com/volcengine/veadk-go/utils"
)

const (
	defaultOptimizeModel = "doubao-seed-1.6-251015"
	defaultHttpTimeout   = 120
)

var (
	ErrUrlValidationFailed         = errors.New("AGENTPILOT_API_URL environment variable is not set")
	ErrApiKeyValidationFailed      = errors.New("AGENTPILOT_API_KEY environment variable is not set")
	ErrWorkspaceIdValidationFailed = errors.New("AGENTPILOT_WORKSPACE_ID environment variable is not set")
)

// VePromptPilot handles prompt optimization interactions.
type VePromptPilot struct {
	url         string
	apiKey      string
	workspaceID string
	httpClient  *http.Client
}

// New creates a new VePromptPilot instance.
func New(opts ...func(*VePromptPilot)) *VePromptPilot {
	p := &VePromptPilot{
		url:         fmt.Sprintf("%s/agent-pilot?Version=2024-01-01&Action=GeneratePromptStream", utils.GetEnvWithDefault(common.AGENTPILOT_API_URL, configs.GetGlobalConfig().PromptPilot.Url, common.DEFAULT_AGENTPILOT_API_URL)),
		apiKey:      utils.GetEnvWithDefault(common.AGENTPILOT_API_KEY, configs.GetGlobalConfig().PromptPilot.ApiKey),
		workspaceID: utils.GetEnvWithDefault(common.AGENTPILOT_WORKSPACE_ID, configs.GetGlobalConfig().PromptPilot.WorkspaceId),
		httpClient: &http.Client{
			Timeout: time.Second * defaultHttpTimeout,
		},
	}

	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithUrl sets the url for the pilot.
func WithUrl(url string) func(*VePromptPilot) {
	return func(p *VePromptPilot) {
		p.url = url
	}
}

// WithAPIKey sets the API key for the pilot.
func WithAPIKey(apiKey string) func(*VePromptPilot) {
	return func(p *VePromptPilot) {
		p.apiKey = apiKey
	}
}

// WithWorkspaceID sets the workspace ID for the pilot.
func WithWorkspaceID(workspaceID string) func(*VePromptPilot) {
	return func(p *VePromptPilot) {
		p.workspaceID = workspaceID
	}
}

// WithHTTPClient sets the HTTP client for the pilot.
func WithHTTPClient(client *http.Client) func(*VePromptPilot) {
	return func(p *VePromptPilot) {
		p.httpClient = client
	}
}

// generatePromptRequest represents the JSON body for the API request.
type generatePromptRequest struct {
	RequestID     string  `json:"request_id"`
	WorkspaceID   string  `json:"workspace_id"`
	TaskType      string  `json:"task_type"`
	Rule          string  `json:"rule"`
	CurrentPrompt string  `json:"current_prompt,omitempty"`
	ModelName     string  `json:"model_name"`
	Temperature   float64 `json:"temperature"`
	TopP          float64 `json:"top_p"`
}

func (p *VePromptPilot) Valid() error {
	if p.url == "" {
		return ErrUrlValidationFailed
	}
	if p.apiKey == "" {
		return ErrApiKeyValidationFailed
	}
	if p.workspaceID == "" {
		return ErrWorkspaceIdValidationFailed
	}
	return nil
}

// Optimize optimizes the prompts for the given agents using the specified feedback and model.
func (p *VePromptPilot) Optimize(agentInfo *prompts.AgentInfo, feedback string, modelName string) (string, error) {
	if err := p.Valid(); err != nil {
		return "", err
	}

	if modelName == "" {
		modelName = defaultOptimizeModel
	}
	var finalPrompt string
	var taskDescription string
	var err error

	if feedback == "" {
		log.Info("Optimizing prompt without feedback.")
		taskDescription, err = prompts.RenderPromptWithTemplate(agentInfo)
	} else {
		log.Infof("Optimizing prompt with feedback: %s\n", feedback)
		taskDescription, err = prompts.RenderPromptFeedbackWithTemplate(agentInfo, feedback)
	}

	if err != nil {
		return "", fmt.Errorf("rendering optimization task description: %w", err)
	}

	//TaskType Enum
	//"DEFAULT"  # single turn task
	//"MULTIMODAL"  # visual reasoning single turn task
	//"DIALOG"  # multi turn dialog
	reqBody := &generatePromptRequest{
		RequestID:     uuid.New().String(),
		WorkspaceID:   p.workspaceID,
		TaskType:      "DIALOG",
		Rule:          taskDescription,
		CurrentPrompt: agentInfo.Instruction,
		ModelName:     modelName,
		Temperature:   1.0,
		TopP:          0.7,
	}

	var builder strings.Builder
	var usageTotal int
	for event, err := range p.generateStream(context.Background(), reqBody) {
		if err != nil {
			return "", fmt.Errorf("generateStream error: %w", err)
		}
		if event.Event == "message" {
			builder.WriteString(event.Data.Content)
		} else if event.Event == "usage" {
			usageTotal = event.Data.Usage.TotalTokens
		} else {
			eventStr, _ := json.Marshal(event)
			log.Infof("Unexpected event: %s\n", string(eventStr))
		}
	}

	finalPrompt = strings.ReplaceAll(builder.String(), "\\n", "\n")

	log.Infof("Optimized prompt is -----\n%s\n-----\n", finalPrompt)

	if usageTotal > 0 {
		log.Infof("Token usage: %d", usageTotal)
	} else {
		log.Info("[Warn]No usage data.")
	}

	return finalPrompt, nil
}

func (p *VePromptPilot) sendRequest(ctx context.Context, reqBody *generatePromptRequest) (*http.Response, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
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

func (p *VePromptPilot) generateStream(ctx context.Context, req *generatePromptRequest) iter.Seq2[*GeneratePromptStreamResponseChunk, error] {
	return func(yield func(*GeneratePromptStreamResponseChunk, error) bool) {
		httpResp, err := p.sendRequest(ctx, req)
		if err != nil {
			yield(nil, err)
			return
		}
		defer func() {
			_ = httpResp.Body.Close()
		}()

		scanner := bufio.NewScanner(httpResp.Body)

		var promptChunk *GeneratePromptStreamResponseChunk
		for scanner.Scan() {
			line := scanner.Text()
			decodedLine := strings.TrimSpace(line)
			promptChunk = parseEventStreamLine(decodedLine, promptChunk)
			if promptChunk != nil {
				hasContent := promptChunk.Data != nil && promptChunk.Data.Content != ""
				hasUsage := promptChunk.Data != nil && promptChunk.Data.Usage != nil
				hasError := promptChunk.Data != nil && promptChunk.Data.Error != ""

				if hasContent || hasUsage {
					yieldData := promptChunk
					promptChunk = nil
					yield(yieldData, nil)
					continue
				} else if hasError {
					yield(nil, fmt.Errorf("prompt pilot generate error: %s", promptChunk.Data.Error))
					continue
				} else {
					continue
				}
			}
		}

		if err := scanner.Err(); err != nil {
			yield(nil, fmt.Errorf("stream error: %w", err))
			return
		}
	}
}
