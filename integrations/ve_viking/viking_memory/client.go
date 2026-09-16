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

package viking_memory

import (
	"net/http"

	"github.com/volcengine/veadk-go/v2/integrations/ve_sign"
	"github.com/volcengine/veadk-go/v2/integrations/ve_viking"
)

const (
	VikingMemoryService = "air"
)

var (
	KnowledgeBaseDomain        = "api-knowledgebase.mlp.cn-beijing.volces.com"
	CollectionCreatePath       = "/api/memory/collection/create"
	CollectionInfoPath         = "/api/memory/collection/info"
	CollectionSearchMemoryPath = "/api/memory/search"
	AddSessionPath             = "/api/memory/messages/add"
)

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

func (c *Client) CollectionCreate(req *CollectionCreateRequest) (*ve_viking.CommonResponse, error) {
	req.Description = "Created by Volcengine Agent Development Kit (VeADK)."
	req.ProjectName = c.Project
	req.CollectionName = c.Index
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    CollectionCreatePath,
		Service: VikingMemoryService,
		Region:  c.Region,
		Header:  ve_viking.BuildHeaders(c.ClientConfig),
		Body:    req,
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

func (c *Client) CollectionInfo() error {
	_, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    CollectionInfoPath,
		Service: VikingMemoryService,
		Region:  c.Region,
		Header:  ve_viking.BuildHeaders(c.ClientConfig),
		Body: CollectionInfoRequest{
			CollectionName: c.Index,
			ProjectName:    c.Project,
			ResourceId:     c.ResourceID,
		},
	}.DoRequest()
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) CollectionSearchMemory(req *CollectionSearchMemoryRequest) (*CollectionSearchMemoryResponse, error) {
	req.ResourceId = c.ResourceID
	req.CollectionName = c.Index
	req.ProjectName = c.Project
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    CollectionSearchMemoryPath,
		Service: VikingMemoryService,
		Region:  c.Region,
		Header:  ve_viking.BuildHeaders(c.ClientConfig),
		Body:    req,
	}.DoRequest()
	if err != nil {
		return nil, err
	}
	var resp *CollectionSearchMemoryResponse
	err = ve_viking.ParseJsonUseNumber(respBody, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) AddSession(req *AddSessionRequest) (*ve_viking.CommonResponse, error) {
	req.ResourceId = c.ResourceID
	req.CollectionName = c.Index
	req.ProjectName = c.Project
	respBody, err := ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Host:    KnowledgeBaseDomain,
		Path:    AddSessionPath,
		Service: VikingMemoryService,
		Region:  c.Region,
		Header:  ve_viking.BuildHeaders(c.ClientConfig),
		Body:    req,
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
