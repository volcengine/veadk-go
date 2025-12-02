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
	"os"
	"strings"

	"veadk-go/consts"
)

type VeIAMCredential struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
}

func GetCredentialFromVeFaaSIAM() (VeIAMCredential, error) {
	b, err := os.ReadFile(consts.VEFAAS_IAM_CRIDENTIAL_PATH)
	if err != nil {
		return VeIAMCredential{}, err
	}
	var cred VeIAMCredential
	if err := json.Unmarshal(b, &cred); err != nil {
		return VeIAMCredential{}, err
	}
	return cred, nil
}

func RefreshAKSK(accessKey string, secretKey string) (VeIAMCredential, error) {
	if strings.TrimSpace(accessKey) != "" && strings.TrimSpace(secretKey) != "" {
		return VeIAMCredential{AccessKeyID: accessKey, SecretAccessKey: secretKey, SessionToken: ""}, nil
	}
	return GetCredentialFromVeFaaSIAM()
}
