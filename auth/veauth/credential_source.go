// Copyright (c) 2025 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package veauth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/volcengine/veadk-go/v2/common"
)

// CredentialSource resolves a Role credential when it is first needed. It is
// intentionally smaller than any specific cloud SDK credential provider so it
// can be reused by model, Skill, and other control-plane clients.
type CredentialSource interface {
	Credential(ctx context.Context) (VeIAMCredential, error)
}

// CredentialSourceFunc adapts a function to CredentialSource.
type CredentialSourceFunc func(context.Context) (VeIAMCredential, error)

func (f CredentialSourceFunc) Credential(ctx context.Context) (VeIAMCredential, error) {
	return f(ctx)
}

// RoleCredentialSourceConfig configures the default Role credential source.
// Explicit AK/SK values take priority, followed by process environment values,
// and finally the mounted VeFaaS IAM credential file.
type RoleCredentialSourceConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	CredentialPath  string
	// EnvironmentPrefix selects VOLCENGINE_* or BYTEPLUS_* variables when
	// explicit credentials are absent. It defaults to VOLCENGINE.
	EnvironmentPrefix string
}

type roleCredentialSource struct {
	config RoleCredentialSourceConfig
}

// NewRoleCredentialSource creates a lazy source. It does not inspect the
// environment or filesystem until Credential is called.
func NewRoleCredentialSource(config RoleCredentialSourceConfig) CredentialSource {
	if strings.TrimSpace(config.CredentialPath) == "" {
		config.CredentialPath = common.VEFAAS_IAM_CRIDENTIAL_PATH
	}
	config.EnvironmentPrefix = strings.ToUpper(strings.TrimSpace(config.EnvironmentPrefix))
	if config.EnvironmentPrefix == "" {
		config.EnvironmentPrefix = "VOLCENGINE"
	}
	return &roleCredentialSource{config: config}
}

func (s *roleCredentialSource) Credential(ctx context.Context) (VeIAMCredential, error) {
	if err := ctx.Err(); err != nil {
		return VeIAMCredential{}, err
	}

	credential := VeIAMCredential{
		AccessKeyID:     strings.TrimSpace(s.config.AccessKeyID),
		SecretAccessKey: strings.TrimSpace(s.config.SecretAccessKey),
		SessionToken:    strings.TrimSpace(s.config.SessionToken),
	}
	explicitSessionToken := credential.SessionToken
	switch {
	case credential.AccessKeyID != "" && credential.SecretAccessKey != "":
		return credential, nil
	case credential.AccessKeyID != "" || credential.SecretAccessKey != "":
		return VeIAMCredential{}, errors.New("Role credential requires both access key ID and secret access key")
	}

	switch s.config.EnvironmentPrefix {
	case "VOLCENGINE":
		credential.AccessKeyID = strings.TrimSpace(os.Getenv(common.VOLCENGINE_ACCESS_KEY))
		credential.SecretAccessKey = strings.TrimSpace(os.Getenv(common.VOLCENGINE_SECRET_KEY))
		if credential.SessionToken == "" {
			credential.SessionToken = strings.TrimSpace(os.Getenv(common.VOLCENGINE_SESSION_TOKEN))
			if credential.SessionToken == "" {
				credential.SessionToken = strings.TrimSpace(os.Getenv(common.VOLC_SESSIONTOKEN))
			}
		}
	case "BYTEPLUS":
		credential.AccessKeyID = strings.TrimSpace(os.Getenv(common.BYTEPLUS_ACCESS_KEY))
		credential.SecretAccessKey = strings.TrimSpace(os.Getenv(common.BYTEPLUS_SECRET_KEY))
		if credential.SessionToken == "" {
			credential.SessionToken = strings.TrimSpace(os.Getenv(common.BYTEPLUS_SESSION_TOKEN))
		}
	default:
		return VeIAMCredential{}, fmt.Errorf("unsupported Role credential environment prefix %q", s.config.EnvironmentPrefix)
	}
	switch {
	case credential.AccessKeyID != "" && credential.SecretAccessKey != "":
		return credential, nil
	case credential.AccessKeyID != "" || credential.SecretAccessKey != "":
		return VeIAMCredential{}, errors.New("Role environment credential requires both access key ID and secret access key")
	}

	credential, err := GetCredentialFromVeFaaSIAM(s.config.CredentialPath)
	if err != nil {
		return VeIAMCredential{}, fmt.Errorf("read Role credential: %w", err)
	}
	credential.AccessKeyID = strings.TrimSpace(credential.AccessKeyID)
	credential.SecretAccessKey = strings.TrimSpace(credential.SecretAccessKey)
	credential.SessionToken = strings.TrimSpace(credential.SessionToken)
	if explicitSessionToken != "" {
		credential.SessionToken = explicitSessionToken
	}
	if credential.AccessKeyID == "" || credential.SecretAccessKey == "" {
		return VeIAMCredential{}, errors.New("Role credential file requires both access key ID and secret access key")
	}
	return credential, nil
}
