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
	"github.com/volcengine/veadk-go/log"

	"net/http"

	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/integrations/ve_sign"
	"github.com/volcengine/veadk-go/utils"
)

// speechListApiKeysResponse matches the JSON structure for the ListAPIKeys response
type speechListApiKeysResponse struct {
	Result struct {
		APIKeys []struct {
			APIKey string `json:"APIKey"`
		} `json:"APIKeys"`
	} `json:"Result"`
}

// GetSpeechToken fetches the Speech API Key
func GetSpeechToken(region string) (string, error) {
	// Default region if not provided
	if region == "" {
		region = "cn-beijing"
	}
	log.Info("Fetching speech token...")

	// 1. Try to get credentials from Environment Variables or Global Config
	accessKey := utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
	secretKey := utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	sessionToken := ""

	// 2. If not found, try to get from VeFaaS IAM
	if accessKey == "" || secretKey == "" {
		cred, err := GetCredentialFromVeFaaSIAM()
		if err != nil {
			return "", fmt.Errorf("failed to get credential from vefaas iam: %w", err)
		}
		accessKey = cred.AccessKeyID
		secretKey = cred.SecretAccessKey
		sessionToken = cred.SessionToken
	}

	header := make(map[string]string)
	if sessionToken != "" {
		header["X-Security-Token"] = sessionToken
	}

	// 3. Construct the signed request
	req := ve_sign.VeRequest{
		AK:      accessKey,
		SK:      secretKey,
		Method:  http.MethodPost,
		Scheme:  "https",
		Host:    "open.volcengineapi.com",
		Path:    "/",
		Service: "speech_saas_prod",
		Region:  region,
		Action:  "ListAPIKeys",
		Version: "2025-05-20",
		Header:  header,
		Body: map[string]interface{}{
			"ProjectName":   "default",
			"OnlyAvailable": true,
		},
	}

	// 4. Execute the request
	respBody, err := req.DoRequest()
	if err != nil {
		return "", fmt.Errorf("failed to list speech api keys: %w", err)
	}

	// 5. Parse the response
	var listResp speechListApiKeysResponse
	if err := json.Unmarshal(respBody, &listResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal speech list api keys response: %w", err)
	}

	if len(listResp.Result.APIKeys) == 0 {
		return "", fmt.Errorf("failed to get speech api key list: empty items. Response: %s", string(respBody))
	}

	firstApiKey := listResp.Result.APIKeys[0].APIKey
	log.Info("Successfully fetching speech API Key.")
	return firstApiKey, nil
}
