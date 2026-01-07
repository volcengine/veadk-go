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

type listVesearchApiKeysResponse struct {
	Result struct {
		ApiKeyVos []struct {
			ApiKey string `json:"api_key"`
		} `json:"api_key_vos"`
	} `json:"Result"`
}

func GetVeSearchToken(region string) (string, error) {
	log.Println("Fetching VeSearch token...")
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

	req := ve_sign.VeRequest{
		AK:      accessKey,
		SK:      secretKey,
		Method:  "POST",
		Scheme:  "https",
		Host:    "open.volcengineapi.com",
		Path:    "/",
		Service: "content_customization",
		Region:  region,
		Action:  "ListAPIKeys",
		Version: "2025-01-01",
		Header:  header,
		Body: map[string]interface{}{
			"biz_scene": "search_agent",
			"page":      1,
			"rows":      10,
		},
	}

	respBody, err := req.DoRequest()
	if err != nil {
		return "", fmt.Errorf("failed to list api keys: %w", err)
	}

	var resp listVesearchApiKeysResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("failed to unmarshal list api keys response: %w", err)
	}

	if len(resp.Result.ApiKeyVos) == 0 {
		return "", fmt.Errorf("failed to get VeSearch token: empty api_key_vos")
	}

	token := resp.Result.ApiKeyVos[0].ApiKey
	if token == "" {
		return "", fmt.Errorf("failed to get VeSearch token: empty api_key")
	}

	log.Println("Fetching VeSearch token done.")
	return token, nil
}
