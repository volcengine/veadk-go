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
	"log"
	"os"
	"strings"

	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/utils"
)

type VeIAMCredential struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
}

func GetCredentialFromVeFaaSIAM(paths ...string) (VeIAMCredential, error) {
	path := common.VEFAAS_IAM_CRIDENTIAL_PATH
	if len(paths) > 0 {
		path = paths[0]
	}
	b, err := os.ReadFile(path)
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

func GetAuthInfo() (ak, sk, sessionToken string) {
	ak = utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
	sk = utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)

	if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
		iam, err := GetCredentialFromVeFaaSIAM()
		if err != nil {
			log.Printf("GetAuthInfo error: %s\n", err.Error())
		} else {
			ak = iam.AccessKeyID
			sk = iam.SecretAccessKey
			sessionToken = iam.SessionToken
		}
	}
	return
}
