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

type getRawApiKeyResponse struct {
	Result struct {
		ApiKey string `json:"ApiKey"`
	} `json:"Result"`
}

type listApiKeysResponse struct {
	Result struct {
		Items []struct {
			ID   int    `json:"Id"`
			Name string `json:"Name"`
		} `json:"Items"`
	} `json:"Result"`
}

func GetArkToken(region string) (string, error) {
	if region == "" {
		region = common.DEFAULT_MODEL_REGION
	}
	log.Info("Fetching ARK token...")

	accessKey := utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
	secretKey := utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	sessionToken := ""

	if accessKey == "" || secretKey == "" {
		// try to get from vefaas iam
		cred, err := GetCredentialFromVeFaaSIAM()
		if err != nil {
			// If we can't get credentials from env or IAM, we can't proceed.
			// Here we return error immediately if IAM fails and no env vars.
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

	// ListApiKeys
	req1 := ve_sign.VeRequest{
		AK:      accessKey,
		SK:      secretKey,
		Method:  "POST",
		Scheme:  "https",
		Host:    "open.volcengineapi.com",
		Path:    "/",
		Service: "ark",
		Region:  region,
		Action:  "ListApiKeys",
		Version: "2024-01-01",
		Header:  header,
		Body: map[string]interface{}{
			"ProjectName": "default",
			"Filter":      map[string]interface{}{"AllowAll": true},
		},
	}

	resp1Body, err := req1.DoRequest()
	if err != nil {
		return "", fmt.Errorf("failed to list api keys: %w", err)
	}
	var listResp listApiKeysResponse
	if err := json.Unmarshal(resp1Body, &listResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal list api keys response: %w", err)
	}

	if len(listResp.Result.Items) == 0 {
		return "", fmt.Errorf("failed to get ARK api key list: empty items")
	}

	firstApiKeyId := listResp.Result.Items[0].ID
	log.Info("By default, VeADK fetches the first API Key in the list.")

	// GetRawApiKey
	req2 := ve_sign.VeRequest{
		AK:      accessKey,
		SK:      secretKey,
		Method:  http.MethodPost,
		Scheme:  "https",
		Host:    "open.volcengineapi.com",
		Path:    "/",
		Service: "ark",
		Region:  region,
		Action:  "GetRawApiKey",
		Version: "2024-01-01",
		Header:  header,
		Body: map[string]interface{}{
			"Id": firstApiKeyId,
		},
	}

	resp2Body, err := req2.DoRequest()
	if err != nil {
		return "", fmt.Errorf("failed to get raw api key: %w", err)
	}

	var getResp getRawApiKeyResponse
	if err := json.Unmarshal(resp2Body, &getResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal get raw api key response: %w", err)
	}

	apiKey := getResp.Result.ApiKey
	if apiKey == "" {
		return "", fmt.Errorf("failed to get ARK api key: key not found in response")
	}

	log.Info("Successfully fetched ARK API Key.")

	return apiKey, nil
}
