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

package builtin_tools

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/volcengine/veadk-go/v2/common"
	"github.com/volcengine/veadk-go/v2/configs"
	agentkitclient "github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"github.com/volcengine/veadk-go/v2/utils"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

const (
	DefaultScheme  = "https"
	DefaultTimeout = 600
)

var ErrInvalidToolID = errors.New("agentkit tool id is invalid")

var runCodeToolDescription = `Run code in a code sandbox and return the output.
For C++ code, don't execute it directly, compile and execute via Python; write sources and object files to /tmp.`

func NewRunCodeSandboxTool() (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:          "run_code",
			Description:   runCodeToolDescription,
			IsLongRunning: true,
		},
		runCodeHandler)
}

type RunCodeArgs struct {
	Code     string `json:"code" jsonschema:"The code to run."`
	Language string `json:"language" jsonschema:"The programming language of the code. Language must be one of the supported languages: python3."`
	Timeout  uint   `json:"timeout" jsonschema:"The timeout in seconds for the code execution. Defaults to 30."`
}

func runCodeHandler(ctx agent.Context, args RunCodeArgs) (map[string]any, error) {
	var result = make(map[string]any)

	toolID := utils.GetEnvWithDefault(common.AGENTKIT_TOOL_ID_SCRIPT, common.AGENTKIT_TOOL_ID, configs.GetGlobalConfig().Tool.RunCode.ToolID)
	if toolID == "" {
		return result, ErrInvalidToolID
	}
	var toolUserSessionID = uuid.New().String()
	if ctx != nil {
		toolUserSessionID = fmt.Sprintf("%s_%s_%s", ctx.AgentName(), ctx.UserID(), ctx.SessionID())
	}

	if args.Timeout <= 0 {
		args.Timeout = DefaultTimeout
	}
	opPayloadBytes, _ := json.Marshal(args)

	reqBody := map[string]interface{}{
		"ToolId":           toolID,
		"UserSessionId":    toolUserSessionID,
		"OperationType":    "RunCode",
		"OperationPayload": string(opPayloadBytes),
	}

	client, err := agentkitclient.NewClient(ctx)
	if err != nil {
		return result, err
	}
	resp, err := client.Do("InvokeTool", reqBody)
	if err != nil {
		return result, err
	}

	if resultWrapper, ok := resp["Result"].(map[string]any); ok {
		if executionResult, ok := resultWrapper["Result"].(string); ok {
			result["result"] = executionResult
			return result, nil
		}
	}

	result["result"] = resp
	return result, nil
}

// RunCodeSandbox executes code through the AgentKit run_code sandbox using the
// same implementation as the built-in run_code tool.
func RunCodeSandbox(ctx agent.Context, args RunCodeArgs) (map[string]any, error) {
	return runCodeHandler(ctx, args)
}
