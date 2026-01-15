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

package viking_knowledge

import (
	"fmt"
	"net/http"

	"github.com/volcengine/veadk-go/integrations/ve_sign"
	"github.com/volcengine/veadk-go/integrations/ve_viking"
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

// docs: https://www.volcengine.com/docs/84313/1254485?lang=zh#go-%E8%AF%AD%E8%A8%80%E8%B0%83%E7%94%A8%E5%85%A8%E6%B5%81%E7%A8%8B%E7%A4%BA%E4%BE%8B

type Client struct {
	*ve_viking.ClientConfig
}

func New(cfg *ve_viking.ClientConfig) (*Client, error) {
	cfg, err := ve_viking.NewConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{ClientConfig: cfg}, nil
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

func (c *Client) SearchKnowledge(query string, topK int32, rerank bool, chunkDiffusionCount int32, metadata map[string]any) (*CollectionSearchKnowledgeResponse, error) {
	searchKnowledgeReqParams := c.generateSearchKnowledgeReqParams(query, topK, metadata, rerank, chunkDiffusionCount)

	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    SearchKnowledgePath,
		Service: VikingKnowledgeService,
		Region:  c.Region,
		Header:  ve_viking.BuildHeaders(c.ClientConfig),
		Body:    searchKnowledgeReqParams,
	}.DoRequest()
	if err != nil {
		return nil, err
	}
	var searchKnowledgeResp *CollectionSearchKnowledgeResponse
	err = ve_viking.ParseJsonUseNumber(respBody, &searchKnowledgeResp)
	if err != nil {
		return nil, err
	}
	return searchKnowledgeResp, nil
}

func (c *Client) CollectionDelete() (*ve_viking.CommonResponse, error) {
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    CollectionDeletePath,
		Service: VikingKnowledgeService,
		Region:  c.Region,
		Header:  ve_viking.BuildHeaders(c.ClientConfig),
		Body: CollectionNameProjectRequest{
			Name:    c.Index,
			Project: c.Project,
		},
	}.DoRequest()
	if err != nil {
		return nil, err
	}
	var resp *ve_viking.CommonResponse
	err = ve_viking.ParseJsonUseNumber(respBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) CollectionCreate(descriptions ...string) (*ve_viking.CommonResponse, error) {
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
		Header:  ve_viking.BuildHeaders(c.ClientConfig),
		Body: CollectionCreateRequest{
			Name:        c.Index,
			Project:     c.Project,
			Description: description,
		},
	}.DoRequest()
	if err != nil {
		return nil, err
	}
	var resp *ve_viking.CommonResponse
	err = ve_viking.ParseJsonUseNumber(respBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) CollectionInfo() (*ve_viking.CommonResponse, error) {
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    CollectionInfoPath,
		Service: VikingKnowledgeService,
		Region:  c.Region,
		Header:  ve_viking.BuildHeaders(c.ClientConfig),
		Body: CollectionNameProjectRequest{
			Name:    c.Index,
			Project: c.Project,
		},
	}.DoRequest()
	if err != nil && len(respBody) == 0 {
		return nil, err
	}
	var resp *ve_viking.CommonResponse
	err = ve_viking.ParseJsonUseNumber(respBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) DocumentAddTOS(tosPath string) (*ve_viking.CommonResponse, error) {
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    DocumentAddPath,
		Service: VikingKnowledgeService,
		Region:  c.Region,
		Header:  ve_viking.BuildHeaders(c.ClientConfig),
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
	var resp *ve_viking.CommonResponse
	err = ve_viking.ParseJsonUseNumber(respBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) DocumentDelete(docID string) (*ve_viking.CommonResponse, error) {
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    DocumentDeletePath,
		Service: VikingKnowledgeService,
		Region:  c.Region,
		Header:  ve_viking.BuildHeaders(c.ClientConfig),
		Body: DocumentDeleteRequest{
			CollectionName: c.Index,
			Project:        c.Project,
			DocID:          docID,
		},
	}.DoRequest()
	if err != nil {
		return nil, err
	}
	var resp *ve_viking.CommonResponse
	err = ve_viking.ParseJsonUseNumber(respBody, &resp)
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
		Header:  ve_viking.BuildHeaders(c.ClientConfig),
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
	err = ve_viking.ParseJsonUseNumber(respBody, &resp)
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
		Header:  ve_viking.BuildHeaders(c.ClientConfig),
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
	err = ve_viking.ParseJsonUseNumber(respBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
