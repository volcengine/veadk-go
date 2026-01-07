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

package veauth

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/integrations/ve_sign"
	"github.com/volcengine/veadk-go/utils"
)

type describeMemoryProjectDetailResult struct {
	Result struct {
		APIKeyInfos []struct {
			APIKeyID string `json:"APIKeyId"`
		} `json:"APIKeyInfos"`
	} `json:"Result"`
}

type describeAPIKeyDetailResult struct {
	Result struct {
		APIKeyValue string `json:"APIKeyValue"`
	} `json:"Result"`
}

func getAPIKeyIDByProjectID(projectID, accessKey, secretKey, sessionToken, region string) (string, error) {
	headers := make(map[string]string)
	if sessionToken != "" {
		headers["X-Security-Token"] = sessionToken
	}

	req := ve_sign.VeRequest{
		AK:      accessKey,
		SK:      secretKey,
		Method:  "POST",
		Scheme:  "https",
		Host:    "open.volcengineapi.com",
		Path:    "/",
		Service: "mem0",
		Region:  region,
		Action:  "DescribeMemoryProjectDetail",
		Version: "2025-10-10",
		Header:  headers,
		Body: map[string]string{
			"MemoryProjectId": projectID,
		},
	}

	respBody, err := req.DoRequest()
	if err != nil {
		return "", fmt.Errorf("failed to describe memory project detail: %w", err)
	}

	fmt.Println(string(respBody))
	var res describeMemoryProjectDetailResult
	if err := json.Unmarshal(respBody, &res); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(res.Result.APIKeyInfos) < 1 || res.Result.APIKeyInfos[0].APIKeyID == "" {
		return "", fmt.Errorf("failed to get mem0 api key id: %s", string(respBody))
	}

	return res.Result.APIKeyInfos[0].APIKeyID, nil
}

func getAPIKeyByAPIKeyID(projectID, apiKeyID, accessKey, secretKey, sessionToken, region string) (string, error) {
	headers := make(map[string]string)
	if sessionToken != "" {
		headers["X-Security-Token"] = sessionToken
	}

	req := ve_sign.VeRequest{
		AK:      accessKey,
		SK:      secretKey,
		Method:  "POST",
		Scheme:  "https",
		Host:    "open.volcengineapi.com",
		Path:    "/",
		Service: "mem0",
		Region:  region,
		Action:  "DescribeAPIKeyDetail",
		Version: "2025-10-10",
		Header:  headers,
		Body: map[string]string{
			"APIKeyId":        apiKeyID,
			"MemoryProjectId": projectID,
		},
	}

	respBody, err := req.DoRequest()
	if err != nil {
		return "", fmt.Errorf("failed to describe api key detail: %w", err)
	}

	fmt.Println(string(respBody))

	var res describeAPIKeyDetailResult
	if err := json.Unmarshal(respBody, &res); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if res.Result.APIKeyValue == "" {
		return "", fmt.Errorf("failed to get mem0 api key: %s", string(respBody))
	}

	return res.Result.APIKeyValue, nil
}

func GetVikingMem0Token(memoryProjectID string, region string) (string, error) {
	if region == "" {
		region = common.DEFAULT_REGION
	}
	log.Printf("Fetching Viking mem0 token...")

	accessKey := utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
	secretKey := utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	sessionToken := ""

	if accessKey == "" || secretKey == "" {
		// try to get from vefaas iam
		cred, err := GetCredentialFromVeFaaSIAM()
		if err != nil {
			return "", fmt.Errorf("failed to get credential from env or vefaas iam: %w", err)
		}
		accessKey = cred.AccessKeyID
		secretKey = cred.SecretAccessKey
		sessionToken = cred.SessionToken
	}

	apiKeyID, err := getAPIKeyIDByProjectID(memoryProjectID, accessKey, secretKey, sessionToken, region)
	if err != nil {
		return "", fmt.Errorf("failed to get mem0 api key id: %w", err)
	}

	return getAPIKeyByAPIKeyID(memoryProjectID, apiKeyID, accessKey, secretKey, sessionToken, region)
}
