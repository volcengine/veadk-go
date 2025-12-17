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

package ve_viking

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/utils"
	"gopkg.in/go-playground/validator.v8"
)

const (
	VikingKnowledgeBaseIndexNotExistCode = 1000005
	VikingKnowledgeBaseSuccessCode       = 0
)

var (
	ErrVikingKnowledgeConfig = errors.New("viking Knowledge Config Error")
)

type ClientConfig struct {
	AK           string `validate:"required"`
	SK           string `validate:"required"`
	SessionToken string `validate:"omitempty"`
	ResourceID   string `validate:"omitempty"`
	Index        string `validate:"omitempty"`
	Project      string `validate:"required"`
	Region       string `validate:"required"`
}

func (c *ClientConfig) validate() error {
	var validate *validator.Validate
	config := &validator.Config{TagName: "validate"}
	validate = validator.New(config)
	if err := validate.Struct(c); err != nil {
		for _, err := range err.(validator.ValidationErrors) {
			return fmt.Errorf("field %s validation failed: %s（rule: %s）", err.Field, err.Tag, err.Param)
		}
	}
	if c.ResourceID == "" && (c.Project == "" || c.Index == "") {
		return fmt.Errorf("%w: knowledge ResourceID or Index and Project is nil", ErrVikingKnowledgeConfig)
	}
	if c.Index != "" {
		if err := precheckIndexNaming(c.Index); err != nil {
			return err
		}
	}
	return nil
}

func NewConfig(cfg *ClientConfig) (*ClientConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("%w: viking config is nil", ErrVikingKnowledgeConfig)
	}
	if cfg.AK == "" {
		cfg.AK = utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
	}
	if cfg.SK == "" {
		cfg.SK = utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	}
	if cfg.AK == "" || cfg.SK == "" {
		iam, err := veauth.GetCredentialFromVeFaaSIAM()
		if err != nil {
			return nil, fmt.Errorf("%w : GetCredential error: %w, VOLCENGINE_ACCESS_KEY/VOLCENGINE_SECRET_KEY not found. Please set via environment variables or config file", ErrVikingKnowledgeConfig, err)
		}
		cfg.AK = iam.AccessKeyID
		cfg.SK = iam.SecretAccessKey
		cfg.SessionToken = iam.SessionToken
	}

	if cfg.Project == "" {
		cfg.Project = utils.GetEnvWithDefault(common.DATABASE_VIKING_PROJECT, configs.GetGlobalConfig().Database.Viking.Project, common.DEFAULT_DATABASE_VIKING_PROJECT)
	}
	if cfg.Region == "" {
		cfg.Region = utils.GetEnvWithDefault(common.DATABASE_VIKING_REGION, configs.GetGlobalConfig().Database.Viking.Region, common.DEFAULT_DATABASE_VIKING_REGION)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%w : %w", ErrVikingKnowledgeConfig, err)
	}
	return cfg, nil
}

func BuildHeaders(c *ClientConfig) map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
	}
	if c.SessionToken != "" {
		headers["X-Security-Token"] = c.SessionToken
	}
	return headers
}

type CommonResponse struct {
	Code      int64          `json:"code"`
	Message   string         `json:"message,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
}

func ParseJsonUseNumber(input []byte, target interface{}) error {
	var d *json.Decoder
	var err error
	d = json.NewDecoder(bytes.NewBuffer(input))
	if d == nil {
		return fmt.Errorf("ParseJsonUseNumber init NewDecoder failed")
	}
	d.UseNumber()
	err = d.Decode(&target)
	if err != nil {
		return fmt.Errorf("ParseJsonUseNumber Decode failed, err: %s", err.Error())
	}
	return nil
}

func precheckIndexNaming(index string) error {
	var indexNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)
	if l := len(index); l < 0 || l > 128 {
		return fmt.Errorf("%w: index length out of range (must be 1–128)", ErrVikingKnowledgeConfig)
	}
	if !indexNameRe.MatchString(index) {
		return fmt.Errorf("%w: index contains characters other than letters、numbers and _", ErrVikingKnowledgeConfig)
	}
	return nil
}
