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

package ve_viking_knowledge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/go-playground/validator/v10"
	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/integrations/ve_sign"
	"github.com/volcengine/veadk-go/utils"
)

const (
	VikingKnowledgeService = "air"
)

var (
	KnowledgeBaseDomain  = "api-knowledgebase.mlp.cn-beijing.volces.com"
	SearchKnowledgePath  = "/api/knowledge/collection/search_knowledge"
	CollectionDeletePath = "/api/knowledge/collection/delete"
	CollectionCreatePath = "/api/knowledge/collection/create"
	CollectionInfoPath   = "/api/knowledge/collection/info"
	DocumentAddPath      = "/api/knowledge/doc/add"
	DocumentDeletePath   = "/api/knowledge/doc/delete"
	DocumentListPath     = "/api/knowledge/doc/list"
	ChunkListPath        = "/api/knowledge/point/list"
)

var VikingKnowledgeConfigErr = errors.New("viking Knowledge Config Error")

// docs: https://www.volcengine.com/docs/84313/1254485?lang=zh#go-%E8%AF%AD%E8%A8%80%E8%B0%83%E7%94%A8%E5%85%A8%E6%B5%81%E7%A8%8B%E7%A4%BA%E4%BE%8B

type Client struct {
	AK           string `validate:"required"`
	SK           string `validate:"required"`
	SessionToken string `validate:"omitempty"`
	ResourceID   string `validate:"omitempty"` //ResourceID or Index + Project
	Index        string `validate:"omitempty"`
	Project      string `validate:"required"`
	Region       string `validate:"required"`
}

func (c *Client) validate() error {
	var validate = validator.New()
	if err := validate.Struct(c); err != nil {
		for _, err := range err.(validator.ValidationErrors) {
			return fmt.Errorf("field %s validation failed: %s（rule: %s）", err.Field(), err.Tag(), err.Param())
		}
	}
	if c.ResourceID == "" && (c.Project == "" || c.Index == "") {
		return fmt.Errorf("%w: knowledge ResourceID or Index and Project is nil", VikingKnowledgeConfigErr)
	}
	if err := precheckIndexNaming(c.Index); err != nil {
		return err
	}
	return nil
}

func New(cfg *Client) (*Client, error) {
	if cfg.AK == "" {
		cfg.AK = utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
	}
	if cfg.SK == "" {
		cfg.SK = utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	}
	if cfg.AK == "" || cfg.SK == "" {
		iam, err := veauth.GetCredentialFromVeFaaSIAM()
		if err != nil {
			return nil, fmt.Errorf("%w : GetCredential error: %w", VikingKnowledgeConfigErr, err)
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
		return nil, fmt.Errorf("%w : %w", VikingKnowledgeConfigErr, err)
	}
	return cfg, nil
}

func (c *Client) buildHeaders() map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
	}
	if c.SessionToken != "" {
		headers["X-Security-Token"] = c.SessionToken
	}
	return headers
}

func (c *Client) generateSearchKnowledgeReqParams(query string, topK int32, metadata map[string]any, rerank bool, chunkDiffusionCount int32) CollectionSearchKnowledgeRequest {
	reqObj := CollectionSearchKnowledgeRequest{
		ResourceId:  c.ResourceID,
		Name:        c.Index,
		Project:     c.Project,
		Query:       query,
		Limit:       topK,
		DenseWeight: 0.5,
		Postprocessing: PostProcessing{
			RerankSwitch:        rerank,
			RetrieveCount:       topK * 3,
			GetAttachmentLink:   true,
			ChunkGroup:          true,
			ChunkDiffusionCount: chunkDiffusionCount,
		},
	}
	if metadata != nil {
		reqObj.QueryParam = &QueryParamInfo{
			DocFilter: c.buildDocFilterQuery(metadata),
		}
	}
	return reqObj
}

func (c *Client) buildDocFilterQuery(metadata map[string]any) map[string]any {
	conds := make([]map[string]any, 0, len(metadata))
	for k, v := range metadata {
		conds = append(conds, map[string]any{
			"op":    "must",
			"field": fmt.Sprintf("%v", k),
			"conds": []string{fmt.Sprintf("%v", v)},
		})
	}
	return map[string]any{
		"op":    "and",
		"conds": conds,
	}
}

func (c *Client) SearchKnowledge(query string, topK int32, chunkDiffusionCount int32, metadata map[string]any, rerank bool) (*CollectionSearchKnowledgeResponse, error) {
	searchKnowledgeReqParams := c.generateSearchKnowledgeReqParams(query, topK, metadata, rerank, chunkDiffusionCount)

	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    SearchKnowledgePath,
		Service: VikingKnowledgeService,
		Region:  c.Region,
		Header:  c.buildHeaders(),
		Body:    searchKnowledgeReqParams,
	}.DoRequest()
	if err != nil {
		return nil, err
	}
	var searchKnowledgeResp *CollectionSearchKnowledgeResponse
	err = ParseJsonUseNumber(respBody, &searchKnowledgeResp)
	if err != nil {
		return nil, err
	}
	return searchKnowledgeResp, nil
}

func (c *Client) CollectionDelete() (*CommonResponse, error) {
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    CollectionDeletePath,
		Service: VikingKnowledgeService,
		Region:  c.Region,
		Header:  c.buildHeaders(),
		Body: CollectionNameProjectRequest{
			Name:    c.Index,
			Project: c.Project,
		},
	}.DoRequest()
	if err != nil {
		return nil, err
	}
	var resp *CommonResponse
	err = ParseJsonUseNumber(respBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) CollectionCreate(descriptions ...string) (*CommonResponse, error) {
	var description string
	if len(descriptions) == 0 || descriptions[0] == "" {
		description = "Created by Volcengine Agent Development Kit (VeADK)."
	} else {
		description = descriptions[0]
	}
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    CollectionCreatePath,
		Service: VikingKnowledgeService,
		Region:  c.Region,
		Header:  c.buildHeaders(),
		Body: CollectionCreateRequest{
			Name:        c.Index,
			Project:     c.Project,
			Description: description,
		},
	}.DoRequest()
	if err != nil {
		return nil, err
	}
	var resp *CommonResponse
	err = ParseJsonUseNumber(respBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) CollectionInfo() (*CommonResponse, error) {
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    CollectionInfoPath,
		Service: VikingKnowledgeService,
		Region:  c.Region,
		Header:  c.buildHeaders(),
		Body: CollectionNameProjectRequest{
			Name:    c.Index,
			Project: c.Project,
		},
	}.DoRequest()
	if err != nil && len(respBody) == 0 {
		return nil, err
	}
	var resp *CommonResponse
	err = ParseJsonUseNumber(respBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) DocumentAddTOS(tosPath string) (*CommonResponse, error) {
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    DocumentAddPath,
		Service: VikingKnowledgeService,
		Region:  c.Region,
		Header:  c.buildHeaders(),
		Body: DocumentAddRequest{
			CollectionName: c.Index,
			Project:        c.Project,
			AddType:        "tos",
			TosPath:        tosPath,
		},
	}.DoRequest()
	if err != nil {
		return nil, err
	}
	var resp *CommonResponse
	err = ParseJsonUseNumber(respBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) DocumentDelete(docID string) (*CommonResponse, error) {
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    DocumentDeletePath,
		Service: VikingKnowledgeService,
		Region:  c.Region,
		Header:  c.buildHeaders(),
		Body: DocumentDeleteRequest{
			CollectionName: c.Index,
			Project:        c.Project,
			DocID:          docID,
		},
	}.DoRequest()
	if err != nil {
		return nil, err
	}
	var resp *CommonResponse
	err = ParseJsonUseNumber(respBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) DocumentList(offset int32, limit int32) (*DocumentListResponse, error) {
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    DocumentListPath,
		Service: VikingKnowledgeService,
		Region:  c.Region,
		Header:  c.buildHeaders(),
		Body: CommonListRequest{
			CollectionName: c.Index,
			Project:        c.Project,
			Offset:         offset,
			Limit:          limit,
		},
	}.DoRequest()
	if err != nil {
		return nil, err
	}
	var resp *DocumentListResponse
	err = ParseJsonUseNumber(respBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) ChunkList(offset int32, limit int32) (*ChunkListResponse, error) {
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    ChunkListPath,
		Service: VikingKnowledgeService,
		Region:  c.Region,
		Header:  c.buildHeaders(),
		Body: CommonListRequest{
			CollectionName: c.Index,
			Project:        c.Project,
			Offset:         offset,
			Limit:          limit,
		},
	}.DoRequest()
	if err != nil {
		return nil, err
	}
	var resp *ChunkListResponse
	err = ParseJsonUseNumber(respBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func precheckIndexNaming(index string) error {
	var indexNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)
	if l := len(index); !(l > 0 && l <= 128) {
		return fmt.Errorf("%w: index length out of range (must be 1–128)", VikingKnowledgeConfigErr)
	}
	if !indexNameRe.MatchString(index) {
		return fmt.Errorf("%w: index contains characters other than letters、numbers and _", VikingKnowledgeConfigErr)
	}
	return nil
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
