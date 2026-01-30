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

package prompts

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/coze-dev/cozeloop-go"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/utils"
)

var (
	ErrNilCozeLoopWorkspaceID = errors.New("coze loop workspace id is nil, Please configure it via environment COZELOOP_WORKSPACE_ID")
	ErrNilCozeLoopApiToken    = errors.New("coze loop api token is nil, Please configure it via environment COZELOOP_API_TOKEN")
)

type PromptGetParam struct {
	PromptKey string
	Version   string
	Label     string
}

type BasePromptManager interface {
	GetPrompt(args *PromptGetParam) string
}

type CozeLoopPromptManager struct {
	client cozeloop.Client
}

// NewCozeLoopPromptManager creates a new instance of CozeLoopPromptManager
func NewCozeLoopPromptManager() (*CozeLoopPromptManager, error) {
	workspaceId := utils.GetEnvWithDefault(common.COZELOOP_WORKSPACE_ID, configs.GetGlobalConfig().CozeLoopConfig.WorkspaceId)
	if strings.TrimSpace(workspaceId) == "" {
		return nil, ErrNilCozeLoopWorkspaceID
	}
	apiToken := utils.GetEnvWithDefault(common.COZELOOP_API_TOKEN, configs.GetGlobalConfig().CozeLoopConfig.ApiToken)
	if strings.TrimSpace(apiToken) == "" {
		return nil, ErrNilCozeLoopApiToken
	}
	client, err := cozeloop.NewClient(
		cozeloop.WithPromptTrace(true),
		cozeloop.WithWorkspaceID(workspaceId),
		cozeloop.WithAPIToken(apiToken),
	)
	if err != nil {
		return nil, fmt.Errorf("NewCozeLoopPromptManager failed: %v", err)
	}
	return &CozeLoopPromptManager{
		client: client,
	}, nil
}

// GetPrompt retrieves the prompt from CozeLoop
// 参数说明 ：https://loop.coze.cn/open/docs/cozeloop/prompt-version-tag-for-go-sdk
// promptKey, version, label string
func (m *CozeLoopPromptManager) GetPrompt(args *PromptGetParam) string {

	if m.client == nil {
		log.Println("CozeLoop client is not initialized")
		return DEFAULT_INSTRUCTION
	}

	pmt, err := m.client.GetPrompt(context.Background(), cozeloop.GetPromptParam{
		PromptKey: args.PromptKey,
		Version:   args.Version,
		Label:     args.Label,
	})

	// Check if prompt is valid and has content
	if err == nil &&
		pmt != nil &&
		pmt.PromptTemplate != nil &&
		len(pmt.PromptTemplate.Messages) > 0 &&
		pmt.PromptTemplate.Messages[0].Content != nil {
		return *pmt.PromptTemplate.Messages[0].Content
	}

	log.Printf("Prompt %s version %s label %s not found, get prompt result is %v, return default instruction\n",
		args.PromptKey, args.Version, args.Label, pmt)

	return DEFAULT_INSTRUCTION
}
