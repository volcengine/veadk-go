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
	"strings"

	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/integrations/ve_sign"
	"github.com/volcengine/veadk-go/utils"
)

type getAppKeyResponse struct {
	Data struct {
		AppKey string `json:"app_key"`
	} `json:"data"`
}

func GetApmPlusToken(region string) (string, error) {
	log.Println("Fetching APMPlus token...")
	if region == "" {
		region = common.DEFAULT_REGION
	}

	accessKey := utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
	secretKey := utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	sessionToken := ""

	if accessKey == "" || secretKey == "" {
		// try to get from vefaas iam
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
	// APMPlus frontend required
	header["X-Apmplus-Region"] = strings.ReplaceAll(region, "-", "_")

	req := ve_sign.VeRequest{
		AK:      accessKey,
		SK:      secretKey,
		Method:  "POST",
		Scheme:  "https",
		Host:    "open.volcengineapi.com",
		Path:    "/",
		Service: "apmplus_server",
		Region:  region,
		Action:  "GetAppKey",
		Version: "2024-07-30",
		Header:  header,
		Body:    map[string]interface{}{},
	}

	respBody, err := req.DoRequest()
	if err != nil {
		return "", fmt.Errorf("failed to get APMPlus token: %w", err)
	}

	var resp getAppKeyResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("failed to unmarshal APMPlus token response: %w", err)
	}

	if resp.Data.AppKey == "" {
		return "", fmt.Errorf("failed to get APMPlus token: app_key not found in response")
	}

	log.Println("Successfully fetching APMPlus API Key.")
	return resp.Data.AppKey, nil
}
