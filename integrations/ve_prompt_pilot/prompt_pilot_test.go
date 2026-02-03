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
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/prompts"
	"github.com/volcengine/veadk-go/utils"
)

func TestNew(t *testing.T) {
	vePromptPilot := New()
	if utils.GetEnvWithDefault(common.AGENTPILOT_WORKSPACE_ID) == "" || utils.GetEnvWithDefault(common.AGENTPILOT_API_KEY) == "" {
		t.Skip()
	}
	prompt, err := vePromptPilot.Optimize(&prompts.AgentInfo{
		Name:        "weather_agent",
		Model:       defaultOptimizeModel,
		Instruction: "你是一个MBTI人格分析大师，负责根据用户提供的个人信息分析用户的MBTI人格。",
	},
		"", defaultOptimizeModel)
	if err != nil {
		log.Errorf("error:%v", err)
		return
	}

	log.Info(prompt)
}

type mockRoundTripper struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func TestVePromptPilot_Optimize_Mock(t *testing.T) {
	agentInfo := &prompts.AgentInfo{
		Name:        "test_agent",
		Instruction: "Initial instruction",
	}

	t.Run("Success", func(t *testing.T) {
		mockRespBody := `event: message
data: "Optimized "
event: message
data: "instruction"
event: usage
data: {"total_tokens": 50}
`
		client := &http.Client{
			Transport: &mockRoundTripper{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					assert.Equal(t, "POST", req.Method)
					assert.Contains(t, req.URL.String(), "/agent-pilot")
					assert.Equal(t, "Bearer test-api-key", req.Header.Get("Authorization"))

					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewBufferString(mockRespBody)),
						Header:     make(http.Header),
					}, nil
				},
			},
		}

		pilot := New(
			WithUrl("http://mock-url/agent-pilot"),
			WithAPIKey("test-api-key"),
			WithWorkspaceID("test-workspace"),
			WithHTTPClient(client),
		)

		prompt, err := pilot.Optimize(agentInfo, "Make it better", "test-model")
		assert.NoError(t, err)
		assert.Equal(t, "Optimized instruction", prompt)
	})

	t.Run("APIError", func(t *testing.T) {
		client := &http.Client{
			Transport: &mockRoundTripper{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusBadRequest,
						Body:       io.NopCloser(bytes.NewBufferString("Bad Request")),
						Header:     make(http.Header),
					}, nil
				},
			},
		}

		pilot := New(
			WithUrl("http://mock-url"),
			WithAPIKey("test-api-key"),
			WithWorkspaceID("test-workspace"),
			WithHTTPClient(client),
		)

		prompt, err := pilot.Optimize(agentInfo, "", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "API error (status 400)")
		assert.Empty(t, prompt)
	})

	t.Run("StreamError", func(t *testing.T) {
		mockRespBody := `event: error
data: Something went wrong
`
		client := &http.Client{
			Transport: &mockRoundTripper{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewBufferString(mockRespBody)),
						Header:     make(http.Header),
					}, nil
				},
			},
		}

		pilot := New(
			WithUrl("http://mock-url"),
			WithAPIKey("test-api-key"),
			WithWorkspaceID("test-workspace"),
			WithHTTPClient(client),
		)

		prompt, err := pilot.Optimize(agentInfo, "", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "prompt pilot generate error: Something went wrong")
		assert.Empty(t, prompt)
	})

	t.Run("ValidationError", func(t *testing.T) {
		pilot := New(
			WithUrl(""), // Invalid URL
		)
		_, err := pilot.Optimize(agentInfo, "", "")
		assert.Equal(t, ErrUrlValidationFailed, err)
	})
}
