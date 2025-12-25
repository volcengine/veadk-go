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
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/integrations/ve_sign"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
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

func runCodeHandler(ctx tool.Context, args RunCodeArgs) (map[string]any, error) {
	var result = make(map[string]any)

	toolID := utils.GetEnvWithDefault(common.AGENTKIT_TOOL_ID)
	if toolID == "" {
		return result, ErrInvalidToolID
	}
	service := utils.GetEnvWithDefault(common.AGENTKIT_TOOL_SERVICE_CODE, common.DEFAULT_AGENTKIT_TOOL_SERVICE_CODE)
	region := utils.GetEnvWithDefault(common.AGENTKIT_TOOL_REGION, common.DEFAULT_AGENTKIT_TOOL_REGION)
	host := utils.GetEnvWithDefault(common.AGENTKIT_TOOL_HOST, fmt.Sprintf("%s.%s.volces.com", service, region))

	var ak string
	var sk string
	var header map[string]string

	if ctx != nil {
		ak = utils.GetStringFromToolContext(ctx, common.VOLCENGINE_ACCESS_KEY)
		sk = utils.GetStringFromToolContext(ctx, common.VOLCENGINE_SECRET_KEY)
	}

	if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
		ak = utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
		sk = utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	}

	if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {

		iam, err := veauth.GetCredentialFromVeFaaSIAM()
		if err != nil {
			log.Warn(fmt.Sprintf("RunCodeTool : GetCredential error: %s", err.Error()))
		} else {
			ak = iam.AccessKeyID
			sk = iam.SecretAccessKey
			if strings.TrimSpace(iam.SessionToken) != "" {
				header = map[string]string{"X-Security-Token": iam.SessionToken}
			}
		}
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

	respBody, err := ve_sign.VeRequest{
		AK:      ak,
		SK:      sk,
		Method:  http.MethodPost,
		Scheme:  DefaultScheme,
		Host:    host,
		Path:    "/",
		Service: service,
		Region:  region,
		Action:  "InvokeTool",
		Version: "2025-10-30",
		Header:  header,
		Body:    reqBody,
	}.DoRequest()

	if err != nil {
		return result, err
	}

	var resp map[string]interface{}
	if err = json.Unmarshal(respBody, &resp); err != nil {
		return result, fmt.Errorf("RunCodeTool: unmarshal response %s failed: %w", respBody, err)
	}

	if resultWrapper, ok := resp["Result"].(map[string]interface{}); ok {
		if executionResult, ok := resultWrapper["Result"].(string); ok {
			result["result"] = executionResult
			return result, nil
		}
	}

	result["result"] = resp
	return result, nil
}
